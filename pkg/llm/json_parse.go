// json_parse.go 实现设计文档 §7.4 的 JSON 输出解析与容错。
package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DetectMatch 是检测轮的单条结果。
type DetectMatch struct {
	ID     string `json:"id"`
	Prefix string `json:"prefix"`
}

type detectResponse struct {
	Matches []DetectMatch `json:"matches"`
}

// Rewrite 是改写轮的单条结果。
type Rewrite struct {
	ID     string `json:"id"`
	Prefix string `json:"prefix"`
	Text   string `json:"text"`
}

type rewriteResponse struct {
	Rewrites []Rewrite `json:"rewrites"`
}

// ParseDetect 解析检测轮输出。容错链：
// 剥离 Markdown 代码块包裹 → 直接解析 → 修复尾逗号 → 失败。
func ParseDetect(raw string) ([]DetectMatch, error) {
	var resp detectResponse
	if err := unmarshalTolerant(raw, &resp); err != nil {
		return nil, err
	}
	return resp.Matches, nil
}

// ParseRewrite 解析改写轮输出。
func ParseRewrite(raw string) ([]Rewrite, error) {
	var resp rewriteResponse
	if err := unmarshalTolerant(raw, &resp); err != nil {
		return nil, err
	}
	return resp.Rewrites, nil
}

// unmarshalTolerant 按设计文档 §7.4.3 的容错顺序尝试。
func unmarshalTolerant(raw string, v any) error {
	if err := json.Unmarshal([]byte(raw), v); err == nil {
		return nil
	}
	// 1. 剥离 ```json ... ``` 包裹。
	if s := stripCodeFence(raw); s != raw {
		if err := json.Unmarshal([]byte(s), v); err == nil {
			return nil
		}
		// 2. 剥离 + 修复尾逗号。
		if err := json.Unmarshal([]byte(fixTrailingCommas(s)), v); err == nil {
			return nil
		}
	}
	// 3. 提取首个 {...} 平衡块再解析（LLM 可能输出解释文字）。
	if s := extractJSONObject(raw); s != "" {
		if err := json.Unmarshal([]byte(s), v); err == nil {
			return nil
		}
		if err := json.Unmarshal([]byte(fixTrailingCommas(s)), v); err == nil {
			return nil
		}
	}
	// 4. 原文修复尾逗号。
	if err := json.Unmarshal([]byte(fixTrailingCommas(raw)), v); err == nil {
		return nil
	}
	return fmt.Errorf("JSON 解析失败（已尝试剥离代码块与修复尾逗号）: %.120s", raw)
}

// stripCodeFence 剥离 ```json ... ``` / ``` ... ``` 包裹。
func stripCodeFence(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return s
	}
	// 去掉首行（```json）。
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[i+1:]
	} else {
		return s
	}
	// 去掉末尾 ```。
	if i := strings.LastIndex(t, "```"); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

// fixTrailingCommas 修复 ] 或 } 前的尾逗号（字符串字面量感知）。
func fixTrailingCommas(s string) string {
	return removeTrailingCommas(s)
}

// removeTrailingCommas 删除 JSON 中 ] / } 前的逗号（字符串字面量外）。
func removeTrailingCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			b.WriteByte(c)
			continue
		}
		switch c {
		case '\\':
			if inString {
				escaped = true
			}
			b.WriteByte(c)
		case '"':
			inString = !inString
			b.WriteByte(c)
		case ',', ']', '}':
			if !inString && c != ',' {
				b.WriteByte(c)
				continue
			}
			if !inString && c == ',' {
				// 前看：跳过空白，若下一个非空白字符是 ] 或 } 则跳过该逗号。
				j := i + 1
				for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
					j++
				}
				if j < len(s) && (s[j] == ']' || s[j] == '}') {
					continue
				}
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// extractJSONObject 提取第一个平衡的 {...} 块（字符串字面量感知）。
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		switch c {
		case '\\':
			if inString {
				escaped = true
			}
		case '"':
			inString = !inString
		case '{':
			if !inString {
				depth++
			}
		case '}':
			if !inString {
				depth--
				if depth == 0 {
					return s[start : i+1]
				}
			}
		}
	}
	return ""
}
