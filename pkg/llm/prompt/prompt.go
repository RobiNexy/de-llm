// Package prompt 实现设计文档 §8 的提示词系统：
// Markdown 文件 + YAML front matter，text/template 渲染。
package prompt

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// ParamDef 是可选参数定义。
type ParamDef struct {
	Default     any    `yaml:"default"`
	Description string `yaml:"description"`
}

// ParamSpec 是参数规格。
type ParamSpec struct {
	Required []string            `yaml:"required"`
	Optional map[string]ParamDef `yaml:"optional"`
}

// SuggestedParams 是建议的 provider 参数。
type SuggestedParams struct {
	Temperature    *float64 `yaml:"temperature"`
	ResponseFormat string   `yaml:"response_format"` // text | json
}

// Prompt 是一个已解析的提示词模板。
type Prompt struct {
	Name        string
	Description string
	Version     int
	Params      ParamSpec
	Suggested   SuggestedParams
	Template    *template.Template
	Source      string // 文件路径或 "[builtin]"
}

// Render 按变量注入优先级渲染（设计文档 §8.3）：
// optional 默认值 < 自动注入变量 < step params（由调用方合并后传入）。
// 必填变量缺失时报错。
func (p *Prompt) Render(vars map[string]any) (string, error) {
	merged := map[string]any{}
	for k, def := range p.Params.Optional {
		merged[k] = def.Default
	}
	for k, v := range vars {
		merged[k] = v
	}
	var missing []string
	for _, req := range p.Params.Required {
		if v, ok := merged[req]; !ok || v == nil || v == "" {
			missing = append(missing, req)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("prompt %q 缺少必填变量: %s", p.Name, strings.Join(missing, ", "))
	}
	var buf bytes.Buffer
	if err := p.Template.Execute(&buf, merged); err != nil {
		return "", fmt.Errorf("prompt %q 渲染失败: %w", p.Name, err)
	}
	return buf.String(), nil
}

// PromptInfo 是 list-prompts 输出的信息。
type PromptInfo struct {
	Name        string
	Description string
	Source      string
}
