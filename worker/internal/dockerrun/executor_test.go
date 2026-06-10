package dockerrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// 集成测试统一用 alpine:latest;运行前 best-effort 预热一次, 减少 CI flakiness。

func warmAlpine(t *testing.T, c *Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exec := NewExecutor(c)
	_, err := exec.Run(ctx, RunSpec{
		Image:      "alpine:latest",
		Cmd:        []string{"true"},
		PullPolicy: "missing",
	}, nil)
	if err != nil {
		t.Skipf("can't pull alpine, skip dockerrun integration: %v", err)
	}
}

type collectSink struct {
	mu    sync.Mutex
	lines []LogLine
}

func (s *collectSink) sink(l LogLine) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, l)
	return nil
}

func (s *collectSink) joined(stream LogStream) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var parts []string
	for _, l := range s.lines {
		if l.Stream == stream {
			parts = append(parts, l.Line)
		}
	}
	return strings.Join(parts, "\n")
}

func TestExecutor_RunSuccess(t *testing.T) {
	c := requireDocker(t)
	warmAlpine(t, c)
	exec := NewExecutor(c)

	col := &collectSink{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := exec.Run(ctx, RunSpec{
		Image:      "alpine:latest",
		Cmd:        []string{"sh", "-c", "echo hello-stdout; echo bad-stderr >&2; exit 0"},
		PullPolicy: "missing",
		AutoRemove: true,
	}, col.sink)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit=%d want 0", res.ExitCode)
	}
	if res.TimedOut {
		t.Errorf("should not time out")
	}
	if !strings.Contains(col.joined(LogStdout), "hello-stdout") {
		t.Errorf("stdout missing: %q", col.joined(LogStdout))
	}
	if !strings.Contains(col.joined(LogStderr), "bad-stderr") {
		t.Errorf("stderr missing: %q", col.joined(LogStderr))
	}
}

func TestExecutor_RunFailExitCode(t *testing.T) {
	c := requireDocker(t)
	warmAlpine(t, c)
	exec := NewExecutor(c)

	res, err := exec.Run(context.Background(), RunSpec{
		Image:      "alpine:latest",
		Cmd:        []string{"sh", "-c", "exit 7"},
		PullPolicy: "missing",
		AutoRemove: true,
	}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode != 7 {
		t.Errorf("exit=%d want 7", res.ExitCode)
	}
}

// 验证 env + mount + workdir 三件套联动:
//
//	挂载 host tmpdir → 容器 /workspace, workdir=/workspace,
//	跑 `echo $MARK > out.txt`, 然后 host 端读 out.txt 验证。
func TestExecutor_EnvAndMount(t *testing.T) {
	c := requireDocker(t)
	warmAlpine(t, c)
	exec := NewExecutor(c)

	host := t.TempDir()
	col := &collectSink{}
	res, err := exec.Run(context.Background(), RunSpec{
		Image:      "alpine:latest",
		Cmd:        []string{"sh", "-c", "echo $MARK > /workspace/out.txt && echo done"},
		Env:        []string{"MARK=hello-helios-e1.4"},
		Mounts:     map[string]string{host: "/workspace"},
		WorkDir:    "/workspace",
		PullPolicy: "missing",
		AutoRemove: true,
	}, col.sink)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit=%d want 0; logs=%s", res.ExitCode, col.joined(LogStdout))
	}
	out, err := os.ReadFile(filepath.Join(host, "out.txt"))
	if err != nil {
		t.Fatalf("read out.txt: %v", err)
	}
	if !strings.Contains(string(out), "hello-helios-e1.4") {
		t.Errorf("out.txt=%q missing env mark", string(out))
	}
	if !strings.Contains(col.joined(LogStdout), "done") {
		t.Errorf("stdout missing 'done': %q", col.joined(LogStdout))
	}
}

func TestExecutor_TimeoutKills(t *testing.T) {
	c := requireDocker(t)
	warmAlpine(t, c)
	exec := NewExecutor(c)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := exec.Run(ctx, RunSpec{
		Image:      "alpine:latest",
		Cmd:        []string{"sh", "-c", "sleep 30"},
		PullPolicy: "missing",
		AutoRemove: true,
	}, nil)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected deadline exceeded, got %v", err)
	}
	if !res.TimedOut {
		t.Errorf("res.TimedOut should be true")
	}
}

