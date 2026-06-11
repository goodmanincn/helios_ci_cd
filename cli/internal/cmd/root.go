// Package cmd — helios CLI 命令树根。
//
// 子命令分布:
//   - login / logout / whoami         认证 (T8.3.2)
//   - projects                        项目 (T8.3.3)
//   - runs                            执行记录 (T8.3.3)
//   - templates                       流水线模板市场 (M8 T8.2.1 配套)
//
// 全局 flag:
//   --profile  覆盖当前 profile (默认从 config + env 推断)
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/helios-cicd/helios/cli/internal/apiclient"
	"github.com/helios-cicd/helios/cli/internal/config"
)

// Version 由 ldflags 注入。
var Version = "dev"

// profileFlag 全局 --profile 值, 各子命令通过 ResolveClient 拿到。
var profileFlag string

func New() *cobra.Command {
	root := &cobra.Command{
		Use:   "helios",
		Short: "Helios CI/CD 平台命令行工具",
		Long: `helios — Helios CI/CD 平台命令行工具

通过 ` + "`helios login`" + ` 登录后, 即可在终端完成所有平台操作:
查看项目 / 触发流水线 / 看运行日志 / 管理模板.

示例:
  helios login --server https://helios.example.com
  helios projects list
  helios runs logs <run-id>`,
		Version:       Version,
		SilenceUsage:  true, // 子命令 RunE 返错时不要刷一整页 usage
		SilenceErrors: true, // 我们自己打印, 避免重复
	}

	root.PersistentFlags().StringVar(&profileFlag, "profile", "",
		"使用指定 profile (默认: $HELIOS_PROFILE 或 config.default_profile)")

	root.AddCommand(
		newLoginCmd(),
		newLogoutCmd(),
		newWhoamiCmd(),
		newProjectsCmd(),
		newRunsCmd(),
		newPipelinesCmd(),
		newTemplatesCmd(),
		newSecretsCmd(),
		newClustersCmd(),
		newHostsCmd(),
	)
	return root
}

// Execute 入口, main.go 调用。返回 exit code.
func Execute() int {
	if err := New().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}

// resolveClient 解析当前 profile + token, 构造 API client。
// 子命令应该首先调用它; 失败时返回的 error 已经包含友好提示。
func resolveClient() (*apiclient.Client, string, error) {
	name, prof, tok, err := config.Active(profileFlag)
	if err != nil {
		return nil, name, err
	}
	if tok.AccessToken == "" {
		return nil, name, fmt.Errorf("profile %q 没有 token, 请先 `helios login --profile %s`", name, name)
	}
	return apiclient.New(prof.Server, tok.AccessToken, prof.OrgID), name, nil
}
