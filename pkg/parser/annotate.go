// annotate.go 实现设计文档 §7.2 的文档标注：
// 为可寻址节点分配 [Pn]/[Hn]/[Ln]/[Qn] 编号，生成发给 LLM 的带标注文本。
// 标注只用于 LLM 输入，最终输出不包含标注。
package parser

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// 节点类型常量，与配置中 target 字段对应。
const (
	TargetParagraph  = "paragraph"
	TargetHeading    = "heading"
	TargetListItem   = "list_item"
	TargetBlockquote = "blockquote"
)

// AnnotatedNode 是一个可寻址节点。
type AnnotatedNode struct {
	ID     string // "P1", "H3" 等
	Type   string // paragraph / heading / list_item / blockquote
	Start  int    // 原文中的起始字节偏移
	End    int    // 原文中的结束字节偏移
	Text   string // 节点原始文本
	Prefix string // 前 15 个 UTF-8 字符（用于匹配校验，忽略换行）
	Level  int    // 仅 heading
}

// AnnotatedDoc 是标注产物。
type AnnotatedDoc struct {
	Source    []byte
	Annotated []byte
	Nodes     []AnnotatedNode
	NodeByID  map[string]*AnnotatedNode
}

// prefixLen 是 prefix 校验字符串的最大可见字符数（设计文档 §7.2）。
const prefixLen = 15

// AnnotateDocument 对 source 中指定类型的节点做标注。
//
// Heading 的节点范围从 '#' 字符开始（含 ATX 标记），改写回贴时整行替换；
// 其余类型从节点首段文本开始（不含列表/引用标记符），回贴时只替换文本。
func AnnotateDocument(source []byte, target string) *AnnotatedDoc {
	doc := &AnnotatedDoc{
		Source:   source,
		NodeByID: map[string]*AnnotatedNode{},
	}
	root := Parse(source)

	counters := map[string]int{}
	var nodes []AnnotatedNode

	kindFor := func(n ast.Node) (kind, id string, ok bool) {
		switch n.(type) {
		case *ast.Paragraph:
			if target != TargetParagraph {
				return "", "", false
			}
			// 列表项与引用块内的段落归其容器（Ln/Qn），不重复作为段落标注。
			for p := n.Parent(); p != nil; p = p.Parent() {
				switch p.(type) {
				case *ast.ListItem, *ast.Blockquote:
					return "", "", false
				}
			}
			return TargetParagraph, "P", true
		case *ast.Heading:
			if target != TargetHeading {
				return "", "", false
			}
			return TargetHeading, "H", true
		case *ast.ListItem:
			if target != TargetListItem {
				return "", "", false
			}
			return TargetListItem, "L", true
		case *ast.Blockquote:
			if target != TargetBlockquote {
				return "", "", false
			}
			return TargetBlockquote, "Q", true
		}
		return "", "", false
	}

	ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		kind, prefix, ok := kindFor(n)
		if !ok {
			return ast.WalkContinue, nil
		}
		start, end, level := nodeExtent(n, source, kind)
		if start < 0 || end <= start {
			return ast.WalkContinue, nil
		}
		counters[prefix]++
		id := fmt.Sprintf("%s%d", prefix, counters[prefix])
		text := string(source[start:end])
		nodeInfo := AnnotatedNode{
			ID:     id,
			Type:   kind,
			Start:  start,
			End:    end,
			Text:   text,
			Prefix: visiblePrefix(text, prefixLen),
			Level:  level,
		}
		nodes = append(nodes, nodeInfo)
		return ast.WalkSkipChildren, nil
	})

	doc.Nodes = nodes
	for i := range nodes {
		doc.NodeByID[nodes[i].ID] = &nodes[i]
	}
	doc.Annotated = renderAnnotated(source, nodes)
	return doc
}

// nodeExtent 返回节点的 [start,end) 与 heading 级别。
func nodeExtent(n ast.Node, source []byte, kind string) (start, end, level int) {
	switch node := n.(type) {
	case *ast.Heading:
		// ATX 标题：节点范围含 '#...' 标记；Setext 标题取文本行。
		segs := node.Lines()
		if segs.Len() == 0 {
			return -1, -1, node.Level
		}
		textStart := segs.At(0).Start
		lineStart := textStart
		for lineStart > 0 && source[lineStart-1] != '\n' {
			lineStart--
		}
		s := lineStart
		// 只有当该行以 # 开头（ATX）时才把标记纳入节点范围。
		i := lineStart
		for i < len(source) && (source[i] == ' ' || source[i] == '\t') {
			i++
		}
		if i < len(source) && source[i] == '#' {
			s = i
		}
		end := segs.At(segs.Len() - 1).Stop
		// 包含行尾 \r（若有）。
		if end < len(source) && source[end] == '\r' {
			end++
		}
		return s, end, node.Level
	case *ast.Paragraph:
		segs := node.Lines()
		if segs.Len() == 0 {
			return -1, -1, 0
		}
		return segs.At(0).Start, segs.At(segs.Len() - 1).Stop, 0
	case *ast.ListItem:
		// 取列表项内第一个块级子节点的文本范围（不含 "- "/"1. " 标记）。
		for c := node.FirstChild(); c != nil; c = c.NextSibling() {
			if para, ok := c.(*ast.Paragraph); ok {
				segs := para.Lines()
				if segs.Len() > 0 {
					return segs.At(0).Start, segs.At(segs.Len() - 1).Stop, 0
				}
			}
		}
		return -1, -1, 0
	case *ast.Blockquote:
		for c := node.FirstChild(); c != nil; c = c.NextSibling() {
			if para, ok := c.(*ast.Paragraph); ok {
				segs := para.Lines()
				if segs.Len() > 0 {
					return segs.At(0).Start, segs.At(segs.Len() - 1).Stop, 0
				}
			}
		}
		return -1, -1, 0
	}
	return -1, -1, 0
}

// renderAnnotated 在每个节点起始处插入 [ID] 标注前缀。
func renderAnnotated(source []byte, nodes []AnnotatedNode) []byte {
	var buf bytes.Buffer
	prev := 0
	for _, n := range nodes {
		if n.Start < prev {
			continue
		}
		buf.Write(source[prev:n.Start])
		fmt.Fprintf(&buf, "[%s] ", n.ID)
		buf.Write(source[n.Start:n.End])
		prev = n.End
	}
	buf.Write(source[prev:])
	return buf.Bytes()
}

// visiblePrefix 取前 n 个 UTF-8 可见字符，跳过换行（设计文档 R9）。
// 返回值与节点文本的对应片段可能不完全一致（换行被剥离），
// 因此校验阶段统一对两侧做相同的换行剥离后再比较。
func visiblePrefix(s string, n int) string {
	var b strings.Builder
	count := 0
	for _, r := range s {
		if r == '\n' || r == '\r' {
			continue
		}
		b.WriteRune(r)
		count++
		if count == n {
			return b.String()
		}
	}
	return b.String()
}

// NormalizePrefix 剥离换行，供 prefix 校验两侧统一处理。
func NormalizePrefix(s string) string {
	return strings.NewReplacer("\n", "", "\r", "").Replace(s)
}

// FormatTargets 生成 target_nodes 变量：节点前 50 个可见字符（设计文档 §7.8）。
func FormatTargets(nodes []*AnnotatedNode) string {
	var buf bytes.Buffer
	for _, n := range nodes {
		fmt.Fprintf(&buf, "[%s] %s\n", n.ID, visiblePrefix(n.Text, 50))
	}
	return buf.String()
}
