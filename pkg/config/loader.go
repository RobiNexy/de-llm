// loader.go 实现设计文档 §3.1/§3.2 的多层级配置查找与合并。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RobiNexy/de-llm/pkg/llm"
	"github.com/RobiNexy/de-llm/pkg/parser"
	"gopkg.in/yaml.v3"
)

// Options 控制 Load 行为。
type Options struct {
	// ConfigPath 为 CLI --config 指定路径（可为空）。
	ConfigPath string
	// NoUserConfig 跳过用户级配置（测试用）。
	NoUserConfig bool
	// UserConfigPath 覆盖默认用户配置路径（测试用）。
	UserConfigPath string
	// WorkDir 覆盖项目查找起点（测试用）。
	WorkDir string
}

// Load 加载并合并配置，返回合并结果。
//
// 查找（§3.1）：--config 优先；否则项目目录向 git 根/主目录逐级向上
// 找 dellm.yaml/.yml、.dellm.yaml/.yml（cwd 优先）；用户级固定
// ~/.config/dellm/config.yaml。合并（§3.2）：内置默认 ← 用户级 ←
// 项目级（← front matter 由调用方再合并）。
func Load(opts Options) (*Config, error) {
	cfg := Default()

	workDir := opts.WorkDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	// 用户级。
	if !opts.NoUserConfig {
		userPath := opts.UserConfigPath
		if userPath == "" {
			home, err := os.UserHomeDir()
			if err == nil {
				userPath = filepath.Join(home, ".config", "dellm", "config.yaml")
			}
		}
		if userPath != "" {
			if data, err := os.ReadFile(userPath); err == nil {
				uc, err := unmarshal(data, userPath)
				if err != nil {
					return nil, err
				}
				cfg.mergeOver(uc)
			}
		}
	}

	// 项目级。
	projectPath := opts.ConfigPath
	if projectPath == "" {
		p, err := findProjectConfig(workDir)
		if err != nil {
			return nil, err
		}
		projectPath = p
	}
	if projectPath != "" {
		data, err := os.ReadFile(projectPath)
		if err != nil {
			return nil, fmt.Errorf("读取配置失败: %w", err)
		}
		pc, err := unmarshal(data, projectPath)
		if err != nil {
			return nil, err
		}
		cfg.mergeOver(pc)
		cfg.SourceFile = projectPath
	}

	expandEnv(cfg)
	cfg.ProfileName = "default"
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// unmarshal 解析 YAML 并做环境变量展开的预处理。
func unmarshal(data []byte, source string) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("配置 %s 解析失败: %w", source, err)
	}
	cfg.SourceFile = source
	return &cfg, nil
}

