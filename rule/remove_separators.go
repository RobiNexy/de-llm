// remove_separators.go 实现设计文档 §6.4：删除 Markdown 水平分隔线。
//
// 实现说明（spike 结论，见测试）：goldmark 的 ast.ThematicBreak 节点
// 不携带行位置信息，无法从 AST 直接定位；改为行扫描并用上下文守卫
// 复刻 goldmark 的判定语义：
//   - front matter 区域跳过（keep_frontmatter）
//   - 围栏代码块内的行跳过
//   - 4 空格以上缩进是代码块，跳过
//   - 纯 '-' 下划线且上一行非空 → 是 Setext 标题下划线，跳过
//     （与 goldmark 一致；对围栏后无空行接 --- 的罕见情形会保守地
//     漏删而非误删，取"宁可不删不可误删"）
package rule

import (
	"bytes"

	"github.com/RobiNexy/de-llm/markdown"
)

func init() {
	Register("remove_separators", NewRemoveSeparatorsRule)
}

type removeSeparatorsRule struct {
	keepFrontmatter bool
}

func NewRemoveSeparatorsRule(cfg RuleConfig) (Rule, error) {
	return &removeSeparatorsRule{keepFrontmatter: cfg.Bool("keep_frontmatter", true)}, nil
}

func (r *removeSeparatorsRule) Name() string        { return "remove_separators" }
func (r *removeSeparatorsRule) Description() string { return "删除章节间的水平分隔线" }

func (r *removeSeparatorsRule) Transform(ctx Context, rm *markdown.RegionMap) ([]byte, error) {
	src := rm.Source
	if len(src) == 0 {
		return src, nil
	}

	lines := bytes.Split(src, []byte("\n"))

	// 三态：围栏代码块状态机。
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0

	// 待删除行索引（升序）。
	var dropLines []int

	fmEndLine := -1
	if rm.FrontMatter != nil {
		// front matter 覆盖到其结束分隔线所在行。
		for i, off := 0, 0; i < len(lines); i++ {
			if off+len(lines[i]) >= rm.FrontMatter[1] {
				fmEndLine = i
				break
			}
			off += len(lines[i]) + 1
		}
	}

	prevNonBlank := false // 上一行是否为非空行（setext 守卫）
	for i, line := range lines {
		trimmed := bytes.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		trimmed = bytes.TrimRight(trimmed, "\r")

		inFrontMatter := fmEndLine >= 0 && i <= fmEndLine

		// 围栏状态机。
		if ch, n := fenceOf(bytes.TrimLeft(trimmed, "\t")); ch != 0 || inFence {
			if !inFence {
				if ch != 0 {
					inFence = true
					fenceChar = ch
					fenceLen = n
				}
			} else {
				if ch == fenceChar && n >= fenceLen {
					inFence = false
				}
			}
			prevNonBlank = false
			continue
		}

		if inFrontMatter && r.keepFrontmatter {
			prevNonBlank = len(bytes.TrimSpace(trimmed)) > 0
			continue
		}

		isBreak := indent <= 3 && isThematicBreakLine(trimmed)
		if isBreak && prevNonBlank && isPureDashRun(trimmed) {
			// Setext H2 下划线，不删。
			isBreak = false
		}
		if isBreak {
			dropLines = append(dropLines, i)
			prevNonBlank = false
			continue
		}
		prevNonBlank = len(bytes.TrimSpace(trimmed)) > 0
	}

	ctx.AddStat("removed", len(dropLines))
	if len(dropLines) == 0 {
		return src, nil
	}

	dropSet := map[int]bool{}
	for _, i := range dropLines {
		dropSet[i] = true
	}

	// 删除行后合并空行：对每个删除行，若其上一行与下一非删除行
	// 均为空行，则额外删除一个空行，保证结果只有一个空行。
	for _, i := range dropLines {
		prevBlank := i > 0 && isBlankMdLine(lines[i-1])
		next := i + 1
		for dropSet[next] {
			next++
		}
		if prevBlank && next < len(lines) && isBlankMdLine(lines[next]) {
			dropSet[next] = true
		}
	}

	var out bytes.Buffer
	out.Grow(len(src))
	for i, line := range lines {
		if dropSet[i] {
			continue
		}
		out.Write(line)
		if i < len(lines)-1 {
			out.WriteByte('\n')
		}
	}
	return out.Bytes(), nil
}

// isThematicBreakLine 判断（去除前导空格、行尾 \r 的）行是否为分隔线：
// 仅由同一种 break 字符（- * _）组成，可含单空格分隔，至少 3 个字符。
func isThematicBreakLine(line []byte) bool {
	if len(line) == 0 {
		return false
	}
	ch := line[0]
	if ch != '-' && ch != '*' && ch != '_' {
		return false
	}
	count := 0
	for _, b := range line {
		switch b {
		case ch:
			count++
		case ' ', '\t':
			// 允许分隔空格
		default:
			return false
		}
	}
	return count >= 3
}

// isPureDashRun 判断是否为纯 '-' 下划线（Setext 候选）。
func isPureDashRun(line []byte) bool {
	if len(line) == 0 {
		return false
	}
	for _, b := range line {
		if b != '-' && b != ' ' && b != '\t' {
			return false
		}
	}
	return bytes.ContainsRune(line, '-')
}

func isBlankMdLine(line []byte) bool {
	return len(bytes.TrimSpace(line)) == 0
}

// fenceOf 识别围栏行：返回围栏字符与长度（不足 3 返回 0）。
// 与 markdown 包中同名逻辑一致（围栏语义需在多处使用，暂不导出以收窄 API）。
func fenceOf(trimmed []byte) (byte, int) {
	if len(trimmed) == 0 {
		return 0, 0
	}
	ch := trimmed[0]
	if ch != '`' && ch != '~' {
		return 0, 0
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0
	}
	return ch, n
}
