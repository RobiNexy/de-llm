// quotes_test.go — golden 测试 + 配对状态机行为断言（design.md §6.3/§12.1）。
package rule

import (
	"strings"
	"testing"
)

func TestQuotesGolden(t *testing.T) {
	cases := []struct {
		caseName      string
		cfg           RuleConfig
		expectWarning bool
	}{
		{caseName: "basic"},
		{caseName: "nested"},
		{caseName: "code_block"},
		{caseName: "unclosed", expectWarning: true},
	}
	for _, tc := range cases {
		t.Run(tc.caseName, func(t *testing.T) {
			runGolden(t, "quotes", tc.caseName, RuleConfig{
				"style":                 "curly",
				"single_quotes":         true,
				"apostrophe_protection": true,
			}, tc.expectWarning)
		})
	}
}

func TestQuotesSingleQuotesDisabled(t *testing.T) {
	r, err := New("quotes", RuleConfig{"single_quotes": false})
	if err != nil {
		t.Fatal(err)
	}
	src := []byte("他说'单引号'保持。\n")
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	result, err := r.Transform(ctx, rm)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result), "\u2018") || strings.Contains(string(result), "\u2019") {
		t.Errorf("单引号应保持原样: %q", result)
	}
	if !strings.Contains(string(result), "'单引号'") {
		t.Errorf("结果错误: %q", result)
	}
}

func TestQuotesApostropheProtectionOff(t *testing.T) {
	r, _ := New("quotes", RuleConfig{"apostrophe_protection": false})
	src := []byte("I don't know.\n")
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	result, _ := r.Transform(ctx, rm)
	// 关闭保护：don't 的撇号按配对计数处理（第 1 个单引号 → 开引号）。
	if !strings.Contains(string(result), "don\u2018t") {
		t.Errorf("结果错误: %q", result)
	}
}

func TestQuotesStateResetsAcrossBlankLine(t *testing.T) {
	r, _ := New("quotes", RuleConfig{})
	// 第一段未闭合 + 空行后新段 → 状态重置，新段的 " 作为开引号。
	src := []byte("未闭合\"引号\n\n新段\"配对\"结束。\n")
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	result, _ := r.Transform(ctx, rm)
	if !strings.Contains(string(result), "新段\u201c配对\u201d") {
		t.Errorf("段间状态应重置: %q", result)
	}
}
