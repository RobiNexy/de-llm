// DeLLM 入口。CLI 逻辑见 cmd 包。
package main

import (
	"os"

	"github.com/RobiNexy/de-llm/internal/command"
)

// version 由构建注入：go build -ldflags "-X main.version=v1.0.0"。
var version = "dev"

func main() {
	os.Exit(command.Execute(version))
}
