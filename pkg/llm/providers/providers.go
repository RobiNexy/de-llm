// Package providers 实现三种内置 Provider（设计文档 §7.11）。
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/RobiNexy/de-llm/pkg/llm"
)

// New 按 type 构造 provider 实例。
func New(cfg llm.ProviderConfig) (llm.Provider, error) {
	switch cfg.Type {
	case "openai":
		return &OpenAI{cfg: cfg}, nil
	case "anthropic":
		return &Anthropic{cfg: cfg}, nil
	case "ollama":
		return &Ollama{cfg: cfg}, nil
	}
	return nil, fmt.Errorf("不支持的 provider type: %q", cfg.Type)
}

// timeout 为请求上下文附加超时。
func timeoutCtx(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

// --- OpenAI 兼容接口 -------------------------------------------------

// OpenAI 支持 /chat/completions；JSON mode 通过 response_format。
// 该接口同时兼容 DeepSeek、Moonshot、Together 等 OpenAI 风格 API。
type OpenAI struct {
	cfg llm.ProviderConfig
}

type openaiRequest struct {
	Model          string          `json:"model"`
	Messages       []openaiMessage `json:"messages"`
	Temperature    float64         `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format,omitempty"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponse struct {
	Choices []struct {
		Message      openaiMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		TotalTokens         int `json:"total_tokens"`
		PromptTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

func (p *OpenAI) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	ctx, cancel := timeoutCtx(ctx, p.cfg.Timeout.D())
	defer cancel()

	payload := openaiRequest{
		Model:       firstNonEmpty(req.Model, p.cfg.Model),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	if payload.Temperature == 0 {
		payload.Temperature = p.cfg.Temperature
	}
	if payload.MaxTokens == 0 {
		payload.MaxTokens = p.cfg.MaxTokens
	}
	for _, m := range req.Messages {
		payload.Messages = append(payload.Messages, openaiMessage{Role: m.Role, Content: m.Content})
	}
	if req.ResponseFormat == llm.FormatJSON {
		payload.ResponseFormat = &struct {
			Type string `json:"type"`
		}{Type: "json_object"}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/chat/completions", trimSlash(p.cfg.BaseURL))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	resp, err := llm.DoWithRetry(ctx, p.client(), httpReq, body, p.cfg.MaxRetries)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai: HTTP %d: %s", resp.StatusCode, truncate(raw, 300))
	}
	var or openaiResponse
	if err := json.Unmarshal(raw, &or); err != nil {
		return nil, fmt.Errorf("openai: 响应解析失败: %w", err)
	}
	if len(or.Choices) == 0 {
		return nil, fmt.Errorf("openai: 响应无 choices")
	}
	out := &llm.CompletionResponse{
		Content:      or.Choices[0].Message.Content,
		FinishReason: or.Choices[0].FinishReason,
	}
	out.Usage.PromptTokens = or.Usage.PromptTokens
	out.Usage.CompletionTokens = or.Usage.CompletionTokens
	out.Usage.TotalTokens = or.Usage.TotalTokens
	if or.Usage.PromptTokensDetails != nil {
		out.Usage.CachedTokens = or.Usage.PromptTokensDetails.CachedTokens
	}
	return out, nil
}

func (p *OpenAI) client() *http.Client { return &http.Client{} }

// --- Anthropic -------------------------------------------------------

// Anthropic 使用 /v1/messages；无原生 JSON mode，
// JSON 输出靠提示词约束 + 解析层剥离（设计文档 §7.11）。
type Anthropic struct {
	cfg llm.ProviderConfig
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

func (p *Anthropic) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	ctx, cancel := timeoutCtx(ctx, p.cfg.Timeout.D())
	defer cancel()

	payload := anthropicRequest{
		Model:     firstNonEmpty(req.Model, p.cfg.Model),
		MaxTokens: req.MaxTokens,
	}
	if payload.MaxTokens == 0 {
		payload.MaxTokens = p.cfg.MaxTokens
	}
	if payload.MaxTokens == 0 {
		payload.MaxTokens = 4096
	}
	payload.Temperature = req.Temperature
	if payload.Temperature == 0 {
		payload.Temperature = p.cfg.Temperature
	}
	for _, m := range req.Messages {
		if m.Role == "system" {
			payload.System = m.Content
			continue
		}
		payload.Messages = append(payload.Messages, anthropicMessage{Role: m.Role, Content: m.Content})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/v1/messages", trimSlash(p.cfg.BaseURL))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if p.cfg.APIKey != "" {
		httpReq.Header.Set("x-api-key", p.cfg.APIKey)
	}

	resp, err := llm.DoWithRetry(ctx, &http.Client{}, httpReq, body, p.cfg.MaxRetries)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, truncate(raw, 300))
	}
	var ar anthropicResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return nil, fmt.Errorf("anthropic: 响应解析失败: %w", err)
	}
	out := &llm.CompletionResponse{}
	for _, c := range ar.Content {
		if c.Type == "text" {
			out.Content += c.Text
		}
	}
	out.Usage.PromptTokens = ar.Usage.InputTokens
	out.Usage.CompletionTokens = ar.Usage.OutputTokens
	out.Usage.TotalTokens = ar.Usage.InputTokens + ar.Usage.OutputTokens
	out.Usage.CachedTokens = ar.Usage.CacheReadInputTokens
	return out, nil
}

// --- Ollama ----------------------------------------------------------

// Ollama 使用本地 /api/chat；format: "json" 开启 JSON mode。
type Ollama struct {
	cfg llm.ProviderConfig
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Format   any             `json:"format,omitempty"`
	Options  ollamaOptions   `json:"options,omitempty"`
	Stream   bool            `json:"stream"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

type ollamaResponse struct {
	Message ollamaMessage `json:"message"`
	Usage   struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (p *Ollama) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	ctx, cancel := timeoutCtx(ctx, p.cfg.Timeout.D())
	defer cancel()

	payload := ollamaRequest{
		Model:  firstNonEmpty(req.Model, p.cfg.Model),
		Stream: false,
		Options: ollamaOptions{
			Temperature: req.Temperature,
			NumPredict:  req.MaxTokens,
		},
	}
	if payload.Options.Temperature == 0 {
		payload.Options.Temperature = p.cfg.Temperature
	}
	if payload.Options.NumPredict == 0 {
		payload.Options.NumPredict = p.cfg.MaxTokens
	}
	for _, m := range req.Messages {
		payload.Messages = append(payload.Messages, ollamaMessage{Role: m.Role, Content: m.Content})
	}
	if req.ResponseFormat == llm.FormatJSON {
		payload.Format = "json"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/api/chat", trimSlash(p.cfg.BaseURL))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := llm.DoWithRetry(ctx, &http.Client{}, httpReq, body, p.cfg.MaxRetries)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama: HTTP %d: %s", resp.StatusCode, truncate(raw, 300))
	}
	var orr ollamaResponse
	if err := json.Unmarshal(raw, &orr); err != nil {
		return nil, fmt.Errorf("ollama: 响应解析失败: %w", err)
	}
	out := &llm.CompletionResponse{
		Content: orr.Message.Content,
	}
	out.Usage.PromptTokens = orr.Usage.PromptTokens
	out.Usage.CompletionTokens = orr.Usage.CompletionTokens
	out.Usage.TotalTokens = orr.Usage.TotalTokens
	return out, nil
}

// --- helpers ---------------------------------------------------------

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}
