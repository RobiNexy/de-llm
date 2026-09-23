// Package markdown 封装 goldmark 解析与区域映射。
//
// 设计文档 §5：goldmark 的 AST 节点通过字节偏移引用原始 source，
// 修改文本会使偏移失效，因此 AST 仅用于结构识别；
// 字符级规则基于 RegionMap 对原始文本操作。
//
// 实现说明：区域划分采用"按字节分类数组"而非区间树——
// 文档规模（几十 KB）下 O(n) 内存的代价可忽略，换来的是
// 嵌套区间（如引用块内列表、嵌套强调）无需重叠消解逻辑，
// 最内层结构最后写入即天然生效，局部可推理性强于区间树。
package parser

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// RegionType 标记一个字节区间属于什么文档结构。
type RegionType int

const (
	RegionText           RegionType = iota // 正文文本
	RegionCode                             // 代码块 / 行内代码
	RegionHTML                             // HTML 块 / 行内 HTML
	RegionHeading                          // 标题文本
	RegionListItem                         // 列表项文本
	RegionTable                            // 表格内容
	RegionBlockquote                       // 引用块内容
	RegionFrontMatter                      // YAML front matter
	RegionEmphasisMarker                   // ** / * 标记符号本身
)

func (t RegionType) String() string {
	switch t {
	case RegionText:
		return "text"
	case RegionCode:
		return "code"
	case RegionHTML:
		return "html"
	case RegionHeading:
		return "heading"
	case RegionListItem:
		return "list_item"
	case RegionTable:
		return "table"
	case RegionBlockquote:
		return "blockquote"
	case RegionFrontMatter:
		return "front_matter"
	case RegionEmphasisMarker:
		return "emphasis_marker"
	}
	return "unknown"
}

// Region 是一个按 Start 升序、互不重叠的源文本区间。
type Region struct {
	Start int
	End   int
	Type  RegionType
	Meta  map[string]any // 仅 EmphasisMarker 使用：position = open | close
}

// ParaStart 记录正文段落（Paragraph 节点）的文本起始偏移，
// 供 indent 规则定位"段落第一行"。
type ParaStart struct {
	Offset       int
	InBlockquote bool
}

// RegionMap 描述整篇源文本的结构区域划分。
type RegionMap struct {
	Regions []Region
	Source  []byte

	// FrontMatter 范围（无 front matter 时为 nil）。
	FrontMatter *[2]int

	// EmphasisMarkers 单独保存强调定界符区间（含 open/close 元信息）。
	// 主 Regions 中这些字节同样标为 RegionEmphasisMarker，便于类型查询。
	EmphasisMarkers []Region

	ParaStarts []ParaStart
}

// TypeAt returns the structure type covering off, or RegionText when the
// offset is outside the document. Returning a value instead of an error keeps
// character rules convenient while RegionAt remains available to callers that
// need to distinguish an uncovered offset.
func (m *RegionMap) TypeAt(off int) RegionType {
	if r, ok := m.RegionAt(off); ok {
		return r.Type
	}
	return RegionText
}

// FilterByType returns copies of all regions whose type is in types.
func (m *RegionMap) FilterByType(types ...RegionType) []Region {
	allowed := make(map[RegionType]bool, len(types))
	for _, t := range types {
		allowed[t] = true
	}
	result := make([]Region, 0)
	for _, region := range m.Regions {
		if allowed[region.Type] {
			result = append(result, region)
		}
	}
	return result
}

// RegionAt 返回覆盖偏移 off 的区域（二分查找）。
func (m *RegionMap) RegionAt(off int) (Region, bool) {
	lo, hi := 0, len(m.Regions)-1
	idx := -1
	for lo <= hi {
		mid := (lo + hi) / 2
		if m.Regions[mid].Start <= off {
			idx = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if idx < 0 {
		return Region{}, false
	}
	r := m.Regions[idx]
	if off >= r.Start && off < r.End {
		return r, true
	}
	return Region{}, false
}

// NewParser 返回项目统一的 goldmark 实例。
// Table 扩展必须启用：表格内容需要被标记为 RegionTable。
func NewParser() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.Table),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
}

// Parse 解析 source 并返回文档 AST。
func Parse(source []byte) ast.Node {
	reader := text.NewReader(source)
	return NewParser().Parser().Parse(reader)
}

// BuildRegionMap 按设计文档 §5.4 构建 RegionMap。
//
// 写入顺序即优先级（后写覆盖先写）：
// 围栏代码块行扫描 → AST 遍历 → front matter（最后，防 AST 误识内部内容）。
func BuildRegionMap(source []byte) (*RegionMap, error) {
	rm := &RegionMap{Source: source}
	classes := make([]RegionType, len(source)) // 零值即 RegionText

	doc := Parse(source)
	markFencedCodeBlocks(source, classes)
	walkRegions(doc, source, classes, rm)
	collectParaStarts(doc, rm)

	rm.FrontMatter = DetectFrontMatter(source)
	if rm.FrontMatter != nil {
		fill(classes, rm.FrontMatter[0], rm.FrontMatter[1], RegionFrontMatter)
	}

	rm.Regions = compress(classes)
	return rm, nil
}

