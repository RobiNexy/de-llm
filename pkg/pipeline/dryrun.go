// dryrun.go 实现设计文档 §9.4 的 --dry-run 报告与 token 预估。
package pipeline

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/RobiNexy/de-llm/pkg/config"
	"github.com/RobiNexy/de-llm/pkg/llm"
	markdown "github.com/RobiNexy/de-llm/pkg/parser"
)

// DryRunReport 输出将执行的操作与 token 预估，不实际处理。
func DryRunReport(cfg *config.Config, profileName string, source []byte, p *Pipeline, sourceFile string) string {
	var sb strings.Builder
	src := cfg.SourceFile
	if src == "" {
		src = "[内置默认]"
	}
	fmt.Fprintf(&sb, "配置: %s (profile: %s)\n", src, profileName)
	if sourceFile == "-" {
		sourceFile = "<stdin>"
	}
	fmt.Fprintf(&sb, "输入: %s (%s, %d 段落, %d 标题)\n\n",
		sourceFile, humanSize(len(source)), countParas(source), countHeadings(source))

	sb.WriteString("预处理:\n")
	for i, r := range p.PreprocessRules() {
		fmt.Fprintf(&sb, "  [%d] %s\n", i+1, r.Name())
	}

	if len(p.Steps()) == 0 {
		sb.WriteString("\nLLM 步骤: (无，纯本地规则处理)\n")
	} else {
		sb.WriteString("\nLLM 步骤:\n")
		totalIn, totalOut := 0, 0
		for i, step := range p.Steps() {
			fmt.Fprintf(&sb, "  [%d] %q (%s", i+1, step.Name, step.Action)
			if step.Action == llm.ActionRewrite {
				if step.Detect {
					sb.WriteString(", detect=true")
				} else {
					sb.WriteString(", detect=false")
				}
			}
			sb.WriteString(")\n")

			est := estimateStep(step, source)
			fmt.Fprintf(&sb, "      预估 token: %s\n", est.describe())
			totalIn += est.in
			totalOut += est.out
		}
		fmt.Fprintf(&sb, "\n  总预估 token: %s\n", fmtTokens(totalIn+totalOut))
	}

	sb.WriteString("\n后处理:\n")
	for i, r := range p.PostprocessRules() {
		fmt.Fprintf(&sb, "  [%d] %s\n", i+1, r.Name())
	}
	return sb.String()
}

type tokenEstimate struct {
	in, out int
	note    string
}

func (e tokenEstimate) describe() string {
	s := fmt.Sprintf("~%s (输入) + ~%s (输出)", fmtTokens(e.in), fmtTokens(e.out))
	if e.note != "" {
		s += "  [" + e.note + "]"
	}
	return s
}

// estimateStep 按设计文档 §9.4 的估算方法：
//   - 中文 1 字符 ≈ 1.5 token；英文 4 字符 ≈ 1 token
//   - full_rewrite 输出按 expand_ratio 倍计算（默认 1.3）
//   - rewrite 检测轮输出按输入的 5%
//   - rewrite 改写轮输出按目标节点总字符数的 1.5 倍
func estimateStep(step *llm.Step, source []byte) tokenEstimate {
	in := estimateTokens(source)
	switch step.Action {
	case llm.ActionFullRewrite:
		ratio := 1.3
		if step.Params != nil {
			if v, ok := step.Params["expand_ratio"].(string); ok {
				if f, err := parseRatio(v); err == nil {
					ratio = f
				}
			}
		}
		return tokenEstimate{in: in, out: int(float64(in) * ratio)}
	case llm.ActionRewrite:
		// 目标节点数量与字符数：按 target 类型标注统计。
		target := step.Target
		annotated := markdown.AnnotateDocument(source, target)
		filtered := applyFilterPublic(step, annotated.Nodes)
		targetChars := 0
		for _, n := range filtered {
			targetChars += utf8.RuneCountInString(n.Text)
		}
		est := tokenEstimate{in: in}
		if step.Detect {
			// 两轮调用：标注原文发送两次（prefix cache 命中率高）。
			est.in += in
			est.out = in / 20
			est.out += int(float64(targetChars) * 1.5 * estTokensPerChar(source))
			est.note = "两轮，prefix cache 命中率高"
		} else {
			est.out = int(float64(targetChars) * 1.5 * estTokensPerChar(source))
			est.note = "filter 筛出 " + itoa(len(filtered)) + " 个节点"
		}
		return est
	}
	return tokenEstimate{in: in, out: in}
}

func applyFilterPublic(step *llm.Step, nodes []markdown.AnnotatedNode) []*markdown.AnnotatedNode {
	return llm.ApplyFilter(nodes, step.Filter)
}

// estTokensPerChar 根据文本的中英比例估算平均每字符 token 数。
func estTokensPerChar(source []byte) float64 {
	total, cjk := 0, 0
	for _, r := range string(source) {
		if r == '\n' {
			continue
		}
		total++
		if isCJK(r) {
			cjk++
		}
	}
	if total == 0 {
		return 1.0
	}
	ratio := float64(cjk) / float64(total)
	// 中文 1.5 token/字符，英文 0.25 token/字符，按比例插值。
	return ratio*1.5 + (1-ratio)*0.25
}

// estimateTokens 直接估算整段文本的 token 数。
func estimateTokens(source []byte) int {
	return int(float64(utf8.RuneCount(source)) * estTokensPerChar(source))
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0xF900 && r <= 0xFAFF) || (r >= 0x3000 && r <= 0x303F) ||
		(r >= 0xFF00 && r <= 0xFFEF)
}

func parseRatio(s string) (float64, error) {
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		return 0, err
	}
	if f <= 0 {
		return 0, fmt.Errorf("ratio 必须为正")
	}
	return f, nil
}

func countParas(source []byte) int {
	rm, err := markdown.BuildRegionMap(source)
	if err != nil {
		return 0
	}
	return len(rm.ParaStarts)
}

func countHeadings(source []byte) int {
	rm, err := markdown.BuildRegionMap(source)
	if err != nil {
		return 0
	}
	n := 0
	for _, r := range rm.Regions {
		if r.Type == markdown.RegionHeading {
			n++
		}
	}
	return n
}

func humanSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/1024/1024)
	}
}

func fmtTokens(n int) string {
	s := itoa(n)
	if n >= 1000 {
		s = itoa(n/1000) + "," + pad3(n%1000)
	}
	return s
}

func pad3(n int) string {
	if n < 10 {
		return "00" + itoa(n)
	}
	if n < 100 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
