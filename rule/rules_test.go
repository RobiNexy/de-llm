// indent_test.go、emphasis_space_test.go、remove_separators_test.go、
// shift_headings_test.go — 各规则的 golden 测试与行为断言。
package rule

import (
	"strings"
	"testing"

	"github.com/RobiNexy/de-llm/markdown"
)

// markdownBuild 供测试使用。
func markdownBuild(src []byte) (*markdown.RegionMap, error) {
	return markdown.BuildRegionMap(src)
}

// --- indent ---

func TestIndentGolden(t *testing.T) {
	cases := []struct {
		caseName string
		cfg      RuleConfig
	}{
		{caseName: "basic"},
		{caseName: "idempotent"},
		{caseName: "blockquote_scope"},
		{caseName: "skip_first", cfg: RuleConfig{"skip_first_paragraph": true}},
	}
	for _, tc := range cases {
		t.Run(tc.caseName, func(t *testing.T) {
			runGolden(t, "indent", tc.caseName, tc.cfg, false)
		})
	}
}

func TestIndentBlockquoteScope(t *testing.T) {
	r, err := New("indent", RuleConfig{"scope": []any{"paragraph", "blockquote"}})
	if err != nil {
		t.Fatal(err)
	}
	src := []byte("正文。\n\n> 引用段落。\n")
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	result, _ := r.Transform(ctx, rm)
	want := "\u3000\u3000正文。\n\n> \u3000\u3000引用段落。\n"
	if string(result) != want {
		t.Errorf("得到 %q, 期望 %q", result, want)
	}
}

func TestIndentSkipContinuationLines(t *testing.T) {
	r, _ := New("indent", RuleConfig{})
	src := []byte("第一行\n第二行（软换行）。\n")
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	result, _ := r.Transform(ctx, rm)
	if strings.Count(string(result), "\u3000\u3000") != 1 {
		t.Errorf("只应缩进段落第一行: %q", result)
	}
}

// --- emphasis_space ---

func TestEmphasisSpaceGolden(t *testing.T) {
	runGolden(t, "emphasis_space", "basic", RuleConfig{}, false)
}

func TestEmphasisSpaceIdempotent(t *testing.T) {
	r, _ := New("emphasis_space", RuleConfig{})
	src := []byte("中文**加粗**中文。\n")
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	once, _ := r.Transform(ctx, rm)
	rm2, _ := markdownBuild(once)
	ctx2 := newTestContext()
	twice, _ := r.Transform(ctx2, rm2)
	if string(once) != string(twice) {
		t.Errorf("幂等性破坏:\nonce:  %q\ntwice: %q", once, twice)
	}
}

// --- remove_separators ---

func TestRemoveSeparatorsGolden(t *testing.T) {
	cases := []string{"basic", "setext", "frontmatter", "collapse"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			runGolden(t, "remove_separators", c, RuleConfig{}, false)
		})
	}
}

func TestRemoveSeparatorsKeepFrontmatterFalse(t *testing.T) {
	// keep_frontmatter: false 时 front matter 内的 ThematicBreak 也会被删
	//（用户自负其责）；Setext 下划线仍受守卫保护（它不是 ThematicBreak）。
	r, err := New("remove_separators", RuleConfig{"keep_frontmatter": false})
	if err != nil {
		t.Fatal(err)
	}
	src := []byte("---\ntitle: x\n---\n\n正文。\n\n---\n\n尾。\n")
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	result, _ := r.Transform(ctx, rm)
	// 首行 ---（ThematicBreak）删除；title: x 的 Setext 下划线保留。
	if strings.Count(string(result), "---") != 1 {
		t.Errorf("期望仅剩 Setext 下划线: %q", result)
	}
	if !strings.HasPrefix(string(result), "title: x") {
		t.Errorf("首行分隔线应被删除: %q", result)
	}
}

func TestRemoveSeparatorsListDashesUntouched(t *testing.T) {
	r, _ := New("remove_separators", RuleConfig{})
	src := []byte("- 列表项一\n- 列表项二\n\n正文\n")
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	result, _ := r.Transform(ctx, rm)
	if string(result) != string(src) {
		t.Errorf("列表项不应被删除: %q", result)
	}
}

// --- shift_headings ---

func TestShiftHeadingsGolden(t *testing.T) {
	cases := []string{"basic", "setext"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			expectWarn := c == "setext"
			runGolden(t, "shift_headings", c, RuleConfig{"offset": -1}, expectWarn)
		})
	}
}

func TestShiftHeadingsOverflowModes(t *testing.T) {
	src := []byte("# 一级\n\n正文\n")

	// clamp：h1 提升仍为 h1。
	r, _ := New("shift_headings", RuleConfig{"offset": -1})
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	result, err := r.Transform(ctx, rm)
	if err != nil || string(result) != "# 一级\n\n正文\n" {
		t.Errorf("clamp 结果: %q err=%v", result, err)
	}

	// error：越界终止。
	r, _ = New("shift_headings", RuleConfig{"offset": -1, "on_overflow": "error"})
	ctx = newTestContext()
	rm, _ = markdownBuild(src)
	if _, err := r.Transform(ctx, rm); err == nil {
		t.Error("error 模式应返回错误")
	}

	// skip：跳过不变。
	r, _ = New("shift_headings", RuleConfig{"offset": -1, "on_overflow": "skip"})
	ctx = newTestContext()
	rm, _ = markdownBuild(src)
	result, err = r.Transform(ctx, rm)
	if err != nil || string(result) != string(src) {
		t.Errorf("skip 结果: %q err=%v", result, err)
	}
}

func TestShiftHeadingsScope(t *testing.T) {
	r, _ := New("shift_headings", RuleConfig{"offset": 1, "scope": []any{1}})
	src := []byte("# 一级\n\n## 二级\n")
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	result, _ := r.Transform(ctx, rm)
	want := "## 一级\n\n## 二级\n"
	if string(result) != want {
		t.Errorf("scope 过滤失败:\n得到 %q\n期望 %q", result, want)
	}
}

func TestShiftHeadingsOffsetZeroNoop(t *testing.T) {
	r, _ := New("shift_headings", RuleConfig{"offset": 0})
	src := []byte("# 标题\n")
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	result, _ := r.Transform(ctx, rm)
	if string(result) != string(src) {
		t.Errorf("offset=0 应无操作: %q", result)
	}
}

func TestShiftHeadingsPreservesOtherLines(t *testing.T) {
	r, _ := New("shift_headings", RuleConfig{"offset": -1})
	// 正文中的 # 字符（代码块内）不受影响。
	src := []byte("## 标题\n\n```go\n## 这不是标题\n```\n")
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	result, _ := r.Transform(ctx, rm)
	want := "# 标题\n\n```go\n## 这不是标题\n```\n"
	if string(result) != want {
		t.Errorf("得到 %q 期望 %q", result, want)
	}
}
