// emphasis_space.go 实现设计文档 §6.8：强调标记前后紧邻中文时插入空格。
//
// 不自行解析强调语法，直接利用 RegionMap 构建时记录的
// EmphasisMarker 区间（delimiter run 扫描，见 markdown/markEmphasis）。
package rule

import (
	"bytes"
	"unicode/utf8"

	"github.com/RobiNexy/de-llm/markdown"
)

func init() {
	Register("emphasis_space", NewEmphasisSpaceRule)
}

type emphasisSpaceRule struct {
	spaceChar string
}

func NewEmphasisSpaceRule(cfg RuleConfig) (Rule, error) {
	sc := cfg.String("space_char", " ")
	if sc == "" {
		sc = " "
	}
	return &emphasisSpaceRule{spaceChar: sc}, nil
}

func (r *emphasisSpaceRule) Name() string { return "emphasis_space" }
func (r *emphasisSpaceRule) Description() string {
	return "强调标记（**、*）前后自动插入空格"
}

// isCJKAdjacent 判断紧邻强调标记的字符是否为"中文"。
//
// 范围刻意比 pangu 的中文集窄：排除全角形式（U+FF00..FFEF），
// 避免（**text**）这类全角括号场景产生"（ **text** ）"的过度插入；
// 同时排除 U+3000（全角空格）——它是缩进字符（indent 规则），
// 属于空白而非正文，"　***text***"不应产生额外空格。
func isCJKAdjacent(ch rune) bool {
	switch {
	case ch == 0x3000:
		return false
	case ch >= 0x4E00 && ch <= 0x9FFF:
		return true
	case ch >= 0x3400 && ch <= 0x4DBF:
		return true
	case ch >= 0xF900 && ch <= 0xFAFF:
		return true
	case ch >= 0x3001 && ch <= 0x303F:
		return true
	case ch >= 0x201C && ch <= 0x201D:
		return true
	case ch >= 0x2018 && ch <= 0x2019:
		return true
	}
	return false
}

func (r *emphasisSpaceRule) Transform(ctx Context, rm *markdown.RegionMap) ([]byte, error) {
	src := rm.Source
	if len(src) == 0 || len(rm.EmphasisMarkers) == 0 {
		return src, nil
	}

	// 收集插入点：去重（嵌套强调可能指向同一 run 边界）。
	seen := map[int]bool{}
	var insertions []int
	add := func(off int) {
		if off < 0 || off > len(src) || seen[off] {
			return
		}
		seen[off] = true
		insertions = append(insertions, off)
	}

	for _, m := range rm.EmphasisMarkers {
		pos, _ := m.Meta["position"].(string)
		switch pos {
		case "open":
			// 前一字符是中文且非空格 → 在标记前插入。
			if ch, _, ok := r.runeBefore(src, m.Start); ok && isCJKAdjacent(ch) {
				add(m.Start)
			}
		case "close":
			// 后一字符是中文且非空格 → 在标记后插入。
			if ch, _, ok := r.runeAfter(src, m.End); ok && isCJKAdjacent(ch) {
				add(m.End)
			}
		}
	}
	ctx.AddStat("inserted", len(insertions))
	if len(insertions) == 0 {
		return src, nil
	}

	// 插入点去重后仍可能乱序（open/close 交错），排序后统一应用。
	sortInts(insertions)

	var out bytes.Buffer
	out.Grow(len(src) + len(insertions)*len(r.spaceChar))
	prev := 0
	for _, off := range insertions {
		out.Write(src[prev:off])
		out.WriteString(r.spaceChar)
		prev = off
	}
	out.Write(src[prev:])
	return out.Bytes(), nil
}

func (r *emphasisSpaceRule) runeBefore(src []byte, off int) (rune, int, bool) {
	if off <= 0 {
		return 0, 0, false
	}
	ch, size := utf8.DecodeLastRune(src[:off])
	if size == 0 {
		return 0, 0, false
	}
	return ch, off - size, true
}

func (r *emphasisSpaceRule) runeAfter(src []byte, off int) (rune, int, bool) {
	if off >= len(src) {
		return 0, 0, false
	}
	ch, size := utf8.DecodeRune(src[off:])
	if size == 0 {
		return 0, 0, false
	}
	return ch, off + size, true
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
