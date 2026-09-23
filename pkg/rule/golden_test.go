// golden_test.go 实现 design.md §12.1 的 golden file 测试机制。
package rule

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	markdown "github.com/RobiNexy/de-llm/pkg/parser"
)

// testContext 收集 warning 与统计，供断言。
type testContext struct {
	warnings []string
	stats    map[string]int
}

func newTestContext() *testContext {
	return &testContext{stats: map[string]int{}}
}

func (c *testContext) Warnf(format string, args ...any) {
	c.warnings = append(c.warnings, fmt.Sprintf(format, args...))
}

func (c *testContext) AddStat(key string, delta int) {
	c.stats[key] += delta
}

// runGolden 对 testdata/<rule>/<case>_input.md 执行规则并与
// <case>_expected.md 比对。
func runGolden(t *testing.T, ruleName, ruleCase string, cfg RuleConfig, expectWarning bool) {
	t.Helper()
	dir := filepath.Join("testdata", ruleName)
	input, err := os.ReadFile(filepath.Join(dir, ruleCase+"_input.md"))
	if err != nil {
		t.Fatalf("读取输入失败: %v", err)
	}
	expected, err := os.ReadFile(filepath.Join(dir, ruleCase+"_expected.md"))
	if err != nil {
		t.Fatalf("读取期望失败: %v", err)
	}

	r, err := New(ruleName, cfg)
	if err != nil {
		t.Fatalf("构造规则失败: %v", err)
	}
	rm, err := markdown.BuildRegionMap(input)
	if err != nil {
		t.Fatalf("构建 RegionMap 失败: %v", err)
	}
	ctx := newTestContext()
	result, err := r.Transform(ctx, rm)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	if string(result) != string(expected) {
		t.Errorf("结果不一致:\n--- 得到 ---\n%s\n--- 期望 ---\n%s", result, expected)
	}
	if expectWarning && len(ctx.warnings) == 0 {
		t.Errorf("期望 warning，实际无")
	}
	if !expectWarning && len(ctx.warnings) > 0 {
		t.Errorf("意外 warnings: %v", ctx.warnings)
	}
}
