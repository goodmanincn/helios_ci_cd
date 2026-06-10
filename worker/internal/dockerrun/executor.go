package dockerrun

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// LogStream 标记日志来源。
type LogStream int

const (
	LogStdout LogStream = 1
	LogStderr LogStream = 2
)

func (s LogStream) String() string {
	if s == LogStderr {
		return "stderr"
	}
	return "stdout"
}

// LogLine 是一条容器日志(已按行切分)。
//
// 注:Docker daemon 的 ContainerLogs 流式数据并不严格按行分块,
// Reader 内部做行缓冲(\n 切),并把 stream/timestamp 一起填好。
type LogLine struct {
	Stream LogStream
	Ts     time.Time
	Line   string // 不含末尾 \n
}

// LogSink 接收一条条日志。回调里别 block 太久;executor 用同一个 goroutine 推。
//
// 实现示例:
//
//	func(l LogLine) error { logFile.WriteString(l.Line); return nil }
type LogSink func(LogLine) error

// RunSpec 描述一次容器化执行。
type RunSpec struct {
	Image string   // 镜像 tag, 必填
	Cmd   []string // entrypoint 命令 (传入 ENTRYPOINT), e.g. []string{"bash","-c","make build"}
	Env   []string // KEY=VAL 形式

	// 挂载:KEY=host 绝对路径, VAL=容器内绝对路径;通常把 workspace 挂到 /workspace。
	Mounts map[string]string

	WorkDir string // 容器内工作目录, 通常 = workspace 挂载点

	// 资源限制(0 = 不限)
	CPUNanos int64 // 纳秒 CPU 配额, 1 CPU = 1e9。例如 2e9 = 2 个核
	MemBytes int64 // 内存上限, 字节

	// PullPolicy: always / missing(默认) / never
	PullPolicy string

	// AutoRemove: true 时 docker daemon 退出后自动删容器(优先使用,避免 leak)
	AutoRemove bool

	// 容器名前缀, 默认 "helios-job-"。最终名 = prefix+UnixNano+rand。
	NamePrefix string

	// NetworkMode: 默认 "bridge", 可设 "none" 提高隔离。
	NetworkMode string
}

// RunResult 一次执行的结果。
type RunResult struct {
	ContainerID string
	ExitCode    int
	Duration    time.Duration
	TimedOut    bool // ctx 超时 或 daemon 报 timeout
}

// Executor 容器化任务执行器。
type Executor struct {
	c *Client
}

// NewExecutor 用现成 Client 构造 Executor。
func NewExecutor(c *Client) *Executor { return &Executor{c: c} }

