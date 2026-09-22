// pangu_test.go — golden 测试 + 插入规则断言（design.md §6.7）。
package rule

import (
	"strings"
	"testing"
)

func TestPanguGolden(t *testing.T) {
	cases := []string{"basic", "code_block", "emphasis"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			runGolden(t, "pangu", c, RuleConfig{}, false)
		})
	}
}

func TestPanguIdempotent(t *testing.T) {
	r, _ := New("pangu", RuleConfig{})
	src := []byte("已 有 空 格 的中文 English 混排。\n")
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	once, _ := r.Transform(ctx, rm)
	ctx2 := newTestContext()
	rm2, _ := markdownBuild(once)
	twice, _ := r.Transform(ctx2, rm2)
	if string(once) != string(twice) {
		t.Errorf("幂等性破坏:\nonce:  %q\ntwice: %q", once, twice)
	}
}

func TestPanguNoInsertAroundASCIIPunct(t *testing.T) {
	r, _ := New("pangu", RuleConfig{})
	src := []byte("英文,逗号.句点:冒号不插入。\n")
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	result, _ := r.Transform(ctx, rm)
	if string(result) != string(src) {
		t.Errorf("ASCII 标点旁不应插入: %q", result)
	}
}

func TestPanguCurlyQuotesCountAsChinese(t *testing.T) {
	r, _ := New("pangu", RuleConfig{})
	src := []byte("他说“hello”给你。\n")
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	result, _ := r.Transform(ctx, rm)
	if !strings.Contains(string(result), "“hello”") {
		t.Errorf("弯引号内英文旁应插入空格: %q", result)
	}
}

func TestPanguTransparentMarkers(t *testing.T) {
	r, _ := New("pangu", RuleConfig{})
	// 英文+标记+中文：空格贴近中文一侧（标记外）。
	src := []byte("**English**中文。\n")
	ctx := newTestContext()
	rm, _ := markdownBuild(src)
	result, _ := r.Transform(ctx, rm)
	if string(result) != "**English** 中文。\n" {
		t.Errorf("结果: %q", result)
	}
	// 中文+标记+英文：空格贴近中文一侧（标记外）。
	src = []byte("中文**English**\n")
	ctx = newTestContext()
	rm, _ = markdownBuild(src)
	result, _ = r.Transform(ctx, rm)
	if string(result) != "中文 **English**\n" {
		t.Errorf("结果: %q", result)
	}
}