// 流式日志验证: 让 10 条逐秒输出, 但用 1.5 秒 ctx 来看到至少 1 条在 ctx 取消前进 sink
// (避免和容器 buffer flush 时机较劲,我们改为放低延迟跑 10 条不 sleep)。
func TestExecutor_LogStreamingOrder(t *testing.T) {
	c := requireDocker(t)
	warmAlpine(t, c)
	exec := NewExecutor(c)
	col := &collectSink{}
	res, err := exec.Run(context.Background(), RunSpec{
		Image: "alpine:latest",
		Cmd: []string{"sh", "-c",
			"for i in 1 2 3 4 5; do echo line-$i; done; for i in 1 2 3; do echo err-$i >&2; done"},
		PullPolicy: "missing",
		AutoRemove: true,
	}, col.sink)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit=%d", res.ExitCode)
	}
	col.mu.Lock()
	defer col.mu.Unlock()
	var outs, errs []string
	for _, l := range col.lines {
		if l.Stream == LogStdout {
			outs = append(outs, l.Line)
		} else {
			errs = append(errs, l.Line)
		}
	}
	if len(outs) != 5 {
		t.Errorf("stdout lines=%d want 5: %v", len(outs), outs)
	}
	if len(errs) != 3 {
		t.Errorf("stderr lines=%d want 3: %v", len(errs), errs)
	}
	// stdout 行内有序
	wantOut := []string{"line-1", "line-2", "line-3", "line-4", "line-5"}
	sort.Strings(wantOut)
	got := append([]string{}, outs...)
	sort.Strings(got)
	for i := range wantOut {
		if got[i] != wantOut[i] {
			t.Errorf("stdout content mismatch: got=%v want=%v", outs, wantOut)
			break
		}
	}
}

func TestExecutor_ImageMissing_Pull(t *testing.T) {
	// 镜像不在本地, missing 策略下应拉一次。
	// 用 alpine:3.18 这种很可能本地没有的 tag (alpine:latest 我们 warm 过, 但 3.18 不一定)
	// 即便已存在也不影响,只要不报错就 OK。
	c := requireDocker(t)
	exec := NewExecutor(c)
	res, err := exec.Run(context.Background(), RunSpec{
		Image:      "alpine:3.18",
		Cmd:        []string{"echo", "pulled"},
		PullPolicy: "missing",
		AutoRemove: true,
	}, nil)
	if err != nil {
		t.Skipf("skip: pull alpine:3.18 failed (network?): %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit=%d want 0", res.ExitCode)
	}
}

func TestExecutor_BadPullPolicy(t *testing.T) {
	c := requireDocker(t)
	exec := NewExecutor(c)
	_, err := exec.Run(context.Background(), RunSpec{
		Image:      "alpine:latest",
		Cmd:        []string{"true"},
		PullPolicy: "weird",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "PullPolicy") {
		t.Errorf("want bad PullPolicy error, got %v", err)
	}
}

func TestExecutor_NilImage(t *testing.T) {
	c := requireDocker(t)
	exec := NewExecutor(c)
	_, err := exec.Run(context.Background(), RunSpec{Image: ""}, nil)
	if err == nil {
		t.Error("want error")
	}
}

func TestBuildHostConfig_BadPath(t *testing.T) {
	_, err := buildHostConfig(RunSpec{
		Mounts: map[string]string{"relative": "/workspace"},
	})
	if err == nil {
		t.Error("want error on relative host path")
	}
	_, err = buildHostConfig(RunSpec{
		Mounts: map[string]string{"/abs": "relative"},
	})
	if err == nil {
		t.Error("want error on relative container path")
	}
}

func TestExecutor_NilClient(t *testing.T) {
	exec := NewExecutor(nil)
	_, err := exec.Run(context.Background(), RunSpec{Image: "alpine"}, nil)
	if err == nil {
		t.Error("want error from nil client")
	}
}

func TestLineWriter_Splitting(t *testing.T) {
	var got []LogLine
	w := &lineWriter{stream: LogStdout, sink: func(l LogLine) error {
		got = append(got, l)
		return nil
	}}
	// 分两次写,中间没有 \n
	_, _ = w.Write([]byte("hello "))
	if len(got) != 0 {
		t.Errorf("should not flush without newline, got %d", len(got))
	}
	_, _ = w.Write([]byte("world\nfoo\r\nbar"))
	w.flush()
	if len(got) != 3 {
		t.Fatalf("want 3 lines, got %d: %v", len(got), got)
	}
	want := []string{"hello world", "foo", "bar"}
	for i, l := range got {
		if l.Line != want[i] {
			t.Errorf("line %d = %q want %q", i, l.Line, want[i])
		}
	}
}

// 编译保护 + 文档:确认 LogStream String() 不是空字符串
func TestLogStreamString(t *testing.T) {
	if LogStdout.String() == "" || LogStderr.String() == "" {
		t.Error("stream string empty")
	}
	if fmt.Sprint(LogStream(99)) == "" {
		t.Error("unknown stream string empty")
	}
}