// Run 执行一次容器化任务。整个生命周期:
//
//	pull (按 policy) → create → start → tail logs (异步) → wait → remove
//
// 行为:
//   - sink 同步收行;失败立刻终止 tail goroutine(但不杀容器,容器照常跑完,只是不再读日志了)
//   - ctx 超时 → docker stop + remove,RunResult.TimedOut=true,ExitCode=-1
//   - 容器自身退出(任意 exit code 都算成功 Run,只是 ExitCode 不为 0 → 调用方判)
//   - 总是 best-effort remove 容器(即便 AutoRemove=true 也再尝一次, 防 leak)
func (e *Executor) Run(ctx context.Context, spec RunSpec, sink LogSink) (*RunResult, error) {
	if e == nil || e.c == nil || e.c.raw == nil {
		return nil, errors.New("executor: nil client")
	}
	if spec.Image == "" {
		return nil, errors.New("executor: image required")
	}
	cli := e.c.raw
	start := time.Now()

	// 1) Pull
	if err := e.ensureImage(ctx, cli, spec); err != nil {
		return nil, err
	}

	// 2) Create container
	cName := spec.NamePrefix
	if cName == "" {
		cName = "helios-job-"
	}
	cName = fmt.Sprintf("%s%d", cName, time.Now().UnixNano())

	hostCfg, err := buildHostConfig(spec)
	if err != nil {
		return nil, err
	}
	cfg := &container.Config{
		Image:        spec.Image,
		Cmd:          spec.Cmd,
		Env:          spec.Env,
		WorkingDir:   spec.WorkDir,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false, // 必须 false 才能用 stdcopy 拆 stdout/stderr
	}
	created, err := cli.ContainerCreate(ctx, cfg, hostCfg, &network.NetworkingConfig{}, nil, cName)
	if err != nil {
		return nil, fmt.Errorf("container create (image=%s): %w", spec.Image, err)
	}
	cid := created.ID

	// 4) 清理(总是执行)
	// 注: spec.AutoRemove 已被强制忽略 — daemon 端 auto-remove 跟 ContainerWait/Logs 竞争,
	//     会把日志/退出码吃掉。这里始终自己 stop + remove。
	defer func() {
		_ = cli.ContainerStop(context.Background(), cid, container.StopOptions{Timeout: ptrInt(5)})
		_ = cli.ContainerRemove(context.Background(), cid, container.RemoveOptions{Force: true})
	}()

	// 5) Start
	if err := cli.ContainerStart(ctx, cid, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("container start (id=%s): %w", cid, err)
	}

	// 6) 注册 wait (用 background ctx, 让 wait 不被主 ctx 抢先取消;
	//     主 ctx 超时由下面 select 接管, ContainerStop 触发 wait 收到 status)
	waitCtx, cancelWait := context.WithCancel(context.Background())
	defer cancelWait()
	statusCh, errCh := cli.ContainerWait(waitCtx, cid, container.WaitConditionNotRunning)

	// 7) Tail logs(异步)。
	// 关键: 用 background ctx 让 tail 跑到 EOF, 不被主 ctx 提前 cancel —
	//       否则容器秒退、缓冲日志还没读完就丢了。
	logsDone := make(chan error, 1)
	go func() { logsDone <- tailLogs(context.Background(), cli, cid, sink) }()

	// 8) Wait
	res := &RunResult{ContainerID: cid}
	var waitErr error
	select {
	case <-ctx.Done():
		// 主 ctx 取消(超时 / 用户取消)→ 主动 stop, daemon 关 logs stream → tailLogs EOF
		_ = cli.ContainerStop(context.Background(), cid, container.StopOptions{Timeout: ptrInt(5)})
		res.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
		res.ExitCode = -1
		waitErr = ctx.Err()

	case st := <-statusCh:
		res.ExitCode = int(st.StatusCode)
		if st.Error != nil && st.Error.Message != "" {
			waitErr = fmt.Errorf("container wait error: %s", st.Error.Message)
		}

	case err := <-errCh:
		waitErr = fmt.Errorf("container wait: %w", err)
	}

	// 9) 等日志 tail 退出(daemon 在容器停止时关闭 stream → io.EOF → tailLogs 返回)
	// 给一个上限,防止 daemon 异常时永远等
	select {
	case <-logsDone:
	case <-time.After(10 * time.Second):
		// 兜底: 强制 stop 让 daemon 释放 stream
		_ = cli.ContainerStop(context.Background(), cid, container.StopOptions{Timeout: ptrInt(1)})
		<-logsDone
	}

	res.Duration = time.Since(start)
	return res, waitErr
}

func (e *Executor) ensureImage(ctx context.Context, cli *dockerclient.Client, spec RunSpec) error {
	policy := strings.ToLower(strings.TrimSpace(spec.PullPolicy))
	if policy == "" {
		policy = "missing"
	}
	switch policy {
	case "never":
		return nil
	case "missing":
		// 探一下镜像存在不
		_, _, err := cli.ImageInspectWithRaw(ctx, spec.Image)
		if err == nil {
			return nil
		}
		// 不存在 → fallthrough 到 pull
	case "always":
		// fallthrough
	default:
		return fmt.Errorf("unknown PullPolicy %q (want always|missing|never)", spec.PullPolicy)
	}

	rc, err := cli.ImagePull(ctx, spec.Image, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("image pull %s: %w", spec.Image, err)
	}
	// 必须读干, 否则下层 connection 不会复用
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
	return nil
}

