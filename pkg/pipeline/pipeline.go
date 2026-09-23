// Package pipeline 实现设计文档 §9 的三段式流水线编排。
package pipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/RobiNexy/de-llm/pkg/config"
	"github.com/RobiNexy/de-llm/pkg/llm"
	"github.com/RobiNexy/de-llm/pkg/llm/prompt"
	"github.com/RobiNexy/de-llm/pkg/llm/providers"
	markdown "github.com/RobiNexy/de-llm/pkg/parser"
	"github.com/RobiNexy/de-llm/pkg/rule"
)

// Context 是一次文档处理的上下文：warning 与统计的汇聚点。
// 内嵌 context.Context 以贯穿 LLM 调用的取消与超时。
type Context struct {
	context.Context

	Warnings []string

	// stats[规则/步骤名][统计键] = 计数
	stats   map[string]map[string]int
	verbose bool
}

func NewContext(parent context.Context, verbose bool) *Context {
	if parent == nil {
		parent = context.Background()
	}
	return &Context{Context: parent, stats: map[string]map[string]int{}, verbose: verbose}
}

func (c *Context) Warnf(format string, args ...any) {
	c.Warnings = append(c.Warnings, fmt.Sprintf(format, args...))
}

// AddStatOwner 显式指定归属者（rule 经由 ruleCtx；LLM step 直接调用）。
func (c *Context) AddStatOwner(owner, key string, delta int) {
	if c.stats[owner] == nil {
		c.stats[owner] = map[string]int{}
	}
	c.stats[owner][key] += delta
}

// ruleCtx 将 Context 绑定到当前规则名，作为 rule.Context 传递。
type ruleCtx struct {
	*Context
	owner string
}

func (rc ruleCtx) AddStat(key string, delta int) {
	rc.Context.AddStatOwner(rc.owner, key, delta)
}

// bindRule 为规则执行绑定统计归属。
func (c *Context) bindRule(name string) ruleCtx { return ruleCtx{Context: c, owner: name} }

// Stats 返回统计快照（拷贝）。
func (c *Context) Stats() map[string]map[string]int {
	out := map[string]map[string]int{}
	for o, m := range c.stats {
		out[o] = map[string]int{}
		for k, v := range m {
			out[o][k] = v
		}
	}
	return out
}

// Pipeline 是构建完成的三段式流水线。
type Pipeline struct {
	preprocess       []rule.Rule
	llmSteps         []*llm.Step
	postprocess      []rule.Rule
	onFailure        string
	consecutiveFails int
	verbose          bool
}

// BuilderOptions 是 BuildPipeline 的选项。
type BuilderOptions struct {
	// ProviderFactory 允许测试注入 mock provider。
	ProviderFactory func(pc llm.ProviderConfig) (llm.Provider, error)
	// Verbose 输出 debug 日志。
	Verbose bool
	// OnFailure 覆盖配置的失败策略。
	OnFailure string
}

// Build 根据配置构建流水线（设计文档 §9.3）。
//
// 步骤：确定 profile → 应用 CLI 过滤（only/skip/skip-llm/shift-headings
// 由调用方在调用前修改 profile 副本或通过 options 传入）→ 实例化规则
// → 解析 prompt → 构造 provider → 组装。
func Build(cfg *config.Config, profileName string, opts BuilderOptions) (*Pipeline, error) {
	prof, ok := cfg.Profiles[profileName]
	if !ok {
		return nil, fmt.Errorf("profile %q 不存在（可用: %s）", profileName, joinKeys(cfg.Profiles))
	}

	onFailure := cfg.LLM.OnFailure
	if onFailure == "" {
		onFailure = "preserve"
	}
	if opts.OnFailure != "" {
		onFailure = opts.OnFailure
	}

	p := &Pipeline{onFailure: onFailure, verbose: opts.Verbose}

	// 解析提示词注册表：内置 + 配置目录（项目优先，§8.4）。
	reg := prompt.NewRegistry()
	if err := reg.LoadBuiltin(prompt.Builtin); err != nil {
		return nil, fmt.Errorf("加载内置提示词失败: %w", err)
	}
	if err := reg.LoadDirs(cfg.LLM.Prompts.Dirs); err != nil {
		return nil, fmt.Errorf("加载外部提示词失败: %w", err)
	}

	// 预处理与后处理规则。
	var err error
	p.preprocess, err = buildRules(prof.Preprocess, cfg.Rules)
	if err != nil {
		return nil, fmt.Errorf("preprocess: %w", err)
	}
	p.postprocess, err = buildRules(prof.Postprocess, cfg.Rules)
	if err != nil {
		return nil, fmt.Errorf("postprocess: %w", err)
	}

	// LLM 步骤。
	for i := range prof.LLMSteps {
		sc := prof.LLMSteps[i]
		step, err := buildStep(sc, cfg, reg, opts)
		if err != nil {
			return nil, fmt.Errorf("llm_step %q: %w", sc.Name, err)
		}
		p.llmSteps = append(p.llmSteps, step)
	}
	return p, nil
}

