// Package rule 实现设计文档 §6 的本地规则系统。
//
// 规则契约：输入 RegionMap + 原始 source，输出新 source。
// 每条规则执行前由流水线重建 RegionMap（规则可能改变文本长度），
// 因此规则内部不需要维护跨规则的字节偏移。
package rule

import (
	"fmt"
	"sort"

	markdown "github.com/RobiNexy/de-llm/pkg/parser"
)

// Context 是流水线注入的告警与统计通道。
// 设计文档 §6.3（未闭合引号 warning）与 §10.3（stats 输出）
// 要求规则具备副作用上报能力，而 Transform 的返回值只有数据，
// 因此以显式参数而非全局状态传递。
type Context interface {
	// Warnf 记录一条 warning（输出到 stderr 日志）。
	Warnf(format string, args ...any)
	// AddStat 累加当前规则的统计计数（如 "replacements"）。
	AddStat(key string, delta int)
}

// Rule 是本地规则的统一接口。
type Rule interface {
	Name() string
	Description() string
	Transform(ctx Context, regionMap *markdown.RegionMap) ([]byte, error)
}

// Factory 是规则工厂：cfg 为该规则在配置 rules: 段下的参数。
type Factory func(cfg RuleConfig) (Rule, error)

// RuleConfig 是规则参数的弱类型视图，提供带默认值的读取方法。
type RuleConfig map[string]any

func (c RuleConfig) String(key, def string) string {
	if v, ok := c[key].(string); ok {
		return v
	}
	return def
}

func (c RuleConfig) Int(key string, def int) int {
	switch v := c[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return def
}

func (c RuleConfig) Bool(key string, def bool) bool {
	if v, ok := c[key].(bool); ok {
		return v
	}
	return def
}

// Strings 读取字符串数组；yaml.v3 反序列化为 []any 时逐元素转换。
func (c RuleConfig) Strings(key string) []string {
	switch v := c[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// Ints 读取整数数组（scope: [1,2,3] 场景）。
func (c RuleConfig) Ints(key string) []int {
	switch v := c[key].(type) {
	case []int:
		return v
	case []any:
		out := make([]int, 0, len(v))
		for _, e := range v {
			switch n := e.(type) {
			case int:
				out = append(out, n)
			case int64:
				out = append(out, int(n))
			case float64:
				out = append(out, int(n))
			}
		}
		return out
	}
	return nil
}

var registry = map[string]Factory{}

// Register 注册规则工厂（init 时调用）。
func Register(name string, f Factory) {
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("rule %q already registered", name))
	}
	registry[name] = f
}

// New 按名称构造规则实例。
func New(name string, cfg RuleConfig) (Rule, error) {
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("未知规则: %s", name)
	}
	return f(cfg)
}

// Names 返回所有已注册规则名（升序）。
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DescriptionOf 返回规则描述。
func DescriptionOf(name string) string {
	r, err := New(name, RuleConfig{})
	if err != nil {
		return ""
	}
	return r.Description()
}
