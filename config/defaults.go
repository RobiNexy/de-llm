// defaults.go 提供内置默认配置（设计文档 §3.3 的 default/quick/full profile）。
package config

import "github.com/RobiNexy/de-llm/llm"

// Default 返回内置默认配置。
// 默认 profile 仅含本地规则：零配置即可用。
func Default() *Config {
	return &Config{
		Version: Version,
		Profiles: map[string]*Profile{
			"default": {
				Preprocess:  []string{"remove_separators"},
				LLMSteps:    nil,
				Postprocess: []string{"quotes", "indent", "pangu", "emphasis_space"},
			},
			"quick": {
				Preprocess:  nil,
				LLMSteps:    nil,
				Postprocess: []string{"quotes", "pangu"},
			},
			"full": fullProfile(),
		},
		Rules: map[string]RuleParams{
			"quotes": {
				"style":                 "curly",
				"single_quotes":         true,
				"apostrophe_protection": true,
			},
			"remove_separators": {"keep_frontmatter": true},
			"shift_headings":    {"offset": 0, "scope": []any{}, "on_overflow": "clamp"},
			"indent": {
				"char":                 "\u3000\u3000",
				"scope":                []any{"paragraph"},
				"skip_first_paragraph": false,
			},
			"pangu":          {"space_char": " "},
			"emphasis_space": {"space_char": " "},
		},
		LLM: LLMConfig{
			Providers: map[string]llm.ProviderConfig{
				"default": defaultProvider(),
			},
			Prompts: PromptsConfig{
				Dirs: []string{"./prompts"},
			},
			Concurrency: 1,
			RateLimit:   "60/minute",
			OnFailure:   "preserve",
		},
		Global: GlobalConfig{Encoding: "utf-8", LogLevel: "info"},
	}
}

// defaultProvider 返回默认 provider 配置。
// api_key 通过 ${VAR} 环境变量展开（加载阶段处理）。
func defaultProvider() llm.ProviderConfig {
	return llm.ProviderConfig{
		Type:        "openai",
		BaseURL:     "https://api.openai.com/v1",
		APIKey:      "${DELLM_API_KEY}",
		Model:       "gpt-4o",
		Temperature: 0.3,
		MaxTokens:   4096,
		Timeout:     llm.Duration(60_000_000_000), // 60s
		MaxRetries:  3,
	}
}

// fullProfile 返回完整流程示例（LLM 改写三步）。
// 该 profile 依赖用户配置 provider 与 API key；未配置时
// 构建流水线会因缺少 provider 而报错，由 --skip-llm 规避。
func fullProfile() *Profile {
	return &Profile{
		Preprocess: []string{"remove_separators"},
		LLMSteps: []llm.StepConfig{
			{
				Name:   "全文细化",
				Action: llm.ActionFullRewrite,
				Prompt: llm.PromptCfg{Rewrite: "elaborate_steps"},
				Params: map[string]any{"expand_ratio": "1.3"},
			},
			{
				Name:   "重写标题",
				Action: llm.ActionRewrite,
				Target: "heading",
				Prompt: llm.PromptCfg{Rewrite: "rewrite_heading"},
				Filter: &llm.Filter{Level: []int{1, 2, 3}},
			},
			{
				Name:   "去掉转折对比句式",
				Action: llm.ActionRewrite,
				Target: "paragraph",
				Detect: true,
				Prompt: llm.PromptCfg{Detect: "detect_contrasting", Rewrite: "rewrite_contrasting"},
			},
		},
		Postprocess: []string{"quotes", "indent", "pangu", "emphasis_space"},
	}
}
