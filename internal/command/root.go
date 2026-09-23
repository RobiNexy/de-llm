// Package cmd 实现设计文档 §4 的 CLI 接口。
//
// 实现说明：rootCmd 由 newRootCmd() 每次构建而非包级单例——
// flag 值绑定在包级变量上，重复构建会将其重置为默认值，
// 避免进程内多次 Execute（测试场景）之间的全局状态泄漏。
package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RobiNexy/de-llm/pkg/config"
	"github.com/RobiNexy/de-llm/pkg/output"
	"github.com/RobiNexy/de-llm/pkg/pipeline"
)

// 全局 flag 变量；每次 newRootCmd() 重新绑定为默认值。
var (
	flagConfig        string
	flagProfile       string
	flagOutput        string
	flagInPlace       bool
	flagDiff          bool
	flagDryRun        bool
	flagOnly          string
	flagSkip          string
	flagSkipLLM       bool
	flagShiftHeadings int
	flagStats         bool
	flagVerbose       bool
	flagFormat        string
	flagQuiet         bool
	flagLogLevel      string
)

// 退出码（设计文档 §10.2）。
const (
	exitOK   = 0
	exitFail = 1
	exitCfg  = 2
)

// newRootCmd 构建根命令（含 flag 注册与子命令挂载）。
func newRootCmd(version string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "dellm [flags] [file...]",
		Short: "去除 LLM 生成的中文 Markdown 文档中的 AI 味",
		Long: `DeLLM 通过本地规范化规则和可编排的 LLM 改写步骤，
将 LLM 输出转化为符合中文排版习惯、风格自然的文档。

三段式流水线：
  ① 预处理（本地规则）→ ② LLM 改写 → ③ 后处理（本地规则）

零配置即可运行（仅本地规则）；配置文件可编排 LLM 步骤。
参见 https://github.com/RobiNexy/de-llm`,
		Example: `  dellm input.md                          # 输出到 stdout
  dellm input.md -o output.md             # 输出到文件
  dellm input.md -i                       # 就地修改
  cat input.md | dellm -                  # 从 stdin 读取
  dellm *.md -i                           # 批量就地修改
  dellm input.md -p full                  # 使用 full profile
  dellm input.md --only quotes,pangu      # 只做引号和盘古之白
  dellm input.md --skip-llm               # 跳过 LLM 步骤
  dellm input.md -p full --dry-run        # 预览 full profile 会做什么
  dellm input.md --shift-headings=-1 -i   # 快捷：所有标题提升一级
  dellm list-rules                        # 列出可用规则
  dellm list-prompts                      # 列出可用提示词`,
		Args:          cobra.ArbitraryArgs,
		RunE:          run,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}

	f := rootCmd.Flags()
	f.StringVarP(&flagConfig, "config", "c", "", "配置文件路径")
	f.StringVarP(&flagProfile, "profile", "p", "default", "使用指定 profile")
	f.StringVarP(&flagOutput, "output", "o", "", "输出文件路径（仅单文件输入时有效）")
	f.BoolVarP(&flagInPlace, "in-place", "i", false, "就地修改")
	f.BoolVarP(&flagDiff, "diff", "d", false, "输出 unified diff，不实际写入")
	f.BoolVar(&flagDryRun, "dry-run", false, "报告将执行的操作，不实际处理")
	f.StringVar(&flagOnly, "only", "", "只运行指定规则或步骤（逗号分隔）")
	f.StringVar(&flagSkip, "skip", "", "跳过指定规则或步骤（逗号分隔）")
	f.BoolVar(&flagSkipLLM, "skip-llm", false, "跳过所有 LLM 步骤")
	f.IntVar(&flagShiftHeadings, "shift-headings", 0, "快捷方式：临时启用 shift_headings 并设置 offset")
	f.BoolVar(&flagStats, "stats", false, "输出处理统计")
	f.BoolVarP(&flagVerbose, "verbose", "v", false, "详细日志")
	f.StringVar(&flagFormat, "format", "text", "输出格式: text|json")
	f.BoolVarP(&flagQuiet, "quiet", "q", false, "静默模式（只输出错误）")
	f.StringVar(&flagLogLevel, "log-level", "info", "日志级别: debug|info|warn|error")

	rootCmd.AddCommand(newListRulesCmd(), newListPromptsCmd())
	return rootCmd
}

// Execute 运行 CLI 并返回退出码。
func Execute(version string) int {
	rootCmd := newRootCmd(version)
	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		var exitErr *exitError
		if errors.As(err, &exitErr) {
			fmt.Fprintln(os.Stderr, "错误:", exitErr.msg)
			return exitErr.code
		}
		fmt.Fprintln(os.Stderr, "错误:", err)
		return exitFail
	}
	return exitOK
}

