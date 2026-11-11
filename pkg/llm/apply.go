// apply.go 实现设计文档 §7.5 的回贴逻辑：
// id + prefix 双重校验，按原文位置从后向前替换。
package llm

import (
	"fmt"
	"sort"
	"strings"

	markdown "github.com/RobiNexy/de-llm/pkg/parser"
)

// Warning 是一次回贴/解析阶段产生的非致命问题。
type Warning struct {
	NodeID  string
	Message string
}

func (w Warning) String() string {
	if w.NodeID != "" {
		return fmt.Sprintf("[%s] %s", w.NodeID, w.Message)
	}
	return w.Message
}

// ValidateMatches 校验检测轮返回的 id/prefix（设计文档 §7.4.3）：
// id 不存在、prefix 不匹配 → 跳过并产生 warning。
func ValidateMatches(doc *markdown.AnnotatedDoc, matches []DetectMatch) ([]*markdown.AnnotatedNode, []Warning) {
	var warnings []Warning
	var nodes []*markdown.AnnotatedNode
	seen := map[string]bool{}
	for _, m := range matches {
		node, ok := doc.NodeByID[m.ID]
		if !ok {
			warnings = append(warnings, Warning{NodeID: m.ID, Message: "节点不存在，跳过"})
			continue
		}
		if seen[m.ID] {
			continue
		}
		if !prefixMatches(node.Prefix, m.Prefix) {
			warnings = append(warnings, Warning{NodeID: m.ID, Message: fmt.Sprintf("prefix 不匹配（期望 %q，实际 %q）", m.Prefix, node.Prefix)})
			continue
		}
		seen[m.ID] = true
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Start < nodes[j].Start })
	return nodes, warnings
}

// prefixMatches 双向前缀比较（归一化换行后）：
// LLM 可能返回比 15 字符更短或更长的前缀，双向容忍降低误拒。
// 空前缀不接受：它会让校验退化为仅检查 id。
func prefixMatches(expected, got string) bool {
	if got == "" {
		return false
	}
	e, g := markdown.NormalizePrefix(expected), markdown.NormalizePrefix(got)
	return strings.HasPrefix(e, g) || strings.HasPrefix(g, e)
}

// validateRewrite 校验回贴文本是否保持节点结构。
// 标题是最容易被 LLM 改坏的结构：ATX 标题必须保留相同数量的 #，
// Setext 标题不能被改写成 ATX。
func validateRewrite(node *markdown.AnnotatedNode, text string) error {
	if node.Type != "heading" {
		return nil
	}
	if strings.ContainsAny(text, "\r\n") {
		return fmt.Errorf("标题改写不能包含换行")
	}
	originalATX := strings.HasPrefix(node.Text, "#")
	rewriteATX := strings.HasPrefix(text, "#")
	if !originalATX {
		if rewriteATX {
			return fmt.Errorf("Setext 标题不能改写为 ATX 标题")
		}
		return nil
	}
	level := 0
	for level < len(text) && text[level] == '#' {
		level++
	}
	if level != node.Level {
		return fmt.Errorf("标题级别不匹配（期望 %d 个 #，实际 %d 个）", node.Level, level)
	}
	if level < len(text) && text[level] != ' ' && text[level] != '\t' {
		return fmt.Errorf("标题 # 后必须是空格或行尾")
	}
	return nil
}

// ApplyRewrites 将改写结果回贴到原文（设计文档 §7.5）：
//   - 未知 id / prefix 为空或不匹配 / 空 text / 结构破坏 → 跳过 + warning
//   - 从后向前替换，保证前面替换不影响后面节点偏移
//
// 返回值 applied 是实际成功回贴的节点数，stats 应使用它而不是原始响应条数。
func ApplyRewrites(doc *markdown.AnnotatedDoc, rewrites []Rewrite) ([]byte, int, []Warning) {
	var warnings []Warning

	// 过滤无效项并按原文位置降序。
	type validRewrite struct {
		node *markdown.AnnotatedNode
		text string
	}
	var valids []validRewrite
	seen := map[string]bool{}
	for _, rw := range rewrites {
		node, ok := doc.NodeByID[rw.ID]
		if !ok {
			warnings = append(warnings, Warning{NodeID: rw.ID, Message: "节点不存在，跳过"})
			continue
		}
		if seen[rw.ID] {
			warnings = append(warnings, Warning{NodeID: rw.ID, Message: "重复改写，跳过后到的"})
			continue
		}
		if strings.TrimSpace(rw.Text) == "" {
			warnings = append(warnings, Warning{NodeID: rw.ID, Message: "text 为空，跳过"})
			continue
		}
		if !prefixMatches(node.Prefix, rw.Prefix) {
			warnings = append(warnings, Warning{NodeID: rw.ID,
				Message: fmt.Sprintf("prefix 为空或不匹配（期望 %q，实际 %q）", node.Prefix, rw.Prefix)})
			continue
		}
		if err := validateRewrite(node, rw.Text); err != nil {
			warnings = append(warnings, Warning{NodeID: rw.ID, Message: err.Error()})
			continue
		}
		seen[rw.ID] = true
		valids = append(valids, validRewrite{node: node, text: rw.Text})
	}
	sort.Slice(valids, func(i, j int) bool { return valids[i].node.Start > valids[j].node.Start })

	result := append([]byte{}, doc.Source...)
	for _, v := range valids {
		result = append(result[:v.node.Start],
			append([]byte(v.text), result[v.node.End:]...)...)
	}
	return result, len(valids), warnings
}
