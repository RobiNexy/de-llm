// Package config 实现设计文档 §3 的配置系统：
// 内置默认 → 用户级 → 项目级 → front matter → CLI flags 的多层合并。
//
// 实现说明：选用 gopkg.in/yaml.v3 直接反序列化到强类型结构，
// 而非设计文档技术选型中的 viper。裁决依据（规则 4：可运行与可验证
// 优先于理论完备）：profile 的"整体替换"合并语义 viper 不支持，
// 仍需手写合并逻辑；强类型结构避免了 viper map[string]any 往返的
// 类型损伤（duration、int 数组）。代价是多约 80 行显式查找/合并代码。
package config

import (
	"fmt"

	"github.com/RobiNexy/de-llm/pkg/llm"
	"github.com/RobiNexy/de-llm/pkg/rule"
)

// Version 是当前配置格式的版本号。
const Version = 1

// Config 是完整配置结构（设计文档 §3.3）。
type Config struct {
	Version  int                   `yaml:"version"`
	Profiles map[string]*Profile   `yaml:"profiles"`
	Rules    map[string]RuleParams `yaml:"rules"`
	LLM      LLMConfig             `yaml:"llm"`
	Global   GlobalConfig          `yaml:"global"`

	// 运行时回填：配置文件路径与选中的 profile 名。
	SourceFile  string `yaml:"-"`
	ProfileName string `yaml:"-"`
}

// Profile 是一个完整的处理行为描述（整体替换合并）。
type Profile struct {
	Preprocess  []string         `yaml:"preprocess"`
	LLMSteps    []llm.StepConfig `yaml:"llm_steps"`
	Postprocess []string         `yaml:"postprocess"`
}

// RuleParams 是规则参数的原始映射，支持深度合并。
type RuleParams map[string]any

// LLMConfig 是 LLM 段配置。
type LLMConfig struct {
	Providers   map[string]llm.ProviderConfig `yaml:"providers"`
	Prompts     PromptsConfig                 `yaml:"prompts"`
	Concurrency int                           `yaml:"concurrency"`
	RateLimit   string                        `yaml:"rate_limit"`
	OnFailure   string                        `yaml:"on_failure"` // preserve | fail
}

// PromptsConfig 是提示词搜索目录配置。
type PromptsConfig struct {
	Dirs []string `yaml:"dirs"`
}

// GlobalConfig 是全局段配置。
type GlobalConfig struct {
	Encoding string `yaml:"encoding"`
	LogLevel string `yaml:"log_level"`
}

// FrontMatterConfig 是文档 front matter 中 dellm: 块的配置（§3.4）。
// 不支持在此定义 llm_steps 或 providers——它们是项目级关注点。
type FrontMatterConfig struct {
	Profile string                `yaml:"profile"`
	Skip    []string              `yaml:"skip"`
	Only    []string              `yaml:"only"`
	Rules   map[string]RuleParams `yaml:"rules"`
}

// Validate 检查配置的约束条件。
func (c *Config) Validate() error {
	if c.Version != 0 && c.Version != Version {
		return fmt.Errorf("不支持的配置版本 %d（当前 %d）", c.Version, Version)
	}
	if c.LLM.OnFailure != "" && c.LLM.OnFailure != "preserve" && c.LLM.OnFailure != "fail" {
		return fmt.Errorf("llm.on_failure 只支持 preserve/fail，得到 %q", c.LLM.OnFailure)
	}
	if c.LLM.Concurrency != 0 && c.LLM.Concurrency != 1 {
		return fmt.Errorf("llm.concurrency 当前只支持 1，得到 %d", c.LLM.Concurrency)
	}
	if _, err := llm.NewRateLimiter(c.LLM.RateLimit); err != nil {
		return fmt.Errorf("llm.rate_limit: %w", err)
	}
	for name, pc := range c.LLM.Providers {
		if pc.Type == "" {
			return fmt.Errorf("llm.providers.%s 缺少 type", name)
		}
		switch pc.Type {
		case "openai", "anthropic", "ollama":
		default:
			return fmt.Errorf("llm.providers.%s 的 type %q 不支持（openai/anthropic/ollama）", name, pc.Type)
		}
		if pc.BaseURL == "" {
			return fmt.Errorf("llm.providers.%s 缺少 base_url", name)
		}
		if pc.MaxTokens < 0 {
			return fmt.Errorf("llm.providers.%s.max_tokens 不能为负数", name)
		}
		if pc.MaxRetries < 0 {
			return fmt.Errorf("llm.providers.%s.max_retries 不能为负数", name)
		}
		if pc.Timeout < 0 {
			return fmt.Errorf("llm.providers.%s.timeout 不能为负数", name)
		}
	}
	for pname, p := range c.Profiles {
		for _, r := range append(append([]string{}, p.Preprocess...), p.Postprocess...) {
			if !knownRule(r) {
				return fmt.Errorf("profile %q 引用了未知规则 %q", pname, r)
			}
		}
		for i, s := range p.LLMSteps {
			if s.Name == "" {
				return fmt.Errorf("profile %q 的第 %d 个 llm_step 缺少 name", pname, i+1)
			}
			if s.Action != llm.ActionRewrite && s.Action != llm.ActionFullRewrite {
				return fmt.Errorf("llm_step %q 的 action %q 不支持（rewrite/full_rewrite）", s.Name, s.Action)
			}
			if s.Prompt.Rewrite == "" {
				return fmt.Errorf("llm_step %q 缺少 rewrite 提示词", s.Name)
			}
			if s.Detect && s.Prompt.Detect == "" {
				return fmt.Errorf("llm_step %q 启用了 detect 但缺少 detect 提示词", s.Name)
			}
			if s.Action == llm.ActionRewrite {
				switch s.Target {
				case "paragraph", "heading", "list_item", "blockquote":
				default:
					return fmt.Errorf("llm_step %q 的 target %q 不支持（paragraph/heading/list_item/blockquote）", s.Name, s.Target)
				}
			}
			if s.Provider == "" {
				if _, ok := c.LLM.Providers["default"]; !ok {
					return fmt.Errorf("llm_step %q 使用默认 provider，但缺少 llm.providers.default", s.Name)
				}
			} else if _, ok := c.LLM.Providers[s.Provider]; !ok {
				return fmt.Errorf("llm_step %q 引用了未知 provider %q", s.Name, s.Provider)
			}
			if s.Filter != nil {
				if s.Filter.MinLength < 0 || s.Filter.MaxLength < 0 {
					return fmt.Errorf("llm_step %q 的 filter 长度不能为负数", s.Name)
				}
				if s.Filter.MinLength > 0 && s.Filter.MaxLength > 0 && s.Filter.MinLength > s.Filter.MaxLength {
					return fmt.Errorf("llm_step %q 的 filter.min_length 不能大于 max_length", s.Name)
				}
			}
		}
	}
	return nil
}

// knownRule 报告规则名是否已注册。
func knownRule(name string) bool { return rule.Exists(name) }
