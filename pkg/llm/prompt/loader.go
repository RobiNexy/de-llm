// loader.go 提供文件系统访问的小型适配（避免引入 os 依赖到测试）。
package prompt

import (
	"embed"
	"io"
	"io/fs"
	"os"
)

type osFS struct{}

func (osFS) Open(name string) (fs.File, error) { return os.Open(name) }

// ReadDir 委托给 os.ReadDir 的 fs.ReadDir 适配。
func ReadDir(f fs.FS, name string) ([]fs.DirEntry, error) { return fs.ReadDir(f, name) }

func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// Builtin 返回编译期内置提示词（go:embed，设计文档 §8.6）。
//
//go:embed builtin/*.md
var Builtin embed.FS