// findProjectConfig 从 workDir 逐级向上查找项目配置。
// 终止条件：git 根目录（含 .git）或用户主目录。
func findProjectConfig(workDir string) (string, error) {
	home, _ := os.UserHomeDir()
	dir := workDir
	for {
		// cwd 显式列出的文件名优先。
		for _, name := range []string{"dellm.yaml", "dellm.yml", ".dellm.yaml", ".dellm.yml"} {
			p := filepath.Join(dir, name)
			if fileExists(p) {
				return p, nil
			}
		}
		if dir == home || fileExists(filepath.Join(dir, ".git")) {
			return "", nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// mergeOver 将 over 合并到 c（over 优先级更高）。
// 合并规则（§3.2）：
//   - profiles: 整体替换（同名 profile 完全采用 over 的定义）
//   - rules: 深度合并
//   - llm.providers: 深度合并
func (c *Config) mergeOver(over *Config) {
	if over == nil {
		return
	}
	if over.Version != 0 {
		c.Version = over.Version
	}
	if over.Profiles != nil {
		if c.Profiles == nil {
			c.Profiles = map[string]*Profile{}
		}
		for name, p := range over.Profiles {
			// 整体替换：直接采用 over 的 profile 指针。
			cp := *p
			c.Profiles[name] = &cp
		}
	}
	mergeRuleMaps(c.Rules, over.Rules)
	if len(over.LLM.Providers) > 0 {
		if c.LLM.Providers == nil {
			c.LLM.Providers = map[string]llm.ProviderConfig{}
		}
		for name, pc := range over.LLM.Providers {
			base, exists := c.LLM.Providers[name]
			if !exists {
				c.LLM.Providers[name] = pc
				continue
			}
			// 深度合并：非零字段覆盖。
			if pc.Type != "" {
				base.Type = pc.Type
			}
			if pc.BaseURL != "" {
				base.BaseURL = pc.BaseURL
			}
			if pc.APIKey != "" {
				base.APIKey = pc.APIKey
			}
			if pc.Model != "" {
				base.Model = pc.Model
			}
			if pc.Temperature != 0 {
				base.Temperature = pc.Temperature
			}
			if pc.MaxTokens != 0 {
				base.MaxTokens = pc.MaxTokens
			}
			if pc.Timeout != 0 {
				base.Timeout = pc.Timeout
			}
			if pc.MaxRetries != 0 {
				base.MaxRetries = pc.MaxRetries
			}
			c.LLM.Providers[name] = base
		}
	}
	if len(over.LLM.Prompts.Dirs) > 0 {
		c.LLM.Prompts.Dirs = over.LLM.Prompts.Dirs
	}
	if over.LLM.Concurrency != 0 {
		c.LLM.Concurrency = over.LLM.Concurrency
	}
	if over.LLM.RateLimit != "" {
		c.LLM.RateLimit = over.LLM.RateLimit
	}
	if over.LLM.OnFailure != "" {
		c.LLM.OnFailure = over.LLM.OnFailure
	}
	if over.Global.Encoding != "" {
		c.Global.Encoding = over.Global.Encoding
	}
	if over.Global.LogLevel != "" {
		c.Global.LogLevel = over.Global.LogLevel
	}
	if over.SourceFile != "" {
		c.SourceFile = over.SourceFile
	}
}

// mergeRuleMaps 深度合并规则参数：二级 map 逐键覆盖（标量值）。
func mergeRuleMaps(base, over map[string]RuleParams) {
	if over == nil {
		return
	}
	if base == nil {
		base = map[string]RuleParams{}
	}
	for name, params := range over {
		dst, exists := base[name]
		if !exists {
			base[name] = params
			continue
		}
		for k, v := range params {
			dst[k] = v
		}
	}
}

// CloneProfile 返回指定 profile 的深拷贝，供调用方安全修改。
func CloneProfile(c *Config, name string) (*Profile, error) {
	p, ok := c.Profiles[name]
	if !ok {
		return nil, fmt.Errorf("profile %q 不存在（可用: %s）", name, strings.Join(profileNames(c), ", "))
	}
	cp := *p
	cp.Preprocess = append([]string{}, p.Preprocess...)
	cp.Postprocess = append([]string{}, p.Postprocess...)
	cp.LLMSteps = append([]llm.StepConfig{}, p.LLMSteps...)
	return &cp, nil
}

func profileNames(c *Config) []string {
	var out []string
	for k := range c.Profiles {
		out = append(out, k)
	}
	return out
}

// expandEnv 展开 provider api_key 中的 ${VAR}。
// 未设置的变量展开为空串（调用方据此跳过该 provider）。
func expandEnv(c *Config) {
	for name, pc := range c.LLM.Providers {
		if strings.Contains(pc.APIKey, "${") {
			pc.APIKey = os.ExpandEnv(pc.APIKey)
			c.LLM.Providers[name] = pc
		}
	}
}

// ParseFrontMatter 提取文档 front matter 中的 dellm: 配置块（§3.4）。
// 无 front matter 或无 dellm 键时返回 nil, nil。
func ParseFrontMatter(source []byte) (*FrontMatterConfig, error) {
	if fm := parser.DetectFrontMatter(source); fm != nil {
		yamlSrc := source[fm[0]:fm[1]]
		// 去掉首尾分隔线行。
		lines := strings.Split(string(yamlSrc), "\n")
		if len(lines) >= 2 {
			lines = lines[1 : len(lines)-1]
		}
		var doc struct {
			Dellm *FrontMatterConfig `yaml:"dellm"`
		}
		if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &doc); err != nil {
			return nil, fmt.Errorf("front matter YAML 解析失败: %w", err)
		}
		return doc.Dellm, nil
	}
	return nil, nil
}
