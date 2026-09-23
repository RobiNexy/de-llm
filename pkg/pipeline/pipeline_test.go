// pipeline_test.go — 端到端集成测试（LLM 用 mock，design.md §12.4）。
package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RobiNexy/de-llm/pkg/config"
	"github.com/RobiNexy/de-llm/pkg/llm"
	"github.com/RobiNexy/de-llm/pkg/llm/prompt"
)

// mockFactory 返回按调用顺序响应的 mock provider。
func mockFactory(responses ...llm.MockResponse) func(llm.ProviderConfig) (llm.Provider, error) {
	p := llm.NewMultiMockProvider(responses...)
	return func(llm.ProviderConfig) (llm.Provider, error) { return p, nil }
}

// testConfig 构造集成测试配置：一步 rewrite（detect=true）。
func testConfig() *config.Config {
	cfg := config.Default()
	cfg.Profiles["test"] = &config.Profile{
		Preprocess:  []string{"remove_separators"},
		Postprocess: []string{"quotes", "pangu"},
		LLMSteps: []llm.StepConfig{
			{
				Name:   "去转折",
				Action: llm.ActionRewrite,
				Target: "paragraph",
				Detect: true,
				Prompt: llm.PromptCfg{Detect: "detect_contrasting", Rewrite: "rewrite_contrasting"},
			},
		},
	}
	cfg.LLM.RateLimit = ""
	return cfg
}

func TestPipelineEndToEnd(t *testing.T) {
	input, err := os.ReadFile("testdata/integration/input.md")
	if err != nil {
		t.Fatal(err)
	}

	// 检测轮命中 P2；改写轮返回新文本。
	factory := mockFactory(
		llm.MockResponse{Content: `{"matches": [{"id": "P2", "prefix": "其原因是明确的"}]}`},
		llm.MockResponse{Content: `{"rewrites": [{"id": "P2", "prefix": "其原因是明确的", "text": "原因明确：正确性优先，速度其次。"}]}`},
	)

	p, err := Build(testConfig(), "test", BuilderOptions{ProviderFactory: factory})
	if err != nil {
		t.Fatal(err)
	}
	ctx := NewContext(context.Background(), false)
	result, err := p.Process(ctx, input)
	if err != nil {
		t.Fatal(err)
	}

	out := string(result)
	if !strings.Contains(out, "原因明确：正确性优先，速度其次。") {
		t.Errorf("改写未回贴:\n%s", out)
	}
	if strings.Contains(out, "与其在速度上死磕") {
		t.Errorf("原文段落应被替换:\n%s", out)
	}
	if !strings.Contains(out, "该方案的核心**不是**追求速度，而是追求稳定性。") {
		t.Errorf("其余段落应原样保留:\n%s", out)
	}
	// 后处理：mmap 与 。之间不再有空格（裁决行为）。
	if strings.Contains(out, "mmap 。") {
		t.Errorf("pangu 裁决行为被破坏:\n%s", out)
	}
}

func TestPipelineSkipLLM(t *testing.T) {
	input, err := os.ReadFile("testdata/integration/input.md")
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.Profiles["test"].LLMSteps = nil

	called := false
	factory := func(llm.ProviderConfig) (llm.Provider, error) {
		called = true
		return nil, nil
	}
	p, err := Build(cfg, "test", BuilderOptions{ProviderFactory: factory})
	if err != nil {
		t.Fatal(err)
	}
	ctx := NewContext(context.Background(), false)
	if _, err := p.Process(ctx, input); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("无 LLM 步骤时不应构造 provider")
	}
}

func TestPipelineConsecutiveFailureCircuitBreaker(t *testing.T) {
	cfg := config.Default()
	cfg.Profiles["f"] = &config.Profile{
		Postprocess: []string{"pangu"},
		LLMSteps: []llm.StepConfig{
			{Name: "s1", Action: llm.ActionFullRewrite, Prompt: llm.PromptCfg{Rewrite: "compress"}},
			{Name: "s2", Action: llm.ActionFullRewrite, Prompt: llm.PromptCfg{Rewrite: "compress"}},
			{Name: "s3", Action: llm.ActionFullRewrite, Prompt: llm.PromptCfg{Rewrite: "compress"}},
		},
	}
	cfg.LLM.RateLimit = ""

	factory := mockFactory(llm.MockResponse{Err: errFake})
	p, err := Build(cfg, "f", BuilderOptions{ProviderFactory: factory})
	if err != nil {
		t.Fatal(err)
	}
	ctx := NewContext(context.Background(), false)
	_, err = p.Process(ctx, []byte("正文。\n"))
	if err == nil {
		t.Fatal("连续 3 次失败应终止流水线")
	}
	if !strings.Contains(err.Error(), "连续 3 次失败") {
		t.Errorf("错误信息不符: %v", err)
	}
}

var errFake = &fakeError{}

type fakeError struct{}

func (*fakeError) Error() string { return "fake api down" }

func TestPipelinePreserveOnSingleFailure(t *testing.T) {
	cfg := config.Default()
	cfg.Profiles["f"] = &config.Profile{
		Postprocess: []string{"pangu"},
		LLMSteps: []llm.StepConfig{
			{Name: "s1", Action: llm.ActionFullRewrite, Prompt: llm.PromptCfg{Rewrite: "compress"}},
		},
	}
	cfg.LLM.RateLimit = ""

	factory := mockFactory(llm.MockResponse{Err: errFake})
	p, _ := Build(cfg, "f", BuilderOptions{ProviderFactory: factory})
	ctx := NewContext(context.Background(), false)
	result, err := p.Process(ctx, []byte("中文English混排。\n"))
	if err != nil {
		t.Fatalf("preserve 策略不应终止: %v", err)
	}
	// 保留原文继续后处理。
	if !strings.Contains(string(result), "中文 English 混排。") {
		t.Errorf("后处理应继续执行: %q", result)
	}
	if len(ctx.Warnings) == 0 {
		t.Error("失败应产生 warning")
	}
}

