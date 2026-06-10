// Package gitrunner 在文件系统上执行 git 操作 (clone/fetch/checkout)。
//
// 设计:
//   - 接口可注入,便于测试替换
//   - 真实实现走 shell `git` 命令,免引 go-git 依赖 (依赖少且 worker 容器里要装 git)
package gitrunner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Cloner 抽象 git 操作。
type Cloner interface {
	// Clone 浅克隆指定分支到 dir。dir 必须不存在,否则报错。
	// commitSHA 非空时,clone 后会再 checkout 到该 SHA (避免 race).
	Clone(ctx context.Context, repoURL, branch, commitSHA, dir string) error
}

// ShellCloner 用系统 git 二进制执行 clone。
type ShellCloner struct {
	// GitBinary git 可执行路径,默认 "git"。
	GitBinary string
}

// NewShell 构造,默认查 PATH 里的 git。
func NewShell() *ShellCloner { return &ShellCloner{GitBinary: "git"} }

// Clone 实现。
func (c *ShellCloner) Clone(ctx context.Context, repoURL, branch, commitSHA, dir string) error {
	bin := c.GitBinary
	if bin == "" {
		bin = "git"
	}

	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("workspace dir already exists: %s", dir)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}

	args := []string{"clone", "--depth", "1", "--branch", branch, "--single-branch", repoURL, dir}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", // 拒绝交互式凭据提示
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %v\n%s", err, string(out))
	}

	if commitSHA != "" {
		// 浅克隆默认只拿 HEAD, 再拿目标 SHA
		fetchCmd := exec.CommandContext(ctx, bin, "-C", dir, "fetch", "--depth", "1", "origin", commitSHA)
		if out, err := fetchCmd.CombinedOutput(); err != nil {
			// 容忍 — 大多数情况 HEAD 就是 commitSHA (push event 的 after)
			// 不是的话再 checkout 也会失败,但保留 clone 产物已足够
			_ = out
		}
		coCmd := exec.CommandContext(ctx, bin, "-C", dir, "checkout", commitSHA)
		if out, err := coCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git checkout %s failed: %v\n%s", commitSHA, err, string(out))
		}
	}
	return nil
}
