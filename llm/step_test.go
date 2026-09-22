package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RobiNexy/de-llm/llm/prompt"
	"github.com/RobiNexy/de-llm/markdown"
)

func TestParseDetectTolerance(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantIDs []string
		wantErr bool
	}{
		{
			name:    "纯 JSON",
			raw:     `{"matches": [{"id": "P3", "prefix": "abc"}]}`,
			wantIDs: []string{"P3"},
		},
		{
			name:    "markdown 代码块包裹",
			raw:     "```json\n{\"matches\": [{\"id\": \"P1\", \"prefix\": \"x\"}]}\n```",
			wantIDs: []string{"P1"},
		},
		{
			name:    "尾逗号",
			raw:     `{"matches": [{"id": "P1", "prefix": "x"}, {"id": "P2", "prefix": "y"},]}`,
			wantIDs: []string{"P1", "P2"},
		},
		{
			name:    "前后解释文字",
			raw:     `好的，以下是结果： {"matches": [{"id": "P7", "prefix": "z"}]} 以上。`,
			wantIDs: []string{"P7"},
		},
		{
			name:    "空结果",
			raw:     `{"matches": []}`,
			wantIDs: nil,
		},
		{
			name:    "完全非法",
			raw:     `这不是 JSON`,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matches, err := ParseDetect(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望错误，得到 nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("意外错误: %v", err)
			}
			if len(matches) != len(tc.wantIDs) {
				t.Fatalf("匹配数 %d != 期望 %d", len(matches), len(tc.wantIDs))
			}
			for i, id := range tc.wantIDs {
				if matches[i].ID != id {
					t.Errorf("matches[%d].ID = %s, 期望 %s", i, matches[i].ID, id)
				}
			}
		})
	}
}

func TestParseRewriteTolerance(t *testing.T) {
	raw := "```json\n{\"rewrites\": [{\"id\": \"P1\", \"prefix\": \"原\", \"text\": \"新\"},]}\n```"
	rewrites, err := ParseRewrite(raw)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(rewrites) != 1 || rewrites[0].Text != "新" {
		t.Fatalf("解析结果错误: %+v", rewrites)
	}
}

const threeParaSrc = "第一个段落的内容足够长可以校验。\n\n第二个段落的内容足够长可以校验。\n\n第三个段落的内容足够长可以校验。\n"

func annotateThreeParas(t *testing.T) *markdown.AnnotatedDoc {
	t.Helper()
	return markdown.AnnotateDocument([]byte(threeParaSrc), "paragraph")
}

func TestApplyRewritesPrefixValidation(t *testing.T) {
	doc := annotateThreeParas(t)

	rewrites := []Rewrite{
		{ID: "P1", Prefix: "第一个段落的内容足够长", Text: "改写一"},
		{ID: "P2", Prefix: "前缀不匹配的内容xxx", Text: "改写二"}, // prefix 不匹配 → 跳过
		{ID: "P9", Prefix: "", Text: "改写三"},            // id 不存在 → 跳过
		{ID: "P3", Prefix: "", Text: "   "},            // 空 text → 跳过
	}
	result, warnings := ApplyRewrites(doc, rewrites)

	want := "改写一\n\n第二个段落的内容足够长可以校验。\n\n第三个段落的内容足够长可以校验。\n"
	if string(result) != want {
		t.Errorf("回贴结果错误:\n得到 %q\n期望 %q", result, want)
	}
	if len(warnings) != 3 {
		t.Fatalf("warnings 数量 %d != 3: %v", len(warnings), warnings)
	}
}

func TestApplyRewritesBackToFront(t *testing.T) {
	doc := annotateThreeParas(t)
	rewrites := []Rewrite{
		{ID: "P1", Prefix: "", Text: "A"},
		{ID: "P2", Prefix: "", Text: "B"},
		{ID: "P3", Prefix: "", Text: "C"},
	}
	result, warnings := ApplyRewrites(doc, rewrites)
	if len(warnings) != 0 {
		t.Fatalf("意外 warnings: %v", warnings)
	}
	want := "A\n\nB\n\nC\n"
	if string(result) != want {
		t.Errorf("得到 %q, 期望 %q", result, want)
	}
}

func TestApplyRewritesDuplicateID(t *testing.T) {
	doc := annotateThreeParas(t)
	rewrites := []Rewrite{
		{ID: "P1", Prefix: "", Text: "甲"},
		{ID: "P1", Prefix: "", Text: "乙"},
	}
	result, warnings := ApplyRewrites(doc, rewrites)
	if string(result) != "甲\n\n第二个段落的内容足够长可以校验。\n\n第三个段落的内容足够长可以校验。\n" {
		t.Errorf("重复 id 应只应用首个: %q", result)
	}
	if len(warnings) != 1 {
		t.Errorf("重复 id 应产生 1 条 warning，得到 %d", len(warnings))
	}
}