// buildStep 构造单个 LLM step。
func buildStep(sc llm.StepConfig, cfg *config.Config, reg *prompt.Registry, opts BuilderOptions) (*llm.Step, error) {
	var ref llm.PromptRef
	if sc.Prompt.Rewrite != "" {
		p, ok := reg.Get(sc.Prompt.Rewrite)
		if !ok {
			return nil, fmt.Errorf("rewrite 提示词 %q 未找到", sc.Prompt.Rewrite)
		}
		ref.Rewrite = p
	} else {
		return nil, fmt.Errorf("缺少 rewrite 提示词")
	}
	if sc.Prompt.Detect != "" {
		p, ok := reg.Get(sc.Prompt.Detect)
		if !ok {
			return nil, fmt.Errorf("detect 提示词 %q 未找到", sc.Prompt.Detect)
		}
		ref.Detect = p
	}

	// provider 解析：step 未指定时用 llm.providers.default。
	pc, ok := cfg.LLM.Providers["default"]
	if !ok {
		return nil, fmt.Errorf("缺少 llm.providers.default 配置")
	}
	pc = sc.Overrides.Apply(pc)
	if pc.APIKey == "" && pc.Type != "ollama" {
		return nil, fmt.Errorf("provider api_key 为空（检查 ${VAR} 环境变量是否已设置）")
	}

	factory := opts.ProviderFactory
	if factory == nil {
		factory = func(pc llm.ProviderConfig) (llm.Provider, error) {
			return providers.New(pc)
		}
	}
	provider, err := factory(pc)
	if err != nil {
		return nil, err
	}

	rl, err := llm.NewRateLimiter(cfg.LLM.RateLimit)
	if err != nil {
		return nil, err
	}

	return &llm.Step{
		Name:       sc.Name,
		Action:     sc.Action,
		Target:     sc.Target,
		Detect:     sc.Detect,
		Filter:     sc.Filter,
		PromptRef:  ref,
		Params:     sc.Params,
		Provider:   provider,
		RateLimit:  rl,
		MaxRetries: pc.MaxRetries,
	}, nil
}

func buildRules(names []string, rulesCfg map[string]config.RuleParams) ([]rule.Rule, error) {
	var out []rule.Rule
	for _, name := range names {
		rc := rule.RuleConfig{}
		if cfg, ok := rulesCfg[name]; ok {
			for k, v := range cfg {
				rc[k] = v
			}
		}
		r, err := rule.New(name, rc)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Process 执行三段式流水线（设计文档 §9.2）。
func (p *Pipeline) Process(ctx *Context, source []byte) ([]byte, error) {
	current := source

	// ① 预处理。
	for _, r := range p.preprocess {
		rm, err := markdown.BuildRegionMap(current)
		if err != nil {
			return nil, fmt.Errorf("preprocess %s: %w", r.Name(), err)
		}
		result, err := r.Transform(ctx.bindRule(r.Name()), rm)
		if err != nil {
			return nil, fmt.Errorf("preprocess %s: %w", r.Name(), err)
		}
		current = result
	}

	// ② LLM 步骤（串行，保证 prefix cache 复用）。
	consecutiveFailures := 0
	for _, step := range p.llmSteps {
		step.Reporter = ctx.Warnf
		result, err := step.Execute(ctx, current)
		if err != nil {
			consecutiveFailures++
			if consecutiveFailures >= 3 {
				return nil, fmt.Errorf("llm step %q: 连续 3 次失败，终止流水线: %w", step.Name, err)
			}
			if p.onFailure == "fail" {
				return nil, fmt.Errorf("llm step %q: %w", step.Name, err)
			}
			ctx.Warnf("llm step %q 失败: %v，保留原文继续", step.Name, err)
			continue
		}
		consecutiveFailures = 0
		current = result
	}

	// ③ 后处理。
	for _, r := range p.postprocess {
		rm, err := markdown.BuildRegionMap(current)
		if err != nil {
			return nil, fmt.Errorf("postprocess %s: %w", r.Name(), err)
		}
		result, err := r.Transform(ctx.bindRule(r.Name()), rm)
		if err != nil {
			return nil, fmt.Errorf("postprocess %s: %w", r.Name(), err)
		}
		current = result
	}
	return current, nil
}

// Steps 暴露 LLM 步骤（dry-run 统计用）。
func (p *Pipeline) Steps() []*llm.Step { return p.llmSteps }

// PreprocessRules / PostprocessRules 暴露规则列表（dry-run 用）。
func (p *Pipeline) PreprocessRules() []rule.Rule  { return p.preprocess }
func (p *Pipeline) PostprocessRules() []rule.Rule { return p.postprocess }

func joinKeys(m map[string]*config.Profile) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