func buildHostConfig(spec RunSpec) (*container.HostConfig, error) {
	hc := &container.HostConfig{
		// 强制 false: AutoRemove 会让 daemon 在退出瞬间清掉容器,
		// 跟我们的 ContainerWait / ContainerLogs 抢资源, 导致退出码=0 + 日志丢。
		// executor 自己负责 stop + remove。
		AutoRemove:  false,
		NetworkMode: container.NetworkMode(spec.NetworkMode),
		Resources: container.Resources{
			NanoCPUs: spec.CPUNanos,
			Memory:   spec.MemBytes,
		},
	}
	if hc.NetworkMode == "" {
		hc.NetworkMode = "bridge"
	}
	for hostPath, ctrPath := range spec.Mounts {
		if !strings.HasPrefix(hostPath, "/") {
			return nil, fmt.Errorf("mount host path must be absolute: %q", hostPath)
		}
		if !strings.HasPrefix(ctrPath, "/") {
			return nil, fmt.Errorf("mount container path must be absolute: %q", ctrPath)
		}
		hc.Mounts = append(hc.Mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: hostPath,
			Target: ctrPath,
		})
	}
	return hc, nil
}

// tailLogs 拉容器日志, 用 stdcopy 拆 stdout/stderr, 按行切, 推给 sink。
func tailLogs(ctx context.Context, cli *dockerclient.Client, cid string, sink LogSink) error {
	if sink == nil {
		return nil
	}
	rc, err := cli.ContainerLogs(ctx, cid, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
	})
	if err != nil {
		return fmt.Errorf("container logs: %w", err)
	}
	defer rc.Close()

	stdoutW := &lineWriter{stream: LogStdout, sink: sink}
	stderrW := &lineWriter{stream: LogStderr, sink: sink}

	// stdcopy.StdCopy 内部循环读;ctx 取消时 rc 会被外面 close → io.EOF
	if _, err := stdcopy.StdCopy(stdoutW, stderrW, rc); err != nil {
		// EOF / context canceled 都不算错
		if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
			err = nil
		}
		// stdcopy 自定义错误:容错处理
		if err != nil && strings.Contains(err.Error(), "use of closed network connection") {
			err = nil
		}
		// flush 剩余未换行的内容
		stdoutW.flush()
		stderrW.flush()
		return err
	}
	stdoutW.flush()
	stderrW.flush()
	return nil
}

// lineWriter 把 daemon 推的字节流按 '\n' 切成 LogLine 转发到 sink。
//
// 容器 stdout/stderr 大块到达时, 用 buf 缓存到下一个 \n 才推。
// flush() 在流结束时强推一行残余(防止丢最后一行不带换行的输出)。
type lineWriter struct {
	stream   LogStream
	sink     LogSink
	buf      []byte
	sinkErr  error
}

func (w *lineWriter) Write(p []byte) (int, error) {
	if w.sinkErr != nil {
		return len(p), nil // 静默吞,executor 不应被 sink 错误连累
	}
	n := len(p)
	w.buf = append(w.buf, p...)
	for {
		i := indexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		// 去掉 \r (Windows 输出常带 \r\n)
		line = strings.TrimRight(line, "\r")
		if err := w.sink(LogLine{Stream: w.stream, Ts: time.Now(), Line: line}); err != nil {
			w.sinkErr = err
			w.buf = w.buf[i+1:]
			return n, nil
		}
		w.buf = w.buf[i+1:]
	}
	return n, nil
}

func (w *lineWriter) flush() {
	if len(w.buf) == 0 || w.sinkErr != nil {
		return
	}
	line := strings.TrimRight(string(w.buf), "\r")
	_ = w.sink(LogLine{Stream: w.stream, Ts: time.Now(), Line: line})
	w.buf = nil
}

func indexByte(b []byte, c byte) int {
	for i := 0; i < len(b); i++ {
		if b[i] == c {
			return i
		}
	}
	return -1
}

func ptrInt(v int) *int { return &v }

// 占位:导入 encoding/binary 避免 unused (后续如果手工解 docker stream multiplexed 头会用到)
var _ = binary.BigEndian
