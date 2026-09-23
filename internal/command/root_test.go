// root_test.go — CLI 层冒烟测试（design.md §4）。
package command

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runRoot 在临时目录中执行根命令，捕获 stdout/stderr 与退出码。
// 每次构建全新的命令实例，避免 flag 状态跨用例泄漏。
func runRoot(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	prevWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(prevWd)

	rootCmd := newRootCmd("test")
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(args)
	err := rootCmd.ExecuteContext(t.Context())
	code := exitOK
	if err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			code = ee.code
		} else {
			code = exitFail
		}
		out.WriteString("EXEC_ERROR: " + err.Error())
	}
	return out.String(), code
}

func TestStdinProcessing(t *testing.T) {
	dir := t.TempDir()
	// stdin 模式难以在 cobra 测试中注入，跳过 stdin，直接测文件。
	f := filepath.Join(dir, "a.md")
	os.WriteFile(f, []byte("中文English混排。\n"), 0o644)
	out, code := runRoot(t, dir, "a.md")
	if code != exitOK {
		t.Fatalf("退出码 %d, 输出: %s", code, out)
	}
	if !strings.Contains(out, "中文 English 混排。") {
		t.Errorf("stdout 输出错误: %q", out)
	}
}

func TestInPlaceAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.md")
	os.WriteFile(f, []byte("中文English混排。\n"), 0o644)
	out, code := runRoot(t, dir, "a.md", "-i")
	if code != exitOK {
		t.Fatalf("退出码 %d: %s", code, out)
	}
	data, _ := os.ReadFile(f)
	if !strings.Contains(string(data), "中文 English 混排。") {
		t.Errorf("就地修改失败: %q", data)
	}
	// 临时文件不残留。
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".dellm.tmp") {
			t.Errorf("临时文件残留: %s", e.Name())
		}
	}
}

func TestDiffModeNotWritten(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.md")
	os.WriteFile(f, []byte("中文English混排。\n"), 0o644)
	out, code := runRoot(t, dir, "a.md", "-d")
	if code != exitOK {
		t.Fatalf("退出码 %d: %s", code, out)
	}
	if !strings.Contains(out, "+　　中文 English 混排。") {
		t.Errorf("diff 输出错误:\n%s", out)
	}
	data, _ := os.ReadFile(f)
	if strings.Contains(string(data), "中文 English") {
		t.Error("-d 模式不应写文件")
	}
}

func TestOnlySkipMutuallyExclusive(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.md")
	os.WriteFile(f, []byte("正文。\n"), 0o644)
	_, code := runRoot(t, dir, "a.md", "--only", "quotes", "--skip", "pangu")
	if code != exitCfg {
		t.Errorf("--only/--skip 互斥应返回退出码 2，得到 %d", code)
	}
}

func TestUnknownProfileExitCode2(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.md")
	os.WriteFile(f, []byte("正文。\n"), 0o644)
	_, code := runRoot(t, dir, "a.md", "-p", "no-such")
	if code != exitCfg {
		t.Errorf("未知 profile 应返回退出码 2，得到 %d", code)
	}
}

func TestMultiFileContinueOnError(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.md")
	os.WriteFile(good, []byte("中文English混排。\n"), 0o644)
	// 目录作为输入导致读取失败。
	bad := filepath.Join(dir, "subdir")
	os.MkdirAll(bad, 0o755)
	out, code := runRoot(t, dir, "good.md", "subdir", "-i")
	if code != exitFail {
		t.Errorf("部分失败应返回退出码 1，得到 %d", code)
	}
	if !strings.Contains(out, "good.md") && !strings.Contains(out, "subdir") {
		t.Errorf("应报告失败文件: %s", out)
	}
	data, _ := os.ReadFile(good)
	if !strings.Contains(string(data), "中文 English 混排。") {
		t.Error("好文件应继续处理完成")
	}
}

func TestOutputFileFlag(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.md")
	os.WriteFile(f, []byte("中文English混排。\n"), 0o644)
	dst := filepath.Join(dir, "out.md")
	out, code := runRoot(t, dir, "a.md", "-o", dst)
	if code != exitOK {
		t.Fatalf("退出码 %d: %s", code, out)
	}
	data, _ := os.ReadFile(dst)
	if !strings.Contains(string(data), "中文 English 混排。") {
		t.Errorf("输出文件错误: %q", data)
	}
}
