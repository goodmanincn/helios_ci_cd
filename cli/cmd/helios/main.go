// Package main helios CLI 入口.
//
// 实际命令树定义在 internal/cmd, main 只做 ldflags 注入 + 启动.
package main

import (
	"os"

	"github.com/helios-cicd/helios/cli/internal/cmd"
)

// Version / BuildTime / GitCommit 由 ldflags 注入, 默认 dev.
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	cmd.Version = Version
	os.Exit(cmd.Execute())
}