type exitError struct {
	msg  string
	code int
}

func (e *exitError) Error() string { return e.msg }

func cfgErr(format string, args ...any) error {
	return &exitError{msg: fmt.Sprintf(format, args...), code: exitCfg}
}

func failErr(format string, args ...any) error {
	return &exitError{msg: fmt.Sprintf(format, args...), code: exitFail}
}

// run 是根命令主流程。
func run(cmd *cobra.Command, args []string) error {
	if _, err := output.Get(flagFormat); err != nil {
		return cfgErr("%v", err)
	}
	if flagLogLevel != "debug" && flagLogLevel != "info" && flagLogLevel != "warn" && flagLogLevel != "error" {
		return cfgErr("不支持的日志级别: %s", flagLogLevel)
	}
	// --only / --skip 互斥（设计文档 §4.2）。
	if flagOnly != "" && flagSkip != "" {
		return cfgErr("--only 与 --skip 互斥，不能同时指定")
	}

	// 加载配置（内置默认 ← 用户级 ← 项目级；CLI flags 最高）。
	cfg, err := config.Load(config.Options{ConfigPath: flagConfig})
	if err != nil {
		return cfgErr("%v", err)
	}

	profileName := flagProfile
	if profileName == "" {
		profileName = "default"
	}

	useStdin := len(args) == 0 || (len(args) == 1 && args[0] == "-")
	if !useStdin && flagOutput != "" && len(args) > 1 {
		return cfgErr("-o 仅在单文件输入时有效")
	}
	if useStdin && flagInPlace {
		return cfgErr("-i 不能用于 stdin 输入")
	}

	if useStdin {
		source, err := io.ReadAll(os.Stdin)
		if err != nil {
			return failErr("读取 stdin 失败: %v", err)
		}
		result, err := processBytes(cfg, profileName, source, "<stdin>")
		if err != nil {
			return err
		}
		return emitOutput(cmd, result, "", &fileResult{orig: source, output: result})
	}

	// 多文件：单文件失败继续（设计文档 §4.5）。
	// 配置类错误（退出码 2）立即终止；处理失败继续并最终返回 1。
	hadFailure := false
	for _, path := range args {
		result, err := processFile(cfg, profileName, path)
		if err != nil {
			cmd.PrintErrf("错误: %s: %v\n", path, err)
			var exitErr *exitError
			if errors.As(err, &exitErr) && exitErr.code == exitCfg {
				return err
			}
			hadFailure = true
			continue
		}
		if err := emitOutput(cmd, result.output, path, result); err != nil {
			cmd.PrintErrf("错误: %s: %v\n", path, err)
			hadFailure = true
		}
	}
	if hadFailure {
		return failErr("部分文件处理失败")
	}
	return nil
}

type fileResult struct {
	output []byte
	orig   []byte
	path   string
}

// emitOutput 按模式输出：diff / stdout / -o / -i。
func emitOutput(cmd *cobra.Command, result []byte, path string, fr *fileResult) error {
	switch {
	case flagDiff:
		name := path
		if name == "" {
			name = "<stdin>"
		}
		orig := []byte(nil)
		if fr != nil {
			orig = fr.orig
		}
		fmt.Fprint(cmd.OutOrStdout(), pipeline.UnifiedDiff("a/"+name, "b/"+name, orig, result, 3))
		return nil
	case flagFormat == "json":
		original := []byte(nil)
		warnings := []string(nil)
		if fr != nil {
			original = fr.orig
		}
		formatted, err := (output.JSONFormatter{}).Format(&output.Result{Original: original, Modified: result, Warnings: warnings})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(formatted))
		return err
	case flagInPlace:
		if fr == nil {
			return failErr("-i 不能用于 stdin")
		}
		return writeFileAtomic(fr.path, result)
	case flagOutput != "":
		return writeFileAtomic(flagOutput, result)
	default:
		fmt.Fprint(cmd.OutOrStdout(), string(result))
		return nil
	}
}

// writeFileAtomic 就地写入的安全流程（设计文档 §4.4）：
// 临时文件 → fsync → rename 原子替换；任何失败删除临时文件，原文件不受影响。
func writeFileAtomic(path string, data []byte) error {
	dir := "."
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		dir = path[:i]
	}
	tmp, err := os.CreateTemp(dir, ".dellm.tmp.XXXXXX")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	tmpName = "" // rename 成功，无需清理
	return nil
}
