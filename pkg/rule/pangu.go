// pangu.go 实现设计文档 §6.7：中英之间自动插入空格（盘古之白）。
//
// 实现为两遍式：第一遍逐 rune 边界收集插入点（含穿过 emphasis
// 标记的透明判定），第二遍统一应用。相比单遍流式处理，两遍式
// 避免"插入后偏移移动导致的边界遗漏"，正确性更容易局部推理。
package rule

import (
	"bytes"
	"unicode/utf8"

	markdown "github.com/RobiNexy/de-llm/pkg/parser"
)

func init() {
	Register("pangu", NewPanguRule)
}

type panguRule struct {
	spaceChar string
}

func NewPanguRule(cfg RuleConfig) (Rule, error) {
	sc := cfg.String("space_char", " ")
	if sc == "" {
		sc = " "
	}
	return &panguRule{spaceChar: sc}, nil
}

func (r *panguRule) Name() string        { return "pangu" }
func (r *panguRule) Description() string { return "中英文之间自动插入空格" }

// charClass 是字符在盘古规则中的类别。
type charClass int

const (
	classOther    charClass = iota // ASCII 标点等：不参与插入
	classChinese                   // 中文字符（含中文标点，范围见设计文档 §6.7）
	classAlnum                     // ASCII 字母/数字
	classBacktick                  // 反引号（行内代码边界）
	classMarker                    // 强调标记：透明，判定时穿透
	classOpaque                    // 代码内容/HTML/front matter：阻断判定
	classSpace                     // 空白：阻断插入
)

// isIdeograph 判断是否为汉字（触发插入）。
// 注：设计文档 §6.7 的"中文字符"范围含中文标点与全角形式，
// 此处拆分为触发插入的汉字子集与不触发的标点子集（裁决说明见下）。
func isIdeograph(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified Ideographs
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK Extension A
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK Compatibility
		return true
	}
	return false
}

// isCJKPunct 判断是否为中文标点/全角形式（不触发插入）。
func isCJKPunct(r rune) bool {
	switch {
	case r >= 0x3001 && r <= 0x303F: // CJK 标点（排除 0x3000 全角空格）
		return true
	case r >= 0xFF00 && r <= 0xFFEF: // 全角形式
		return true
	case r >= 0x201C && r <= 0x201D: // 中文双引号
		return true
	case r >= 0x2018 && r <= 0x2019: // 中文单引号
		return true
	}
	return false
}

// panguAllowed 是 pangu 可处理的区域类型。
func panguAllowed(t markdown.RegionType) bool {
	switch t {
	case markdown.RegionText, markdown.RegionHeading,
		markdown.RegionListItem, markdown.RegionTable, markdown.RegionBlockquote:
		return true
	}
	return false
}

func (r *panguRule) Transform(ctx Context, rm *markdown.RegionMap) ([]byte, error) {
	src := rm.Source
	if len(src) == 0 {
		return src, nil
	}

	// 第一遍：收集插入点（offset 去重，可能来自不同边界的同一位置）。
	seen := map[int]bool{}
	var insertions []int
	add := func(off int) {
		if off < 0 || off > len(src) || seen[off] {
			return
		}
		seen[off] = true
		insertions = append(insertions, off)
	}
	i := 0
	for i < len(src) {
		_, size1 := utf8.DecodeRune(src[i:])
		if size1 <= 0 {
			size1 = 1
		}
		mid := i + size1
		if mid < len(src) {
			if off, ok := r.insertOffset(rm, mid); ok {
				add(off)
			}
		}
		i = mid
	}
	ctx.AddStat("inserted", len(insertions))
	if len(insertions) == 0 {
		return src, nil
	}

	// 第二遍：统一应用。
	var out bytes.Buffer
	out.Grow(len(src) + len(insertions))
	prev := 0
	for _, off := range insertions {
		out.Write(src[prev:off])
		out.WriteString(r.spaceChar)
		prev = off
	}
	out.Write(src[prev:])
	return out.Bytes(), nil
}

// insertOffset 判断是否在插入并返回插入点。
//
// 位置约定：空格贴近中文字符一侧（pangu 惯例）。
//   - 转换 (中文 | 标记 | 英文)：插在标记 run 起点 → 中文 **English
//   - 转换 (英文 | 标记 | 中文)：插在标记 run 终点 → English** 中文
//   - 无标记：插在 mid 边界。
func (r *panguRule) insertOffset(rm *markdown.RegionMap, mid int) (int, bool) {
	if r.markerAt(rm, mid-1) && r.markerAt(rm, mid) {
		return 0, false // 标记 run 内部：不可插入
	}

	c1, off1 := r.classBackward(rm, mid)
	c2, off2 := r.classForward(rm, mid)

	switch {
	case c1 == classChinese && c2 == classAlnum:
		return off1, true // off1 = 中文右侧边界
	case c1 == classAlnum && c2 == classChinese:
		return off2, true // off2 = 中文左侧边界
	case c1 == classChinese && c2 == classBacktick:
		return mid, true
	case c1 == classBacktick && c2 == classChinese:
		return mid, true
	}
	return 0, false
}

// classForward 计算 off 处 rune 的类别；若为强调标记则穿透整个 marker run。
// 返回值二：找到的非标记字符的起始偏移（即其左侧边界，插入点候选）。
func (r *panguRule) classForward(rm *markdown.RegionMap, off int) (charClass, int) {
	for off < len(rm.Source) {
		ch, size := utf8.DecodeRune(rm.Source[off:])
		if size <= 0 {
			return classOpaque, off
		}
		if r.markerAt(rm, off) {
			off += size
			continue
		}
		return r.classOf(rm, off, ch), off
	}
	return classOpaque, off
}

// classBackward 从 end 向前取类别；若为强调标记则穿透整个 marker run。
// 返回值二：找到的非标记字符的结束偏移（即其右侧边界，插入点候选）。
func (r *panguRule) classBackward(rm *markdown.RegionMap, end int) (charClass, int) {
	for end > 0 {
		ch, size := utf8.DecodeLastRune(rm.Source[:end])
		if size <= 0 {
			return classOpaque, end
		}
		if r.markerAt(rm, end-size) {
			end -= size
			continue
		}
		return r.classOf(rm, end-size, ch), end
	}
	return classOpaque, end
}

// classOf 计算非标记 rune 的类别。
func (r *panguRule) classOf(rm *markdown.RegionMap, off int, ch rune) charClass {
	switch {
	case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
		return classSpace
	case ch == '`':
		if reg, ok := rm.RegionAt(off); ok && reg.Type == markdown.RegionCode {
			return classBacktick
		}
		return classOther
	case isIdeograph(ch):
		if reg, ok := rm.RegionAt(off); ok && panguAllowed(reg.Type) {
			return classChinese
		}
		return classOpaque
	case (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9'):
		if reg, ok := rm.RegionAt(off); ok && panguAllowed(reg.Type) {
			return classAlnum
		}
		return classOpaque
	}
	// 中文标点/全角形式/ASCII 标点：不触发插入（见 isCJKPunct 裁决说明）。
	return classOther
}

func (r *panguRule) markerAt(rm *markdown.RegionMap, off int) bool {
	if off < 0 || off >= len(rm.Source) {
		return false
	}
	reg, ok := rm.RegionAt(off)
	return ok && reg.Type == markdown.RegionEmphasisMarker
}
