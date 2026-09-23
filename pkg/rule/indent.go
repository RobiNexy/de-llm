// indent.go 实现设计文档 §6.6：段首缩进。
//
// 只在段落第一行行首插入缩进字符；软换行产生的后续行不处理。
// 使用 U+3000 而非 ASCII 空格：后者会被 Markdown 渲染器
// 解释为代码块缩进或直接忽略（设计文档 §6.6）。
package rule

import (
	"bytes"
	"strings"
	"unicode/utf8"

	markdown "github.com/RobiNexy/de-llm/pkg/parser"
)

func init() {
	Register("indent", NewIndentRule)
}

const defaultIndentChar = "\u3000\u3000" // 两个全角空格

type indentRule struct {
	char               string
	scopeParagraph     bool
	scopeBlockquote    bool
	skipFirstParagraph bool
}

func NewIndentRule(cfg RuleConfig) (Rule, error) {
	ch := cfg.String("char", defaultIndentChar)
	if !utf8.ValidString(ch) || ch == "" {
		return nil, ErrIndentChar
	}
	r := &indentRule{char: ch, skipFirstParagraph: cfg.Bool("skip_first_paragraph", false)}
	scope := cfg.Strings("scope")
	if len(scope) == 0 {
		scope = []string{"paragraph"}
	}
	for _, s := range scope {
		switch s {
		case "paragraph":
			r.scopeParagraph = true
		case "blockquote":
			r.scopeBlockquote = true
		default:
			return nil, ErrIndentScope(s)
		}
	}
	return r, nil
}

var ErrIndentChar = errIndentChar{}

type errIndentChar struct{}

func (errIndentChar) Error() string { return "indent: char 不能为空且必须是合法 UTF-8" }

type ErrIndentScope string

func (e ErrIndentScope) Error() string {
	return "indent: 不支持的 scope: " + string(e) + "（支持 paragraph / blockquote）"
}

func (r *indentRule) Name() string        { return "indent" }
func (r *indentRule) Description() string { return "段首缩进（两个全角空格）" }

func (r *indentRule) Transform(ctx Context, rm *markdown.RegionMap) ([]byte, error) {
	src := rm.Source
	if len(src) == 0 {
		return src, nil
	}

	// 收集插入点。
	var insertions []int
	seenPara := false
	for _, ps := range rm.ParaStarts {
		if ps.InBlockquote {
			if !r.scopeBlockquote {
				continue
			}
		} else if !r.scopeParagraph {
			continue
		}
		if r.skipFirstParagraph && !seenPara {
			seenPara = true
			continue
		}
		seenPara = true

		// 幂等性：段落文本已以缩进字符开头则跳过。
		rest := src[ps.Offset:]
		if strings.HasPrefix(string(rest), r.char) {
			continue
		}
		if ch, _ := utf8.DecodeRune(rest); ch == '\u3000' {
			continue
		}
		insertions = append(insertions, ps.Offset)
	}

	ctx.AddStat("indented", len(insertions))
	if len(insertions) == 0 {
		return src, nil
	}

	var out bytes.Buffer
	out.Grow(len(src) + len(insertions)*len(r.char))
	prev := 0
	for _, off := range insertions {
		out.Write(src[prev:off])
		out.WriteString(r.char)
		prev = off
	}
	out.Write(src[prev:])
	return out.Bytes(), nil
}
