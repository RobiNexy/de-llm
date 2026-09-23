// config_test.go — 多层级加载与合并语义测试（design.md §3.1/§3.2）。
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RobiNexy/de-llm/pkg/llm"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadProjectConfig(t *testing.T) {
	p := writeTemp(t, "dellm.yaml", `
version: 1
profiles:
  custom:
    preprocess: []
    llm_steps: []
    postprocess: [quotes, pangu]
`)
	cfg, err := Load(Options{ConfigPath: p, NoUserConfig: true})
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	prof, ok := cfg.Profiles["custom"]
	if !ok {
		t.Fatal("custom profile 缺失")
	}
	if len(prof.Postprocess) != 2 {
		t.Errorf("postprocess = %v", prof.Postprocess)
	}
}

func TestProfileReplacementNotMerged(t *testing.T) {
	// §3.2：profiles 整体替换。项目级重定义 default 后，
	// 内置 default 的规则列表不应残留。
	p := writeTemp(t, "dellm.yaml", `
version: 1
profiles:
  default:
    preprocess: []
    llm_steps: []
    postprocess: [pangu]
`)
	cfg, err := Load(Options{ConfigPath: p, NoUserConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Profiles["default"].Postprocess) != 1 || cfg.Profiles["default"].Postprocess[0] != "pangu" {
		t.Errorf("profile 应整体替换: %+v", cfg.Profiles["default"].Postprocess)
	}
}

func TestRulesDeepMerge(t *testing.T) {
	// §3.2：rules 深度合并——项目级只覆盖指定键。
	p := writeTemp(t, "dellm.yaml", `
version: 1
rules:
  quotes:
    single_quotes: false
`)
	cfg, err := Load(Options{ConfigPath: p, NoUserConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	q := cfg.Rules["quotes"]
	if q["single_quotes"] != false {
		t.Errorf("single_quotes 应为 false: %v", q["single_quotes"])
	}
	if q["apostrophe_protection"] != true {
		t.Errorf("apostrophe_protection 应保留默认 true: %v", q["apostrophe_protection"])
	}
}

func TestProvidersDeepMerge(t *testing.T) {
	p := writeTemp(t, "dellm.yaml", `
version: 1
llm:
  providers:
    default:
      base_url: "https://api.deepseek.com/v1"
      model: "deepseek-chat"
`)
	cfg, err := Load(Options{ConfigPath: p, NoUserConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	pc := cfg.LLM.Providers["default"]
	if pc.Model != "deepseek-chat" {
		t.Errorf("model 应覆盖: %v", pc.Model)
	}
	if pc.MaxRetries != 3 {
		t.Errorf("max_retries 应保留默认 3: %v", pc.MaxRetries)
	}
	if pc.Timeout.D().Seconds() != 60 {
		t.Errorf("timeout 应保留默认 60s: %v", pc.Timeout)
	}
}

func TestAPIKeyEnvExpansion(t *testing.T) {
	t.Setenv("DELLM_TEST_KEY_XYZ", "secret123")
	p := writeTemp(t, "dellm.yaml", `
version: 1
llm:
  providers:
    default:
      api_key: "${DELLM_TEST_KEY_XYZ}"
`)
	cfg, err := Load(Options{ConfigPath: p, NoUserConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Providers["default"].APIKey != "secret123" {
		t.Errorf("环境变量未展开: %q", cfg.LLM.Providers["default"].APIKey)
	}
}

func TestFindProjectConfigUpward(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dellm.yaml"), []byte("version: 1\nprofiles:\n  default:\n    postprocess: [pangu]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	found, err := findProjectConfig(sub)
	if err != nil {
		t.Fatal(err)
	}
	if found != filepath.Join(dir, "dellm.yaml") {
		t.Errorf("应向上找到配置: %q", found)
	}
}

func TestFrontMatterConfig(t *testing.T) {
	src := []byte(`---
title: 测试
dellm:
  profile: quick
  skip:
    - indent
  rules:
    quotes:
      single_quotes: false
---

# 正文
`)
	fm, err := ParseFrontMatter(src)
	if err != nil {
		t.Fatal(err)
	}
	if fm == nil {
		t.Fatal("front matter 未解析")
	}
	if fm.Profile != "quick" {
		t.Errorf("profile = %q", fm.Profile)
	}
	if len(fm.Skip) != 1 || fm.Skip[0] != "indent" {
		t.Errorf("skip = %v", fm.Skip)
	}
	if fm.Rules["quotes"]["single_quotes"] != false {
		t.Errorf("rules 覆盖未解析: %v", fm.Rules["quotes"])
	}
}

func TestFrontMatterAbsent(t *testing.T) {
	fm, err := ParseFrontMatter([]byte("# 无 front matter\n"))
	if err != nil {
		t.Fatal(err)
	}
	if fm != nil {
		t.Errorf("应返回 nil: %v", fm)
	}
}

func TestValidateRejectsBadAction(t *testing.T) {
	cfg := Default()
	cfg.Profiles["bad"] = &Profile{
		LLMSteps: []llm.StepConfig{
			{Name: "s", Action: "unknown", Target: "paragraph"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("应拒绝未知 action")
	}
}
