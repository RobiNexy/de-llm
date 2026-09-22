// diff.go 生成 unified diff（--diff 模式）。
//
// 实现选型：标准库无 diff；引入第三方库只为一处 CLI 输出不成比例。
// 这里实现基于 LCS 的行级 diff——文档规模（千行级）下代价可接受；
// 超大输入退化为整体替换输出，保证有界。
package pipeline

import (
	"fmt"
	"strings"
)

// UnifiedDiff 生成 old→new 的 unified diff，context 为上下文行数。
// 无差异时返回空串。
func UnifiedDiff(oldName, newName string, oldSrc, newSrc []byte, context int) string {
	a := splitLines(string(oldSrc))
	b := splitLines(string(newSrc))
	ops := diffOps(a, b)
	if !hasChanges(ops) {
		return ""
	}
	if context < 0 {
		context = 0
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", oldName, newName)

	// 变更点分组：间隔 ≤ 2*context 的变更归入同一 hunk。
	type hunk struct{ start, end int } // ops 索引区间，含 start 不含 end
	var hunks []hunk
	i := 0
	for i < len(ops) {
		if ops[i].kind == opEqual {
			i++
			continue
		}
		start := i
		end := i + 1
		for end < len(ops) {
			if ops[end].kind != opEqual {
				end++
				continue
			}
			// 数连续 equal 长度。
			run := 0
			for end+run < len(ops) && ops[end+run].kind == opEqual {
				run++
			}
			if run > 2*context {
				break
			}
			end += run
		}
		hunks = append(hunks, hunk{start, end})
		i = end
	}

	for _, h := range hunks {
		hs := h.start - context
		if hs < 0 {
			hs = 0
		}
		he := h.end + context
		if he > len(ops) {
			he = len(ops)
		}
		aBefore, bBefore := 0, 0
		for k := 0; k < hs; k++ {
			switch ops[k].kind {
			case opEqual:
				aBefore++
				bBefore++
			case opDelete:
				aBefore++
			case opInsert:
				bBefore++
			}
		}
		aLen, bLen := 0, 0
		for k := hs; k < he; k++ {
			switch ops[k].kind {
			case opEqual:
				aLen++
				bLen++
			case opDelete:
				aLen++
			case opInsert:
				bLen++
			}
		}
		aStart, bStart := aBefore+1, bBefore+1
		if aLen == 0 {
			aStart--
		}
		if bLen == 0 {
			bStart--
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", aStart, aLen, bStart, bLen)
		for k := hs; k < he; k++ {
			switch ops[k].kind {
			case opEqual:
				sb.WriteByte(' ')
			case opDelete:
				sb.WriteByte('-')
			case opInsert:
				sb.WriteByte('+')
			}
			sb.WriteString(ops[k].line)
		}
	}
	return sb.String()
}

func hasChanges(ops []diffOp) bool {
	for _, o := range ops {
		if o.kind != opEqual {
			return true
		}
	}
	return false
}

type opKind int

const (
	opEqual opKind = iota
	opDelete
	opInsert
)

type diffOp struct {
	kind opKind
	line string
}

// splitLines 按行切分并保留换行符。
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	raw := strings.Split(s, "\n")
	lines := raw
	// 以 \n 结尾的文本 Split 产生末尾空串，丢弃。
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l + "\n"
	}
	return out
}

// diffOps 计算 LCS 编辑脚本（动态规划）。
// n*m 超过阈值时退化为整体删除+插入，保证内存有界。
func diffOps(a, b []string) []diffOp {
	n, m := len(a), len(b)
	const maxCells = 4 << 20 // 4M cells
	if n*m > maxCells {
		return fallbackOps(a, b)
	}
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{opEqual, a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{opDelete, a[i]})
			i++
		default:
			ops = append(ops, diffOp{opInsert, b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{opDelete, a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{opInsert, b[j]})
	}
	return ops
}

func fallbackOps(a, b []string) []diffOp {
	ops := make([]diffOp, 0, len(a)+len(b))
	for _, l := range a {
		ops = append(ops, diffOp{opDelete, l})
	}
	for _, l := range b {
		ops = append(ops, diffOp{opInsert, l})
	}
	return ops
}