// walkRegions 深度优先遍历，将叶子节点区间写入分类数组。
// 父节点先写、子节点后写覆盖，保证最内层结构优先。
func walkRegions(n ast.Node, source []byte, classes []RegionType, rm *RegionMap) {
	setLeaf(n, source, classes)
	markEmphasis(n, source, classes, rm)
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		walkRegions(child, source, classes, rm)
	}
}

// setLeaf 写入节点自身携带的字节区间。
func setLeaf(n ast.Node, source []byte, classes []RegionType) {
	switch node := n.(type) {
	case *ast.CodeSpan:
		start, end := spanExtent(node)
		s, e := extendBackticks(source, start, end)
		fill(classes, s, e, RegionCode)
	case *ast.FencedCodeBlock:
		// 围栏整体已在 markFencedCodeBlocks 中处理。
	case *ast.CodeBlock:
		segs := node.Lines()
		for i := 0; i < segs.Len(); i++ {
			seg := segs.At(i)
			fill(classes, seg.Start, seg.Stop, RegionCode)
		}
	case *ast.HTMLBlock:
		segs := node.Lines()
		for i := 0; i < segs.Len(); i++ {
			seg := segs.At(i)
			fill(classes, seg.Start, seg.Stop, RegionHTML)
		}
		if node.HasClosure() {
			fill(classes, node.ClosureLine.Start, node.ClosureLine.Stop, RegionHTML)
		}
	case *ast.RawHTML:
		segs := node.Segments
		for i := 0; i < segs.Len(); i++ {
			seg := segs.At(i)
			fill(classes, seg.Start, seg.Stop, RegionHTML)
		}
	case *ast.Text:
		seg := node.Segment
		if seg.Start < seg.Stop {
			fill(classes, seg.Start, seg.Stop, classifyByAncestors(n))
		}
	case *ast.AutoLink:
		// AutoLink 的位置在其内部 Text 子节点上；无子节点时不特殊标记。
		if s, e := spanExtent(node); s >= 0 {
			fill(classes, s, e, classifyByAncestors(node))
		}
	}
}

// classifyByAncestors 依据祖先链（设计文档 §5.4 表格）确定文本区域类型。
// 优先级：CodeSpan > Heading > Table > ListItem > Blockquote > Text。
// CodeSpan 优先：其内部 Text 子节点不能覆盖已标记的代码区域。
func classifyByAncestors(n ast.Node) RegionType {
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.(type) {
		case *ast.CodeSpan:
			return RegionCode
		case *ast.Heading:
			return RegionHeading
		case *extast.TableCell, *extast.Table:
			return RegionTable
		case *ast.ListItem:
			return RegionListItem
		case *ast.Blockquote:
			return RegionBlockquote
		}
	}
	return RegionText
}

// spanExtent 返回节点全部后代文本覆盖的 [min,max) 区间。
// 注意：goldmark 的 Text.Segment 是字段而非方法，不能走接口断言。
func spanExtent(n ast.Node) (int, int) {
	start, end := -1, -1
	var visit func(n ast.Node)
	visit = func(n ast.Node) {
		switch t := n.(type) {
		case *ast.Text:
			seg := t.Segment
			if seg.Start < seg.Stop {
				if start < 0 || seg.Start < start {
					start = seg.Start
				}
				if seg.Stop > end {
					end = seg.Stop
				}
			}
		}
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			visit(c)
		}
	}
	visit(n)
	return start, end
}

// extendBackticks 从内容区间向外扩展覆盖相邻的反引号 run（CodeSpan 场景）。
func extendBackticks(source []byte, start, end int) (int, int) {
	for start > 0 && source[start-1] == '`' {
		start--
	}
	for end < len(source) && source[end] == '`' {
		end++
	}
	return start, end
}

// markEmphasis 识别强调定界符 run。
//
// goldmark 的 Emphasis 节点不携带自身 Segment，定界符位于子文本区间之外。
// 从子文本边界向外扫描同种定界符字符（* 或 _），把整个 run 标记为
// RegionEmphasisMarker。嵌套强调（***text***）两层扫描到同一 run，天然去重。
func markEmphasis(n ast.Node, source []byte, classes []RegionType, rm *RegionMap) {
	node, ok := n.(*ast.Emphasis)
	if !ok || node.ChildCount() == 0 {
		return
	}
	start, end := spanExtent(node)
	if start < 0 || end < 0 {
		return
	}
	openStart := scanDelimiterRun(source, start, -1)
	closeEnd := scanDelimiterRun(source, end, +1)
	fill(classes, openStart, start, RegionEmphasisMarker)
	fill(classes, end, closeEnd, RegionEmphasisMarker)
	rm.EmphasisMarkers = append(rm.EmphasisMarkers,
		Region{Start: openStart, End: start, Type: RegionEmphasisMarker, Meta: map[string]any{"position": "open"}},
		Region{Start: end, End: closeEnd, Type: RegionEmphasisMarker, Meta: map[string]any{"position": "close"}},
	)
}

