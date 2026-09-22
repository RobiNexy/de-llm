// step.go 实现设计文档 §7.9 的 LLM Step 执行逻辑。
package llm

import (
	"context"
	"fmt"
	"log"

	"github.com/RobiNexy/de-llm/llm/prompt"
	"github.com/RobiNexy/de-llm/markdown"
)

// ActionType 是 step 的动作类型。
type ActionType string

const (
	ActionRewrite     ActionType = "rewrite"
	ActionFullRewrite ActionType = "full_rewrite"
)

// Filter 是结构层面的硬筛选（设计文档 §7.7）。
type Filter struct {
	Level     []int `yaml:"level"`
	MinLength int   `yaml:"min_length"`
	MaxLength int   `yaml:"max_length"`
}

// Step 是一个可执行的 LLM 步骤。
type Step struct {
	Name       string
	Action     ActionType
	Target     string // paragraph | heading | list_item | blockquote
	Detect     bool
	Filter     *Filter
	PromptRef  PromptRef
	Params     map[string]any
	Provider   Provider
	RateLimit  *RateLimiter
	MaxRetries int

	// Reporter 接收 warning；为 nil 时输出到标准日志。
	Reporter func(format string, args ...any)

	// Usage 累计本 step 的 token 消耗（串行执行，无需加锁）。
	Usage Usage

	// Stats 记录节点处理数。
	NodesDetected  int
	NodesRewritten int
}

// PromptRef 持有 detect / rewrite 两个提示词；rewrite 必填。
type PromptRef struct {
	Detect  *prompt.Prompt // detect: false 时可为 nil
	Rewrite *prompt.Prompt
}

// StepConfig 是配置文件中 llm_steps 的单项。
type StepConfig struct {
	Name      string         `yaml:"name"`
	Action    ActionType     `yaml:"action"`
	Target    string         `yaml:"target"`
	Detect    bool           `yaml:"detect"`
	Filter    *Filter        `yaml:"filter"`
	Prompt    PromptCfg      `yaml:"prompt"`
	Params    map[string]any `yaml:"params"`
	Overrides Overrides      `yaml:"overrides"`
}

// PromptCfg 引用两个提示词名。
type PromptCfg struct {
	Detect  string `yaml:"detect"`
	Rewrite string `yaml:"rewrite"`
}

// Execute 按 action 分发（设计文档 §7.9）。
func (s *Step) Execute(ctx context.Context, doc []byte) ([]byte, error) {
	switch s.Action {
	case ActionRewrite:
		return s.executeRewrite(ctx, doc)
	case ActionFullRewrite:
		return s.executeFullRewrite(ctx, doc)
	}
	return nil, fmt.Errorf("未知 action: %s", s.Action)
}

func (s *Step) warn(format string, args ...any) {
	if s.Reporter != nil {
		s.Reporter(format, args...)
		return
	}
	log.Printf(format, args...)
}

// complete 封装限流 + 调用 + usage 累计。
func (s *Step) complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if err := s.RateLimit.Wait(ctx); err != nil {
		return nil, err
	}
	resp, err := s.Provider.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	s.Usage.PromptTokens += resp.Usage.PromptTokens
	s.Usage.CompletionTokens += resp.Usage.CompletionTokens
	s.Usage.TotalTokens += resp.Usage.TotalTokens
	s.Usage.CachedTokens += resp.Usage.CachedTokens
	return resp, nil
}

func (s *Step) executeRewrite(ctx context.Context, doc []byte) ([]byte, error) {
	// 1. 标注。
	annotated := markdown.AnnotateDocument(doc, s.Target)

	// 2. 确定目标节点。
	var targets []*markdown.AnnotatedNode
	if s.Detect {
		if s.PromptRef.Detect == nil {
			return nil, fmt.Errorf("step %q: detect: true 但未配置 detect 提示词", s.Name)
		}
		detectVars := map[string]any{"annotated_doc": string(annotated.Annotated)}
		mergeVars(detectVars, s.Params)
		detectPrompt, err := s.PromptRef.Detect.Render(detectVars)
		if err != nil {
			return nil, err
		}
		resp, err := s.complete(ctx, &CompletionRequest{
			Messages:       []Message{{Role: "user", Content: detectPrompt}},
			ResponseFormat: FormatJSON,
		})
		if err != nil {
			return nil, err
		}
		matches, err := ParseDetect(resp.Content)
		if err != nil {
			return nil, err
		}
		var w []Warning
		targets, w = ValidateMatches(annotated, matches)
		for _, warning := range w {
			s.warn("llm step %q 检测轮: %s", s.Name, warning)
		}
	} else {
		targets = ApplyFilter(annotated.Nodes, s.Filter)
	}
	s.NodesDetected = len(targets)

	// 检测轮 / filter 之后没有目标 → 原样返回。
	if len(targets) == 0 {
		return doc, nil
	}

	// 3. 改写轮：整篇 + 目标节点列表（prefix cache 复用同一份标注原文）。
	rewriteVars := map[string]any{
		"annotated_doc": string(annotated.Annotated),
		"target_nodes":  markdown.FormatTargets(targets),
	}
	mergeVars(rewriteVars, s.Params)
	rewritePrompt, err := s.PromptRef.Rewrite.Render(rewriteVars)
	if err != nil {
		return nil, err
	}
	resp, err := s.complete(ctx, &CompletionRequest{
		Messages:       []Message{{Role: "user", Content: rewritePrompt}},
		ResponseFormat: FormatJSON,
	})
	if err != nil {
		return nil, err
	}
	rewrites, err := ParseRewrite(resp.Content)
	if err != nil {
		return nil, err
	}

	// 4. 回贴。
	result, warnings := ApplyRewrites(annotated, rewrites)
	s.NodesRewritten = len(rewrites)
	for _, w := range warnings {
		s.warn("llm step %q 改写轮: %s", s.Name, w)
	}
	return result, nil
}

func (s *Step) executeFullRewrite(ctx context.Context, doc []byte) ([]byte, error) {
	vars := map[string]any{"document": string(doc)}
	mergeVars(vars, s.Params)
	rendered, err := s.PromptRef.Rewrite.Render(vars)
	if err != nil {
		return nil, err
	}
	resp, err := s.complete(ctx, &CompletionRequest{
		Messages: []Message{{Role: "user", Content: rendered}},
		// full_rewrite 不用 JSON mode（设计文档 §7.3.2）。
	})
	if err != nil {
		return nil, err
	}
	if resp.Content == "" {
		return nil, fmt.Errorf("llm step %q: full_rewrite 返回空内容", s.Name)
	}
	return []byte(resp.Content), nil
}

// applyFilter 按结构硬筛选节点（不感知内容模式，设计文档 §7.7）。
func ApplyFilter(nodes []markdown.AnnotatedNode, f *Filter) []*markdown.AnnotatedNode {
	out := make([]*markdown.AnnotatedNode, 0, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		if f != nil {
			if len(f.Level) > 0 && n.Type == "heading" && !containsInt(f.Level, n.Level) {
				continue
			}
			if f.MinLength > 0 && runeLen(n.Text) < f.MinLength {
				continue
			}
			if f.MaxLength > 0 && runeLen(n.Text) > f.MaxLength {
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

func containsInt(arr []int, v int) bool {
	for _, e := range arr {
		if e == v {
			return true
		}
	}
	return false
}

func runeLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// mergeVars 把 src 合并到 dst（src 优先级更高）。
func mergeVars(dst, src map[string]any) {
	for k, v := range src {
		dst[k] = v
	}
}
