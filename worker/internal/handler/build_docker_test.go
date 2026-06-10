package handler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/helios-cicd/helios/api/pkg/projectrepo"
	"github.com/helios-cicd/helios/api/pkg/runstate"
	"github.com/helios-cicd/helios/api/pkg/tasks"
	"github.com/helios-cicd/helios/worker/internal/dockerrun"
)

// requireDockerForBuild: 没 docker 就 skip。
func requireDockerForBuild(t *testing.T) *dockerrun.Client {
	t.Helper()
	if os.Getenv("DOCKER_HOST") == "" {
		if _, err := os.Stat("/var/run/docker.sock"); err != nil {
			t.Skip("no docker, skip docker-runtime build test")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := dockerrun.New(ctx, dockerrun.ClientConfig{NegotiateOnce: true, RequestTO: 5 * time.Second})
	if err != nil {
		t.Skipf("docker ping fail, skip: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// 容器内跑 build_command 成功路径:
//   - workspace 挂载到 /workspace
//   - HELIOS_RUN_ID env 注入到容器
//   - 产物写回 host workspace (验证 bind mount 双向)
//   - logFile 收到 stdout
func TestBuild_Docker_Success(t *testing.T) {
	db := requireDB(t)
	dc := requireDockerForBuild(t)

	pid, projCleanup := seedProjectWithConfig(t, db, map[string]any{
		"build_command": "echo docker-build-from-$HELIOS_RUN_ID && echo done > /workspace/artifact.txt",
		"build_image":   "alpine:latest",
	})
	defer projCleanup()

	tmpWs := t.TempDir()
	rid, runCleanup := seedRunForProject(t, db, pid, "running")
	defer runCleanup()

	// 准备 workspace: 必须存在 (checkout 完后的状态)
	wsDir := filepath.Join(tmpWs, intToStr(rid), "src")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatalf("mkdir ws: %v", err)
	}

	h := NewBuild(projectrepo.New(db), runstate.New(db), tmpWs, 60*time.Second,
		WithDockerRuntime(dockerrun.NewExecutor(dc)))

	payload, _ := (&tasks.RunBuildPayload{RunID: rid, ProjectID: pid}).Marshal()
	if err := h.ProcessTask(context.Background(), asynq.NewTask(tasks.TypeRunBuild, payload)); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}

	// 状态 = success
	status, _ := runstate.New(db).Status(context.Background(), rid)
	if status != runstate.StatusSuccess {
		t.Fatalf("status=%s want success", status)
	}
	// 产物落到 host
	out, err := os.ReadFile(filepath.Join(wsDir, "artifact.txt"))
	if err != nil {
		t.Fatalf("read artifact.txt: %v", err)
	}
	if !strings.Contains(string(out), "done") {
		t.Errorf("artifact.txt=%q want 'done'", string(out))
	}
	// build.log 含 stdout + env 注入
	logBytes, _ := os.ReadFile(filepath.Join(tmpWs, intToStr(rid), "build.log"))
	if !strings.Contains(string(logBytes), "docker-build-from-"+intToStr(rid)) {
		t.Errorf("build.log missing env-injected stdout: %q", string(logBytes))
	}
}

// 容器内命令 exit != 0 → MarkFailed (不重试)
func TestBuild_Docker_Failure(t *testing.T) {
	db := requireDB(t)
	dc := requireDockerForBuild(t)

	pid, projCleanup := seedProjectWithConfig(t, db, map[string]any{
		"build_command": "echo will fail && exit 5",
		"build_image":   "alpine:latest",
	})
	defer projCleanup()

	tmpWs := t.TempDir()
	rid, runCleanup := seedRunForProject(t, db, pid, "running")
	defer runCleanup()

	wsDir := filepath.Join(tmpWs, intToStr(rid), "src")
	_ = os.MkdirAll(wsDir, 0o755)

	h := NewBuild(projectrepo.New(db), runstate.New(db), tmpWs, 60*time.Second,
		WithDockerRuntime(dockerrun.NewExecutor(dc)))

	payload, _ := (&tasks.RunBuildPayload{RunID: rid, ProjectID: pid}).Marshal()
	if err := h.ProcessTask(context.Background(), asynq.NewTask(tasks.TypeRunBuild, payload)); err != nil {
		t.Fatalf("ProcessTask should swallow user-command failure, got: %v", err)
	}

	status, _ := runstate.New(db).Status(context.Background(), rid)
	if status != runstate.StatusFailed {
		t.Fatalf("status=%s want failed", status)
	}
}

// 没配 build_image → 用 DefaultBuildImage (alpine:latest)
func TestBuild_Docker_DefaultImage(t *testing.T) {
	db := requireDB(t)
	dc := requireDockerForBuild(t)

	pid, projCleanup := seedProjectWithConfig(t, db, map[string]any{
		"build_command": "true",
		// 故意不写 build_image
	})
	defer projCleanup()

	tmpWs := t.TempDir()
	rid, runCleanup := seedRunForProject(t, db, pid, "running")
	defer runCleanup()
	_ = os.MkdirAll(filepath.Join(tmpWs, intToStr(rid), "src"), 0o755)

	h := NewBuild(projectrepo.New(db), runstate.New(db), tmpWs, 60*time.Second,
		WithDockerRuntime(dockerrun.NewExecutor(dc)))

	payload, _ := (&tasks.RunBuildPayload{RunID: rid, ProjectID: pid}).Marshal()
	if err := h.ProcessTask(context.Background(), asynq.NewTask(tasks.TypeRunBuild, payload)); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	status, _ := runstate.New(db).Status(context.Background(), rid)
	if status != runstate.StatusSuccess {
		t.Errorf("status=%s want success", status)
	}
}

// Docker runtime 但 executor 为 nil → 视为基础设施错: MarkFailed + return nil (不重试)
//
// 注: 因为构造时强制要传 executor (WithDockerRuntime 强制有效), 这种情况只可能是手工
// 把 runtime 改 docker 而没传 executor; 这里通过反向构造来覆盖。
func TestBuild_Docker_NilExecutor(t *testing.T) {
	db := requireDB(t)
	pid, projCleanup := seedProjectWithConfig(t, db, map[string]any{
		"build_command": "true",
		"build_image":   "alpine:latest",
	})
	defer projCleanup()

	tmpWs := t.TempDir()
	rid, runCleanup := seedRunForProject(t, db, pid, "running")
	defer runCleanup()
	_ = os.MkdirAll(filepath.Join(tmpWs, intToStr(rid), "src"), 0o755)

	h := NewBuild(projectrepo.New(db), runstate.New(db), tmpWs, 60*time.Second)
	h.runtime = BuildRuntimeDocker // 手工破坏

	payload, _ := (&tasks.RunBuildPayload{RunID: rid, ProjectID: pid}).Marshal()
	if err := h.ProcessTask(context.Background(), asynq.NewTask(tasks.TypeRunBuild, payload)); err != nil {
		t.Fatalf("should swallow infra error after MarkFailed: %v", err)
	}
	status, _ := runstate.New(db).Status(context.Background(), rid)
	if status != runstate.StatusFailed {
		t.Errorf("status=%s want failed", status)
	}
}

func intToStr(i int64) string {
	return strings.TrimSpace(formatInt(i))
}
func formatInt(i int64) string {
	// minimal, avoid strconv import duplication with main test file
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