// mustTestPrompt 构造最小模板用于 step 测试。
func mustTestPrompt(t *testing.T, name, body string) *prompt.Prompt {
	t.Helper()
	p, err := prompt.Parse("---\nname: " + name + "\nversion: 1\nparams:\n  required: []\n---\n\n" + body)
	if err != nil {
		t.Fatalf("构造 prompt 失败: %v", err)
	}
	return p
}

func TestStepRewriteDetectFlow(t *testing.T) {
	src := []byte("## 标题\n\n第一段。" + strings.Repeat("长", 20) + "\n\n其原因是明确的：与其抱怨不如行动。\n")

	multi := NewMultiMockProvider(
		MockResponse{MatchPromptContains: "matches", Content: `{"matches": [{"id": "P2", "prefix": "其原因是明确的"}]}`},
		MockResponse{Content: `{"rewrites": [{"id": "P2", "prefix": "其原因是明确的", "text": "与其抱怨，不如行动。改为直接陈述。"}]}`},
	)
	step := &Step{
		Name:   "t",
		Action: ActionRewrite,
		Target: "paragraph",
		Detect: true,
		PromptRef: PromptRef{
			Detect:  mustTestPrompt(t, "detect", "检测：{{ .annotated_doc }}"),
			Rewrite: mustTestPrompt(t, "rewrite", "改写：{{ .annotated_doc }} {{ .target_nodes }}"),
		},
		Provider: multi,
	}

	result, err := step.Execute(context.Background(), src)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if multi.calls != 2 {
		t.Fatalf("调用次数 %d != 2", multi.calls)
	}
	want := "## 标题\n\n第一段。" + strings.Repeat("长", 20) + "\n\n与其抱怨，不如行动。改为直接陈述。\n"
	if string(result) != want {
		t.Errorf("结果错误:\n得到 %q\n期望 %q", result, want)
	}
	if step.NodesDetected != 1 || step.NodesRewritten != 1 {
		t.Errorf("节点统计错误: detected=%d rewritten=%d", step.NodesDetected, step.NodesRewritten)
	}
}

func TestStepRewriteFilterFlow(t *testing.T) {
	// detect: false，程序 filter 筛标题；LLM 对不需要改的标题不返回 → 原样保留。
	src := []byte("# 主标题：一个冗长的标题\n\n正文。\n")
	multi := NewMultiMockProvider(
		MockResponse{Content: `{"rewrites": [{"id": "H1", "prefix": "# 主标题：一个冗长", "text": "# 主标题"}]}`},
	)
	step := &Step{
		Name:   "h",
		Action: ActionRewrite,
		Target: "heading",
		Detect: false,
		PromptRef: PromptRef{
			Rewrite: mustTestPrompt(t, "rewrite", "改写 {{ .annotated_doc }}"),
		},
		Provider: multi,
	}
	result, err := step.Execute(context.Background(), src)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	want := "# 主标题\n\n正文。\n"
	if string(result) != want {
		t.Errorf("结果错误:\n得到 %q\n期望 %q", result, want)
	}
}

func TestStepFullRewrite(t *testing.T) {
	multi := NewMultiMockProvider(MockResponse{Content: "全文重写后的内容\n"})
	step := &Step{
		Name:      "full",
		Action:    ActionFullRewrite,
		PromptRef: PromptRef{Rewrite: mustTestPrompt(t, "full", "文档：{{ .document }}")},
		Provider:  multi,
	}
	result, err := step.Execute(context.Background(), []byte("原文\n"))
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if string(result) != "全文重写后的内容\n" {
		t.Errorf("结果错误: %q", result)
	}
}

func TestStepFailure(t *testing.T) {
	multi := NewMultiMockProvider(MockResponse{Err: errors.New("api down")})
	step := &Step{
		Name:      "f",
		Action:    ActionFullRewrite,
		PromptRef: PromptRef{Rewrite: mustTestPrompt(t, "full", "{{ .document }}")},
		Provider:  multi,
	}
	_, err := step.Execute(context.Background(), []byte("原文"))
	if err == nil {
		t.Fatal("期望错误")
	}
}

func TestStepEmptyDetectionResult(t *testing.T) {
	// 检测轮无匹配 → 不发起改写轮，原文原样返回。
	multi := NewMultiMockProvider(
		MockResponse{Content: `{"matches": []}`},
	)
	step := &Step{
		Name:   "t",
		Action: ActionRewrite,
		Target: "paragraph",
		Detect: true,
		PromptRef: PromptRef{
			Detect:  mustTestPrompt(t, "detect", "{{ .annotated_doc }}"),
			Rewrite: mustTestPrompt(t, "rewrite", "{{ .annotated_doc }}"),
		},
		Provider: multi,
	}
	result, err := step.Execute(context.Background(), []byte("正文。\n"))
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if multi.calls != 1 {
		t.Fatalf("只应调用检测轮 1 次，实际 %d", multi.calls)
	}
	if string(result) != "正文。\n" {
		t.Errorf("结果错误: %q", result)
	}
}
