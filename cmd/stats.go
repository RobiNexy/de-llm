// stats.go 输出处理统计（设计文档 §10.3）。
package cmd

import (
	"fmt"
	"strings"

	"github.com/RobiNexy/de-llm/pipeline"
)

// formatStats 渲染单文件的统计块。
func formatStats(path string, ctx *pipeline.Context, p *pipeline.Pipeline) string {
	var sb strings.Builder
	sb.WriteString("── 处理统计 ──────────────────────────\n")
	fmt.Fprintf(&sb, "文件: %s\n\n", path)

	stats := ctx.Stats()

	sb.WriteString("预处理:\n")
	for _, r := range p.PreprocessRules() {
		writeRuleStats(&sb, r.Name(), stats)
	}

	sb.WriteString("\nLLM 步骤:\n")
	for _, step := range p.Steps() {
		fmt.Fprintf(&sb, "  %q:", step.Name)
		if step.NodesDetected > 0 {
			fmt.Fprintf(&sb, " 处理 %d 个节点，成功 %d", step.NodesDetected, step.NodesRewritten)
		}
		sb.WriteString("\n")
		if step.Usage.TotalTokens > 0 {
			fmt.Fprintf(&sb, "    Token: %d (输入) + %d (输出), 缓存命中: %d\n",
				step.Usage.PromptTokens, step.Usage.CompletionTokens, step.Usage.CachedTokens)
		}
	}

	sb.WriteString("\n后处理:\n")
	for _, r := range p.PostprocessRules() {
		writeRuleStats(&sb, r.Name(), stats)
	}
	sb.WriteString("──────────────────────────────────────\n")
	return sb.String()
}

// writeRuleStats 输出单条规则的计数（如 "替换 24 处引号"）。
func writeRuleStats(sb *strings.Builder, name string, stats map[string]map[string]int) {
	m := stats[name]
	if len(m) == 0 {
		return
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%s %d 处", statLabel(k), v))
	}
	fmt.Fprintf(sb, "  %s: %s\n", name, strings.Join(parts, ", "))
}

// statLabel 将统计键翻译为中文动词。
func statLabel(key string) string {
	switch key {
	case "replaced":
		return "替换"
	case "inserted":
		return "插入"
	case "removed":
		return "删除"
	case "shifted":
		return "调整"
	case "indented":
		return "缩进"
	}
	return key
}
