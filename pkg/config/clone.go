// clone.go provides a deep-enough copy of Config for per-file processing.
// Front matter and CLI shortcuts must never mutate the shared configuration
// loaded once at process start.
package config

import "github.com/RobiNexy/de-llm/pkg/llm"

// Clone returns an independent copy of c. Maps and slices reachable from the
// copy can be mutated without affecting c; pointer-valued rule settings are
// copied as values.
func Clone(c *Config) *Config {
	if c == nil {
		return nil
	}
	out := *c
	out.Rules = cloneRuleMap(c.Rules)
	out.Profiles = make(map[string]*Profile, len(c.Profiles))
	for name, p := range c.Profiles {
		out.Profiles[name] = cloneProfile(p)
	}
	out.LLM = c.LLM
	out.LLM.Providers = make(map[string]llm.ProviderConfig, len(c.LLM.Providers))
	for name, pc := range c.LLM.Providers {
		out.LLM.Providers[name] = pc
	}
	out.LLM.Prompts.Dirs = append([]string(nil), c.LLM.Prompts.Dirs...)
	return &out
}

func cloneProfile(p *Profile) *Profile {
	if p == nil {
		return nil
	}
	out := *p
	out.Preprocess = append([]string(nil), p.Preprocess...)
	out.Postprocess = append([]string(nil), p.Postprocess...)
	out.LLMSteps = make([]llm.StepConfig, len(p.LLMSteps))
	for i := range p.LLMSteps {
		out.LLMSteps[i] = cloneStep(p.LLMSteps[i])
	}
	return &out
}

func cloneStep(s llm.StepConfig) llm.StepConfig {
	out := s
	if s.Filter != nil {
		f := *s.Filter
		f.Level = append([]int(nil), s.Filter.Level...)
		out.Filter = &f
	}
	if s.Params != nil {
		out.Params = make(map[string]any, len(s.Params))
		for k, v := range s.Params {
			out.Params[k] = v
		}
	}
	if s.Overrides.Temperature != nil {
		v := *s.Overrides.Temperature
		out.Overrides.Temperature = &v
	}
	if s.Overrides.MaxTokens != nil {
		v := *s.Overrides.MaxTokens
		out.Overrides.MaxTokens = &v
	}
	if s.Overrides.Timeout != nil {
		v := *s.Overrides.Timeout
		out.Overrides.Timeout = &v
	}
	return out
}

func cloneRuleMap(in map[string]RuleParams) map[string]RuleParams {
	if in == nil {
		return nil
	}
	out := make(map[string]RuleParams, len(in))
	for name, params := range in {
		cp := make(RuleParams, len(params))
		for k, v := range params {
			cp[k] = v
		}
		out[name] = cp
	}
	return out
}
