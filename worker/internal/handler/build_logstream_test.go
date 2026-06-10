package handler

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/helios-cicd/helios/api/pkg/logstream"
	"github.com/helios-cicd/helios/api/pkg/projectrepo"
	"github.com/helios-cicd/helios/api/pkg/runstate"
)

// T1.5.1: host runtime + WithLogStream 双写到 Redis Stream.
// 用项目级 redis (HELIOS_REDIS_ADDR=127.0.0.1:6380), 没 redis 自动 skip.
func TestBuild_HostRuntime_WritesLogStream(t *testing.T) {
	db := requireDB(t)
	defer db.Close()

	addr := os.Getenv("HELIOS_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6380"
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis %s unreachable: %v", addr, err)
	}

	pid, clProj := seedProjectWithConfig(t, db, map[string]any{
		"build_command": "echo line-stdout && echo line-stderr 1>&2 && echo done",
	})
	defer clProj()
	runID, clRun := seedRunForProject(t, db, pid, runstate.StatusRunning)
	defer clRun()
	defer func() { _ = rdb.Del(context.Background(), logstream.StreamKey(runID)).Err() }()

	tmpWs := t.TempDir()
	ensureWorkspace(t, tmpWs, runID)

	w := logstream.NewWriter(rdb, logstream.Config{MaxLen: 1000})
	h := NewBuild(projectrepo.New(db), runstate.New(db), tmpWs, 30*time.Second,
		WithLogStream(w))
	if err := h.ProcessTask(context.Background(), newBuildTask(t, runID, pid)); err != nil {
		t.Fatalf("process: %v", err)
	}

	// 校验 run 成功
	var st string
	_ = db.QueryRow("SELECT status FROM runs WHERE id=$1", runID).Scan(&st)
	if st != runstate.StatusSuccess {
		t.Fatalf("status=%s want success", st)
	}

	// 校验 redis stream: 至少含 system 行 (build runtime=host) + line-stdout/stderr + done + [SUCCESS]
	r := logstream.NewReader(rdb)
	entries, err := r.ReadRange(context.Background(), runID, "", 100)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if len(entries) < 4 {
		t.Fatalf("entries=%d want >=4: %+v", len(entries), entries)
	}

	// 分桶检查
	var sawSystemStart, sawStdoutLine, sawStderrLine, sawSuccess bool
	for _, e := range entries {
		switch {
		case e.Stream == logstream.StreamSystem && strings.Contains(e.Line, "build runtime=host"):
			sawSystemStart = true
		case e.Stream == logstream.StreamStdout && e.Line == "line-stdout":
			sawStdoutLine = true
		case e.Stream == logstream.StreamStderr && e.Line == "line-stderr":
			sawStderrLine = true
		case e.Stream == logstream.StreamSystem && strings.Contains(e.Line, "[SUCCESS]"):
			sawSuccess = true
		}
	}
	if !sawSystemStart {
		t.Errorf("missing system start line")
	}
	if !sawStdoutLine {
		t.Errorf("missing stdout 'line-stdout'")
	}
	if !sawStderrLine {
		t.Errorf("missing stderr 'line-stderr'")
	}
	if !sawSuccess {
		t.Errorf("missing [SUCCESS] system line")
	}
}
