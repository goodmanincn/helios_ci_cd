// executor.go — 远程命令执行 (流式 stdout/stderr + 超时 + 中断)。
package sshrunner

import (
	"context"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/ssh"
)

// ExecSpec 执行参数。
type ExecSpec struct {
	Command string        // 要执行的命令
	Timeout time.Duration // 0 = 无超时
	Env     map[string]string
}

// ExecResult 执行结果。
type ExecResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

// Executor 在已有 SSH 连接上执行命令。
type Executor struct {
	client *ssh.Client
}

// NewExecutor 从 Client 构造 Executor。
func NewExecutor(c *Client) *Executor {
	return &Executor{client: c.Raw()}
}

// Exec 执行命令，返回完整输出 (简化版，不流式)。
func (e *Executor) Exec(ctx context.Context, spec ExecSpec) (*ExecResult, error) {
	session, err := e.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	// 环境变量
	for k, v := range spec.Env {
		_ = session.Setenv(k, v) // 部分服务器会拒绝，忽略错误
	}

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := session.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	// 启动命令
	if err := session.Start(spec.Command); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	// 带超时的 context
	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, spec.Timeout)
		defer cancel()
	}

	// 并发读取 stdout/stderr
	outCh := make(chan []byte, 1)
	errCh := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(stdoutPipe); outCh <- b }()
	go func() { b, _ := io.ReadAll(stderrPipe); errCh <- b }()

	// 等待命令完成或 context 取消
	done := make(chan error, 1)
	go func() { done <- session.Wait() }()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return nil, fmt.Errorf("timeout: %w", ctx.Err())
	case err := <-done:
		res := &ExecResult{
			Stdout: <-outCh,
			Stderr: <-errCh,
		}
		if exitErr, ok := err.(*ssh.ExitError); ok {
			res.ExitCode = exitErr.ExitStatus()
		} else if err != nil {
			return nil, fmt.Errorf("wait: %w", err)
		}
		return res, nil
	}
}

// ExecStream 流式执行，实时回调每一行输出。
type LineCallback func(stream string, line string)

// ExecStream 执行命令并通过 callback 逐行回调 stdout/stderr。
func (e *Executor) ExecStream(ctx context.Context, spec ExecSpec, cb LineCallback) (*ExecResult, error) {
	session, err := e.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	for k, v := range spec.Env {
		_ = session.Setenv(k, v)
	}

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := session.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := session.Start(spec.Command); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, spec.Timeout)
		defer cancel()
	}

	// 并发读取 + 行缓冲
	outCh := make(chan []byte, 1)
	errCh := make(chan []byte, 1)
	go func() {
		b := readLines(stdoutPipe, "stdout", cb)
		outCh <- b
	}()
	go func() {
		b := readLines(stderrPipe, "stderr", cb)
		errCh <- b
	}()

	done := make(chan error, 1)
	go func() { done <- session.Wait() }()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return nil, fmt.Errorf("timeout: %w", ctx.Err())
	case err := <-done:
		res := &ExecResult{
			Stdout: <-outCh,
			Stderr: <-errCh,
		}
		if exitErr, ok := err.(*ssh.ExitError); ok {
			res.ExitCode = exitErr.ExitStatus()
		} else if err != nil {
			return nil, fmt.Errorf("wait: %w", err)
		}
		return res, nil
	}
}
