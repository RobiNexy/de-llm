// root_test.go — CLI 层冒烟测试（design.md §4）。
package command

import (
	"bytes"
	"encoding/json"
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

func TestDryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.md")
	if err := os.WriteFile(f, []byte("中文English\"引号\"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	out, code := runRoot(t, dir, "a.md", "--dry-run")
	if code != exitOK {
		t.Fatalf("退出码 %d: %s", code, out)
	}
	after, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("dry-run 不应改变 mtime")
	}
	if before.Mode() != after.Mode() {
		t.Errorf("dry-run 不应改变 mode: %v -> %v", before.Mode(), after.Mode())
	}
}

func TestDryRunRejectsWriteFlags(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.md")
	os.WriteFile(f, []byte("正文。\n"), 0o644)
	_, code := runRoot(t, dir, "a.md", "--dry-run", "-i")
	if code != exitCfg {
		t.Errorf("--dry-run 与 -i 应互斥，得到退出码 %d", code)
	}
}

func TestInPlacePreservesMode(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.md")
	if err := os.WriteFile(f, []byte("中文\"引号\"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	out, code := runRoot(t, dir, "a.md", "-i")
	if code != exitOK {
		t.Fatalf("退出码 %d: %s", code, out)
	}
	fi, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("就地修改应保留权限 640，得到 %o", fi.Mode().Perm())
	}
}

func TestJSONIncludesStatsAndWarnings(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.md")
	os.WriteFile(f, []byte("\"未闭合的引号\n"), 0o644)
	out, code := runRoot(t, dir, "a.md", "--format", "json")
	if code != exitOK {
		t.Fatalf("退出码 %d: %s", code, out)
	}
	var payload struct {
		Modified string                    `json:"modified"`
		Stats    map[string]map[string]int `json:"stats"`
		Warnings []string                  `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("JSON 解析失败: %v\n%s", err, out)
	}
	if len(payload.Warnings) == 0 {
		t.Error("JSON 应包含 quotes 的未闭合引号 warning")
	}
	if len(payload.Stats["quotes"]) == 0 {
		t.Errorf("JSON 应包含 quotes 统计: %+v", payload.Stats)
	}
}

func TestMultiFileJSONIsArray(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("中文English。\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("第二篇English。\n"), 0o644)
	out, code := runRoot(t, dir, "a.md", "b.md", "--format", "json")
	if code != exitOK {
		t.Fatalf("退出码 %d: %s", code, out)
	}
	var payload []map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("多文件 JSON 应为数组: %v\n%s", err, out)
	}
	if len(payload) != 2 {
		t.Errorf("数组长度 %d != 2", len(payload))
	}
}

func TestFrontMatterRulesDoNotLeakAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.md")
	second := filepath.Join(dir, "b.md")
	os.WriteFile(first, []byte("---\ndellm:\n  rules:\n    quotes:\n      single_quotes: false\n---\n\n'first'\n"), 0o644)
	os.WriteFile(second, []byte("'second'\n"), 0o644)
	out, code := runRoot(t, dir, "a.md", "b.md", "--only", "quotes")
	if code != exitOK {
		t.Fatalf("退出码 %d: %s", code, out)
	}
	if !strings.Contains(out, "\u2018second\u2019") {
		t.Errorf("front matter 规则不应泄漏到第二个文件: %q", out)
	}
}