// scanDelimiterRun 从 offset 沿 dir 方向扫描连续的 * / _，返回 run 边界。
func scanDelimiterRun(source []byte, offset, dir int) int {
	isDelim := func(b byte) bool { return b == '*' || b == '_' }
	i := offset
	for {
		next := i + dir
		if next < 0 || next >= len(source) || !isDelim(source[next]) {
			break
		}
		i = next
	}
	if dir > 0 {
		return i + 1
	}
	return i
}

// markFencedCodeBlocks 用行扫描定位围栏代码块的完整范围（含围栏行与 info string）。
// 不依赖 AST：空代码块、波浪线围栏、未闭合围栏都能覆盖。
func markFencedCodeBlocks(source []byte, classes []RegionType) {
	lines := bytes.Split(source, []byte("\n"))
	lineOffsets := make([]int, len(lines))
	off := 0
	for i, l := range lines {
		lineOffsets[i] = off
		off += len(l) + 1
	}

	inBlock := false
	fenceChar := byte(0)
	fenceLen := 0
	for i, line := range lines {
		trimmed := bytes.TrimLeft(line, " \t")
		if !inBlock {
			if ch, n := fenceOf(trimmed); ch != 0 {
				inBlock = true
				fenceChar = ch
				fenceLen = n
				fill(classes, lineOffsets[i], lineOffsets[i]+len(line), RegionCode)
			}
			continue
		}
		// 块内：整行标记，直到出现不少于开栏长度的同字符围栏。
		fill(classes, lineOffsets[i], lineOffsets[i]+len(line), RegionCode)
		if ch, n := fenceOf(trimmed); ch == fenceChar && n >= fenceLen {
			inBlock = false
		}
	}
}

func fenceOf(trimmed []byte) (byte, int) {
	if len(trimmed) == 0 {
		return 0, 0
	}
	ch := trimmed[0]
	if ch != '`' && ch != '~' {
		return 0, 0
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0
	}
	return ch, n
}

// collectParaStarts 记录 Paragraph 节点首个文本段的起始偏移。
func collectParaStarts(doc ast.Node, rm *RegionMap) {
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if p, ok := n.(*ast.Paragraph); ok {
			segs := p.Lines()
			if segs.Len() > 0 {
				inQuote := false
				for parent := p.Parent(); parent != nil; parent = parent.Parent() {
					if _, ok := parent.(*ast.Blockquote); ok {
						inQuote = true
						break
					}
				}
				rm.ParaStarts = append(rm.ParaStarts, ParaStart{
					Offset:       segs.At(0).Start,
					InBlockquote: inQuote,
				})
			}
		}
		return ast.WalkContinue, nil
	})
}

func fill(classes []RegionType, start, end int, t RegionType) {
	if start < 0 || end <= start || end > len(classes) {
		return
	}
	for i := start; i < end; i++ {
		classes[i] = t
	}
}

// compress 将逐字节分类压缩为有序不重叠区间。
func compress(classes []RegionType) []Region {
	if len(classes) == 0 {
		return nil
	}
	var regions []Region
	start := 0
	cur := classes[0]
	for i := 1; i < len(classes); i++ {
		if classes[i] != cur {
			regions = append(regions, Region{Start: start, End: i, Type: cur})
			start = i
			cur = classes[i]
		}
	}
	regions = append(regions, Region{Start: start, End: len(classes), Type: cur})
	return regions
}

// DetectFrontMatter 识别文档头部的 YAML front matter（设计文档 §3.4）。
// 返回覆盖两个分隔线行的字节范围；无 front matter 时返回 nil。
func DetectFrontMatter(source []byte) *[2]int {
	lines := bytes.Split(source, []byte("\n"))
	if len(lines) == 0 || string(bytes.TrimRight(lines[0], "\r")) != "---" {
		return nil
	}
	off := len(lines[0]) + 1
	for i := 1; i < len(lines); i++ {
		line := bytes.TrimRight(lines[i], "\r")
		if string(line) == "---" || string(line) == "..." {
			return &[2]int{0, off + len(line)}
		}
		off += len(lines[i]) + 1
	}
	return nil
}

// LineOf 返回偏移所在的 1-based 行号，用于 warning 输出。
func LineOf(source []byte, off int) int {
	line := 1
	for i := 0; i < off && i < len(source); i++ {
		if source[i] == '\n' {
			line++
		}
	}
	return line
}
