// quotes.go 实现设计文档 §6.3：半角引号 → 中文弯引号（配对计数状态机）。
package rule

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"github.com/RobiNexy/de-llm/markdown"
)

func init() {
	Register("quotes", NewQuotesRule)
}

type quotesConfig struct {
	style                string
	singleQuotes         bool
	apostropheProtection bool
}

func NewQuotesRule(cfg RuleConfig) (Rule, error) {
	style := cfg.String("style", "curly")
	if style != "curly" {
		return nil, fmt.Errorf("quotes: 不支持的 style %q（当前仅支持 curly）", style)
	}
	return &quotesRule{
		style:                style,
		singleQuotes:         cfg.Bool("single_quotes", true),
		apostropheProtection: cfg.Bool("apostrophe_protection", true),
	}, nil
}

type quotesRule struct {
	style                string
	singleQuotes         bool
	apostropheProtection bool
}

func (r *quotesRule) Name() string { return "quotes" }
func (r *quotesRule) Description() string {
	return "半角引号 → 中文弯引号（配对计数）"
}

// quotesState 是配对计数状态机的状态。
// 双引号与单引号各自独立维护"下一个是开引号还是闭引号"。
type quotesState struct {
	doubleExpectOpen bool
	singleExpectOpen bool
}

// quotesAllowed 判断区域是否可处理（设计文档 §6.3）。
func quotesAllowed(t markdown.RegionType) bool {
	switch t {
	case markdown.RegionText, markdown.RegionHeading,
		markdown.RegionListItem, markdown.RegionTable, markdown.RegionBlockquote:
		return true
	}
	return false
}

// quotesSkip 引起状态重置的区域类型（设计文档：跨过这些区域视为段落边界）。
func quotesSkip(t markdown.RegionType) bool {
	switch t {
	case markdown.RegionCode, markdown.RegionHTML, markdown.RegionFrontMatter:
		return true
	}
	return false
}

func (r *quotesRule) Transform(ctx Context, rm *markdown.RegionMap) ([]byte, error) {
	src := rm.Source
	if len(src) == 0 {
		return src, nil
	}
	var out bytes.Buffer
	out.Grow(len(src) + len(src)/8)

	st := quotesState{doubleExpectOpen: true, singleExpectOpen: true}
	prevEnd := -1 // 上一个可处理区域的结束偏移；-1 表示尚未开始

	flushGap := func(from, to int) {
		if from >= 0 && to > from {
			out.Write(src[from:to])
		}
	}

	for i := range rm.Regions {
		reg := rm.Regions[i]
		if reg.End <= reg.Start {
			continue
		}
		if !quotesAllowed(reg.Type) {
			continue
		}
		// 段边界识别：与上一个可处理区域之间跨过空白段（含 \n\n）
		// 或存在 Code/HTML/FrontMatter 区域 → 重置状态机。
		if prevEnd >= 0 {
			gap := src[prevEnd:reg.Start]
			crossedSkip := false
			for j := i - 1; j >= 0; j-- {
				mid := rm.Regions[j]
				if mid.End <= prevEnd {
					break
				}
				if quotesSkip(mid.Type) {
					crossedSkip = true
					break
				}
			}
			if bytes.Contains(gap, []byte("\n\n")) || crossedSkip {
				r.closeParagraph(ctx, rm, st, prevEnd)
				st = quotesState{doubleExpectOpen: true, singleExpectOpen: true}
			}
		}
		gapStart := prevEnd
		if gapStart < 0 {
			gapStart = 0
		}
		flushGap(gapStart, reg.Start)

		r.transformRegion(ctx, rm, &out, reg, &st)
		prevEnd = reg.End
	}
	if prevEnd < 0 {
		flushGap(0, len(src))
	} else {
		flushGap(prevEnd, len(src))
	}
	// 文档结尾的未闭合检查。
	r.closeParagraph(ctx, rm, st, len(src))
	return out.Bytes(), nil
}

// closeParagraph 在段落边界检查未闭合引号并输出 warning（不回退转换）。
func (r *quotesRule) closeParagraph(ctx Context, rm *markdown.RegionMap, st quotesState, off int) {
	if !st.doubleExpectOpen {
		ctx.Warnf("quotes: 第 %d 行存在未闭合的双引号", markdown.LineOf(rm.Source, min(off, len(rm.Source)-1)))
	}
	if r.singleQuotes && !st.singleExpectOpen {
		ctx.Warnf("quotes: 第 %d 行存在未闭合的单引号", markdown.LineOf(rm.Source, min(off, len(rm.Source)-1)))
	}
}

// transformRegion 对单个可处理区域执行字符级状态机转换。
//
// 段边界不仅在区域之间（跨区域 gap），也在区域内部：RegionMap 的
// Text 区域可能横跨多个段落（空行字节也属于 Text 区域），
// 因此在区域内部扫描到空行（\n\n）时同样重置状态。
func (r *quotesRule) transformRegion(ctx Context, rm *markdown.RegionMap, out *bytes.Buffer, reg markdown.Region, st *quotesState) {
	src := rm.Source
	i := reg.Start
	lastWasNewline := false
	for i < reg.End {
		b := src[i]
		switch {
		case b == '\n':
			out.WriteByte(b)
			if lastWasNewline {
				// 空行 = 段落边界：重置配对状态。
				st.doubleExpectOpen = true
				st.singleExpectOpen = true
			}
			lastWasNewline = true
			i++
		case b == '\r':
			out.WriteByte(b)
			i++
		case b == '"':
			if st.doubleExpectOpen {
				out.WriteString("\u201c") // "
			} else {
				out.WriteString("\u201d") // "
			}
			st.doubleExpectOpen = !st.doubleExpectOpen
			ctx.AddStat("replaced", 1)
			lastWasNewline = false
			i++
		case b == '\'':
			if !r.singleQuotes || (r.apostropheProtection && r.isApostrophe(src, i)) {
				out.WriteByte(b)
				lastWasNewline = false
				i++
				continue
			}
			if st.singleExpectOpen {
				out.WriteString("\u2018") // '
			} else {
				out.WriteString("\u2019") // '
			}
			st.singleExpectOpen = !st.singleExpectOpen
			ctx.AddStat("replaced", 1)
			lastWasNewline = false
			i++
		default:
			// 逐字节拷贝多字节 UTF-8 序列。
			size := 1
			if b >= 0x80 {
				_, size = utf8.DecodeRune(src[i:reg.End])
				if size <= 0 {
					size = 1
				}
			}
			out.Write(src[i : i+size])
			lastWasNewline = false
			i += size
		}
	}
}

// isApostrophe 判定 src[i] 的 ' 是否为英文撇号：
// 前一字符与后一字符均为 ASCII 字母（设计文档 §6.3）。
// 已知误判：'twas 等古语缩写（R2，可接受）。
func (r *quotesRule) isApostrophe(src []byte, i int) bool {
	prevIsLetter := i > 0 && isASCIILetter(src[i-1])
	nextIsLetter := i+1 < len(src) && isASCIILetter(src[i+1])
	return prevIsLetter && nextIsLetter
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
