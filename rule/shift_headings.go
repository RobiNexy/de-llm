// shift_headings.go 实现设计文档 §6.5：标题级别整体提升或下降。
//
// 这是少数需要修改结构的规则。实现不走"AST 修改 + 重新渲染"
// （goldmark 默认渲染会规范化语法风格），而是：
// 1. 用 AST 精确定位所有 Heading 节点
// 2. 仅重写包含 ATX 标记的行前缀（# 的个数），其余字节原样保留
// 3. 从后向前应用变更，避免偏移失效
//
// 已知限制（R10）：Setext 标题（=== / --- 下划线）不处理，输出 warning。
package rule

import (
	"bytes"
	"fmt"

	"github.com/RobiNexy/de-llm/markdown"
	"github.com/yuin/goldmark/ast"
)

func init() {
	Register("shift_headings", NewShiftHeadingsRule)
}

type shiftHeadingsRule struct {
	offset     int
	scope      []int
	onOverflow string // clamp | error | skip
}

func NewShiftHeadingsRule(cfg RuleConfig) (Rule, error) {
	r := &shiftHeadingsRule{
		offset:     cfg.Int("offset", 0),
		scope:      cfg.Ints("scope"),
		onOverflow: cfg.String("on_overflow", "clamp"),
	}
	switch r.onOverflow {
	case "clamp", "error", "skip":
	default:
		return nil, fmt.Errorf("shift_headings: 不支持的 on_overflow %q（支持 clamp/error/skip）", r.onOverflow)
	}
	return r, nil
}

func (r *shiftHeadingsRule) Name() string        { return "shift_headings" }
func (r *shiftHeadingsRule) Description() string { return "标题级别整体提升或下降" }

type headingChange struct {
	start    int // '#' run 的起始偏移
	oldLevel int
	newLevel int
}

func (r *shiftHeadingsRule) Transform(ctx Context, rm *markdown.RegionMap) ([]byte, error) {
	src := rm.Source
	if len(src) == 0 || r.offset == 0 {
		return src, nil
	}

	var changes []headingChange
	var setextSkipped int

	doc := markdown.Parse(src)
	walkErr := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		heading, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}
		oldLevel := heading.Level
		if len(r.scope) > 0 && !containsInt(r.scope, oldLevel) {
			return ast.WalkContinue, nil
		}

		// Setext 判定先于 overflow 处理：所有被跳过的标题
		// 都应进入 warning 计数，与 overflow 路径无关。
		_, isATX := r.atxMarker(src, heading)
		if !isATX {
			setextSkipped++
			return ast.WalkContinue, nil
		}

		newLevel := oldLevel + r.offset
		if newLevel < 1 || newLevel > 6 {
			switch r.onOverflow {
			case "clamp":
				if newLevel < 1 {
					newLevel = 1
				} else {
					newLevel = 6
				}
			case "error":
				line := r.headingLine(src, heading)
				return ast.WalkStop, fmt.Errorf("shift_headings: 第 %d 行标题 h%d 越界（目标 h%d）",
					markdown.LineOf(src, line), oldLevel, newLevel)
			case "skip":
				return ast.WalkContinue, nil
			}
		}
		if newLevel == oldLevel {
			return ast.WalkContinue, nil
		}

		markerStart, _ := r.atxMarker(src, heading)
		changes = append(changes, headingChange{start: markerStart, oldLevel: oldLevel, newLevel: newLevel})
		return ast.WalkContinue, nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	ctx.AddStat("shifted", len(changes))
	if setextSkipped > 0 {
		ctx.Warnf("shift_headings: %d 个 Setext 标题（===/--- 下划线）不支持级别调整，已跳过", setextSkipped)
	}
	if len(changes) == 0 {
		return src, nil
	}

	// 从后向前替换 '#' run。
	out := append([]byte{}, src...)
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		newRun := bytes.Repeat([]byte("#"), c.newLevel)
		out = append(out[:c.start:c.start],
			append(newRun, out[c.start+c.oldLevel:]...)...)
	}
	return out, nil
}

// atxMarker 返回 ATX 标题 '#' run 的起始偏移；非 ATX（Setext）返回 false。
func (r *shiftHeadingsRule) atxMarker(src []byte, h *ast.Heading) (int, bool) {
	segs := h.Lines()
	if segs.Len() == 0 {
		return 0, false
	}
	textStart := segs.At(0).Start
	lineStart := textStart
	for lineStart > 0 && src[lineStart-1] != '\n' {
		lineStart--
	}
	i := lineStart
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	if i < len(src) && src[i] == '#' {
		return i, true
	}
	return 0, false
}

// headingLine 返回标题所在行号（error 信息用）。
func (r *shiftHeadingsRule) headingLine(src []byte, h *ast.Heading) int {
	if segs := h.Lines(); segs.Len() > 0 {
		return segs.At(0).Start
	}
	return 0
}

func containsInt(arr []int, v int) bool {
	for _, e := range arr {
		if e == v {
			return true
		}
	}
	return false
}
