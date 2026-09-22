// process.go 实现单文件的处理流程：front matter 合并、
// --only/--skip/--skip-llm/--shift-headings 过滤、流水线执行与统计。
//
// 过滤参数按文件独立计算（front matter 可覆盖），不修改全局 flag，
// 避免多文件处理时的跨文件污染。
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/RobiNexy/de-llm/config"
	"github.com/RobiNexy/de-llm/llm"
	"github.com/RobiNexy/de-llm/pipeline"
)

// filterSpec 是一次处理生效的过滤参数（CLI + front matter 合并结果）。
type filterSpec struct {
	profile       string
	only          []string
	skip          []string
	skipLLM       bool
	shiftHeadings int
}

// processFile 读取文件并处理。
func processFile(cfg *config.Config, profileName, path string) (*fileResult, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取失败: %w", err)
	}
	result, err := processBytes(cfg, profileName, source, path)
	if err != nil {
		return nil, err
	}
	return &fileResult{output: result, orig: source, path: path}, nil
}

// processBytes 对一份文档执行完整流程。
func processBytes(cfg *config.Config, profileName string, source []byte, displayPath string) ([]byte, error) {
	logf := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "[dellm] "+format+"\n", args...)
	}

	spec := filterSpec{
		profile:       profileName,
		only:          splitList(flagOnly),
		skip:          splitList(flagSkip),
		skipLLM:       flagSkipLLM,
		shiftHeadings: flagShiftHeadings,
	}

	// front matter 覆盖（设计文档 §3.4）。
	fmCfg, err := config.ParseFrontMatter(source)
	if err != nil {
		return nil, failErr("%s: %v", displayPath, err)
	}
	if fmCfg != nil {
		if fmCfg.Profile != "" {
			spec.profile = fmCfg.Profile
		}
		spec.skip = append(spec.skip, fmCfg.Skip...)
		if len(fmCfg.Only) > 0 {
			if len(spec.only) > 0 {
				return nil, failErr("%s: front matter 指定 only 与 CLI --only 冲突", displayPath)
			}
			spec.only = fmCfg.Only
		}
		for name, params := range fmCfg.Rules {
			if cfg.Rules == nil {
				cfg.Rules = map[string]config.RuleParams{}
			}
			if cfg.Rules[name] == nil {
				cfg.Rules[name] = config.RuleParams{}
			}
			for k, v := range params {
				cfg.Rules[name][k] = v
			}
		}
	}

	profCopy, err := config.CloneProfile(cfg, spec.profile)
	if err != nil {
		return nil, cfgErr("%s: %v", displayPath, err)
	}
	if spec.skipLLM {
		profCopy.LLMSteps = nil
	}
	if spec.shiftHeadings != 0 {
		applyShiftHeadingsShortcut(profCopy, cfg, spec.shiftHeadings)
	}
	applyOnlySkip(profCopy, spec.only, spec.skip, logf)

	runCfg := *cfg
	runCfg.Profiles = map[string]*config.Profile{spec.profile: profCopy}

	p, err := pipeline.Build(&runCfg, spec.profile, pipeline.BuilderOptions{Verbose: flagVerbose})
	if err != nil {
		return nil, cfgErr("%s: %v", displayPath, err)
	}

	if flagDryRun {
		fmt.Fprint(os.Stderr, pipeline.DryRunReport(&runCfg, spec.profile, source, p, displayPath))
		return source, nil
	}

	ctx := pipeline.NewContext(nil, flagVerbose)
	result, err := p.Process(ctx, source)
	if err != nil {
		return nil, failErr("%s: %v", displayPath, err)
	}

	for _, w := range ctx.Warnings {
		logf("警告: %s", w)
	}
	if flagStats {
		fmt.Fprint(os.Stderr, formatStats(displayPath, ctx, p))
	}
	return result, nil
}

// applyShiftHeadingsShortcut 实现 --shift-headings=N（设计文档 §4.3）：
// preprocess 最前面加入 shift_headings（若无）并设置 offset。
func applyShiftHeadingsShortcut(prof *config.Profile, cfg *config.Config, offset int) {
	found := false
	for _, name := range prof.Preprocess {
		if name == "shift_headings" {
			found = true
			break
		}
	}
	if !found {
		prof.Preprocess = append([]string{"shift_headings"}, prof.Preprocess...)
	}
	if cfg.Rules == nil {
		cfg.Rules = map[string]config.RuleParams{}
	}
	if cfg.Rules["shift_headings"] == nil {
		cfg.Rules["shift_headings"] = config.RuleParams{}
	}
	cfg.Rules["shift_headings"]["offset"] = offset
}

// applyOnlySkip 过滤规则与 LLM 步骤（设计文档 §4.2）：
//   - 精确匹配规则名或 step 的 name 字段，大小写不敏感
//   - 未匹配任何规则/步骤时输出 warning 但不报错
func applyOnlySkip(prof *config.Profile, only, skip []string, logf func(string, ...any)) {
	// 过滤前先收集全集，供未匹配 warning 使用。
	original := lowerSet(allProfileNames(prof))

	onlySet := lowerSet(only)
	skipSet := lowerSet(skip)

	switch {
	case len(onlySet) > 0:
		matched := filterProfile(prof, func(name string) bool {
			return onlySet[strings.ToLower(name)]
		})
		if missing := diffKeys(onlySet, matched); len(missing) > 0 {
			logf("警告: --only 未匹配到规则/步骤: %s", strings.Join(missing, ", "))
		}
	case len(skipSet) > 0:
		// skip：保留未匹配项；匹配数 = skipSet ∩ 原全集。
		filterProfile(prof, func(name string) bool {
			return !skipSet[strings.ToLower(name)]
		})
		matchedSkip := map[string]bool{}
		for k := range skipSet {
			if original[k] {
				matchedSkip[k] = true
			}
		}
		if missing := diffKeys(skipSet, matchedSkip); len(missing) > 0 {
			logf("警告: --skip 未匹配到规则/步骤: %s", strings.Join(missing, ", "))
		}
	}
}

// allProfileNames 收集 profile 中的全部规则/步骤名。
func allProfileNames(prof *config.Profile) []string {
	out := make([]string, 0, len(prof.Preprocess)+len(prof.Postprocess)+len(prof.LLMSteps))
	out = append(out, prof.Preprocess...)
	out = append(out, prof.Postprocess...)
	for _, s := range prof.LLMSteps {
		out = append(out, s.Name)
	}
	return out
}

// filterProfile 按 keep 谓词过滤 profile 的三段列表，返回匹配项集合。
func filterProfile(prof *config.Profile, keep func(string) bool) map[string]bool {
	matched := map[string]bool{}
	filterSlice := func(names []string) []string {
		out := make([]string, 0, len(names))
		for _, n := range names {
			if keep(n) {
				matched[strings.ToLower(n)] = true
				out = append(out, n)
			}
		}
		return out
	}
	prof.Preprocess = filterSlice(prof.Preprocess)
	prof.Postprocess = filterSlice(prof.Postprocess)
	steps := make([]llm.StepConfig, 0, len(prof.LLMSteps))
	for _, s := range prof.LLMSteps {
		if keep(s.Name) {
			matched[strings.ToLower(s.Name)] = true
			steps = append(steps, s)
		}
	}
	prof.LLMSteps = steps
	return matched
}

func lowerSet(list []string) map[string]bool {
	s := map[string]bool{}
	for _, v := range list {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			s[v] = true
		}
	}
	return s
}

func diffKeys(want, matched map[string]bool) []string {
	var missing []string
	for k := range want {
		if !matched[k] {
			missing = append(missing, k)
		}
	}
	return missing
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