func TestPipelineOnFailureFailMode(t *testing.T) {
	cfg := config.Default()
	cfg.Profiles["f"] = &config.Profile{
		LLMSteps: []llm.StepConfig{
			{Name: "s1", Action: llm.ActionFullRewrite, Prompt: llm.PromptCfg{Rewrite: "compress"}},
		},
	}
	cfg.LLM.RateLimit = ""
	cfg.LLM.OnFailure = "fail"

	factory := mockFactory(llm.MockResponse{Err: errFake})
	p, _ := Build(cfg, "f", BuilderOptions{ProviderFactory: factory})
	ctx := NewContext(context.Background(), false)
	if _, err := p.Process(ctx, []byte("正文")); err == nil {
		t.Fatal("fail 模式应立即终止")
	}
}

func TestBuildRejectsUnknownProfile(t *testing.T) {
	if _, err := Build(config.Default(), "no-such", BuilderOptions{}); err == nil {
		t.Fatal("未知 profile 应报错")
	}
}

func TestBuildRejectsMissingPrompt(t *testing.T) {
	cfg := config.Default()
	cfg.Profiles["p"] = &config.Profile{
		LLMSteps: []llm.StepConfig{
			{Name: "s", Action: llm.ActionFullRewrite, Prompt: llm.PromptCfg{Rewrite: "no_such_prompt"}},
		},
	}
	if _, err := Build(cfg, "p", BuilderOptions{}); err == nil {
		t.Fatal("缺失提示词应报错")
	}
}

// --- diff ---

func TestUnifiedDiff(t *testing.T) {
	oldSrc := []byte("a\nb\nc\nd\ne\n")
	newSrc := []byte("a\nB\nc\nd\ne\n")
	d := UnifiedDiff("a.md", "b.md", oldSrc, newSrc, 1)
	if !strings.Contains(d, "-b\n+B\n") {
		t.Errorf("diff 错误:\n%s", d)
	}
	if !strings.HasPrefix(d, "--- a.md\n+++ b.md\n") {
		t.Errorf("头错误:\n%s", d)
	}
}

func TestUnifiedDiffNoChange(t *testing.T) {
	if d := UnifiedDiff("a", "b", []byte("same\n"), []byte("same\n"), 3); d != "" {
		t.Errorf("无差异应返回空串: %q", d)
	}
}

func TestUnifiedDiffMultipleHunks(t *testing.T) {
	oldSrc := []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n")
	newSrc := []byte("X\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\nY\n")
	d := UnifiedDiff("a", "b", oldSrc, newSrc, 1)
	if strings.Count(d, "\n@@ ") != 2 && !strings.HasPrefix(d, "@@ ") {
		t.Errorf("应产生两个 hunk:\n%s", d)
	}
}

// --- dry run ---

func TestDryRunReport(t *testing.T) {
	cfg := config.Default()
	p, err := Build(cfg, "default", BuilderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	report := DryRunReport(cfg, "default", []byte("# 标题\n\n正文中文English。\n"), p, "input.md")
	if !strings.Contains(report, "profile: default") {
		t.Errorf("报告错误:\n%s", report)
	}
	if !strings.Contains(report, "remove_separators") {
		t.Errorf("应列出预处理规则:\n%s", report)
	}
}

// prompt 注册表内置加载健全性。
func TestBuiltinPromptsLoaded(t *testing.T) {
	reg := prompt.NewRegistry()
	if err := reg.LoadBuiltin(prompt.Builtin); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"detect_contrasting", "rewrite_contrasting", "rewrite_heading", "elaborate_steps", "compress", "polish"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("内置提示词 %s 未加载", name)
		}
	}
}

// prompt 渲染（design.md §12.5）。
func TestPromptRender(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "llm", "prompt", "builtin", "rewrite_contrasting.md"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := prompt.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Render(map[string]any{
		"annotated_doc": "[P1] 测试段落",
		"target_nodes":  "[P1] 测试段落",
		"style":         "克制",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "克制") || !strings.Contains(result, "[P1] 测试段落") {
		t.Errorf("渲染错误:\n%s", result)
	}
}

func TestPromptRequiredVarMissing(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "llm", "prompt", "builtin", "rewrite_contrasting.md"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := prompt.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Render(map[string]any{}); err == nil {
		t.Error("缺少必填变量应报错")
	}
}

func TestPromptOptionalDefault(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "llm", "prompt", "builtin", "rewrite_contrasting.md"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := prompt.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Render(map[string]any{
		"annotated_doc": "X",
		"target_nodes":  "Y",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "直接陈述事实") {
		t.Errorf("optional 默认值未注入:\n%s", result)
	}
}

// 自定义提示词目录加载与覆盖（§8.4）。
func TestPromptDirsOverride(t *testing.T) {
	dir := t.TempDir()
	override := "---\nname: detect_contrasting\ndescription: 自定义覆盖版\nversion: 2\nparams:\n  required: []\n---\n\n自定义正文 {{ .annotated_doc }}\n"
	if err := os.WriteFile(filepath.Join(dir, "detect_contrasting.md"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := prompt.NewRegistry()
	if err := reg.LoadBuiltin(prompt.Builtin); err != nil {
		t.Fatal(err)
	}
	if err := reg.LoadDirs([]string{dir}); err != nil {
		t.Fatal(err)
	}
	p, ok := reg.Get("detect_contrasting")
	if !ok {
		t.Fatal("提示词缺失")
	}
	if p.Description != "自定义覆盖版" {
		t.Errorf("目录覆盖未生效: %s", p.Description)
	}
}
