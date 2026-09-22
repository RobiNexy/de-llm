// Package llm 实现设计文档 §7 的 LLM 处理系统。
package llm

import (
	"context"
	"time"
)

// Provider 是 LLM 提供方的统一抽象（设计文档 §7.11）。
type Provider interface {
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
}

// ResponseFormat 控制是否请求 JSON 输出。
type ResponseFormat int

const (
	FormatText ResponseFormat = iota
	FormatJSON
)

// CompletionRequest 是一次补全请求。
type CompletionRequest struct {
	Model          string
	Messages       []Message
	Temperature    float64
	MaxTokens      int
	ResponseFormat ResponseFormat
}

// Message 是对话消息。
type Message struct {
	Role    string // system | user | assistant
	Content string
}

// CompletionResponse 是补全结果。
type CompletionResponse struct {
	Content      string
	FinishReason string
	Usage        Usage
}

// Usage 记录 token 消耗；CachedTokens 为 prefix cache 命中数（provider 支持时）。
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
}

// ProviderConfig 是单个 provider 的配置（设计文档 §3.3）。
type ProviderConfig struct {
	Type        string   `yaml:"type"` // openai | anthropic | ollama
	BaseURL     string   `yaml:"base_url"`
	APIKey      string   `yaml:"api_key"`
	Model       string   `yaml:"model"`
	Temperature float64  `yaml:"temperature"`
	MaxTokens   int      `yaml:"max_tokens"`
	Timeout     Duration `yaml:"timeout"`
	MaxRetries  int      `yaml:"max_retries"`
}

// Overrides 是 llm_step 级别的 provider 参数覆盖。
type Overrides struct {
	Model       string   `yaml:"model"`
	Temperature *float64 `yaml:"temperature"`
	MaxTokens   int      `yaml:"max_tokens"`
	Timeout     Duration `yaml:"timeout"`
}

// Apply 将覆盖项应用到配置副本。
func (o Overrides) Apply(base ProviderConfig) ProviderConfig {
	c := base
	if o.Model != "" {
		c.Model = o.Model
	}
	if o.Temperature != nil {
		c.Temperature = *o.Temperature
	}
	if o.MaxTokens > 0 {
		c.MaxTokens = o.MaxTokens
	}
	if o.Timeout > 0 {
		c.Timeout = o.Timeout
	}
	return c
}

// Duration 包装 time.Duration 以支持 "60s" 形式的 YAML 反序列化。
type Duration time.Duration

func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		dd, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		*d = Duration(dd)
		return nil
	}
	var n int64
	if err := unmarshal(&n); err == nil {
		*d = Duration(n * int64(time.Second))
		return nil
	}
	return unmarshal(&d)
}

func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

func (d Duration) D() time.Duration { return time.Duration(d) }
