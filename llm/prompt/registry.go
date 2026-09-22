// registry.go 实现提示词注册表与内置提示词加载（设计文档 §8.4/§8.5）。
package prompt

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// Registry 保存所有已加载的提示词；同名覆盖，后加载者优先。
type Registry struct {
	prompts map[string]*Prompt
}

func NewRegistry() *Registry {
	return &Registry{prompts: map[string]*Prompt{}}
}

// Register 注册一个提示词；同名覆盖已有条目。
func (r *Registry) Register(p *Prompt) error {
	if p.Template == nil {
		return fmt.Errorf("prompt %q 模板为空", p.Name)
	}
	if p.Name == "" {
		return fmt.Errorf("prompt 缺少 name")
	}
	r.prompts[p.Name] = p
	return nil
}

// Get 按名称查找。
func (r *Registry) Get(name string) (*Prompt, bool) {
	p, ok := r.prompts[name]
	return p, ok
}

// List 返回全部提示词（按名称排序）。
func (r *Registry) List() []*PromptInfo {
	out := make([]*PromptInfo, 0, len(r.prompts))
	for _, p := range r.prompts {
		out = append(out, &PromptInfo{Name: p.Name, Description: p.Description, Source: p.Source})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// LoadBuiltin 加载 go:embed 的内置提示词。
func (r *Registry) LoadBuiltin(builtinFS embed.FS) error {
	return fs.WalkDir(builtinFS, "builtin", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := builtinFS.ReadFile(path)
		if err != nil {
			return err
		}
		p, err := Parse(string(data))
		if err != nil {
			return fmt.Errorf("内置提示词 %s: %w", path, err)
		}
		p.Source = "[builtin]"
		return r.Register(p)
	})
}

// LoadDirs 按目录顺序加载外部提示词。
//
// 覆盖优先级（设计文档 §8.4）：dirs 中越靠前优先级越高。
// 实现为倒序加载（后加载覆盖先加载），使先扫描的目录最终生效。
func (r *Registry) LoadDirs(dirs []string) error {
	for i := len(dirs) - 1; i >= 0; i-- {
		dir := dirs[i]
		if dir == "" {
			continue
		}
		if err := r.loadDir(dir); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) loadDir(dir string) error {
	entries, err := fs.ReadDir(osFS{}, dir)
	if err != nil {
		// 目录不存在不是错误：用户可能未创建提示词目录。
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := readFile(path)
		if err != nil {
			return fmt.Errorf("读取提示词 %s: %w", path, err)
		}
		p, err := Parse(string(data))
		if err != nil {
			return fmt.Errorf("提示词 %s: %w", path, err)
		}
		p.Source = path
		if err := r.Register(p); err != nil {
			return err
		}
	}
	return nil
}

// Parse 解析提示词文件：YAML front matter + text/template 正文。
func Parse(content string) (*Prompt, error) {
	fm, body := splitFrontMatter(content)
	if fm == "" {
		return nil, fmt.Errorf("缺少 YAML front matter")
	}
	var meta struct {
		Name        string          `yaml:"name"`
		Description string          `yaml:"description"`
		Version     int             `yaml:"version"`
		Params      ParamSpec       `yaml:"params"`
		Suggested   SuggestedParams `yaml:"suggested"`
	}
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		return nil, fmt.Errorf("front matter 解析失败: %w", err)
	}
	if meta.Name == "" {
		return nil, fmt.Errorf("缺少 name")
	}
	tmpl, err := template.New(meta.Name).Parse(body)
	if err != nil {
		return nil, fmt.Errorf("模板解析失败: %w", err)
	}
	return &Prompt{
		Name:        meta.Name,
		Description: meta.Description,
		Version:     meta.Version,
		Params:      meta.Params,
		Suggested:   meta.Suggested,
		Template:    tmpl,
	}, nil
}

// splitFrontMatter 分离 front matter 与正文；无 front matter 返回 ("", 原文)。
func splitFrontMatter(content string) (string, string) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return "", content
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" || strings.TrimRight(lines[i], "\r") == "..." {
			return strings.Join(lines[1:i], "\n"), strings.Join(lines[i+1:], "\n")
		}
	}
	return "", content
}
