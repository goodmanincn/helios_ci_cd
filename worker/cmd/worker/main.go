// Package main helios-worker — 后台任务消费者 (Asynq)
package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/helios-cicd/helios/api/pkg/runrepo"
	"github.com/helios-cicd/helios/api/pkg/tasks"
	"github.com/helios-cicd/helios/worker/internal/gitrunner"
	"github.com/helios-cicd/helios/worker/internal/handler"
)

// Version 通过 ldflags 注入。
var Version = "dev"

func main() {
	log.Printf("helios-worker starting (version=%s)", Version)

	dsn := mustEnv("HELIOS_DB_DSN")
	redisAddr := mustEnv("HELIOS_REDIS_ADDR")
	workspaceDir := envOr("HELIOS_WORKSPACE_DIR", "/tmp/helios/runs")
	concurrency := 10

	// === DB ===
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("db ping: %v", err)
	}
	cancel()

	// === 处理器 ===
	repo := runrepo.New(db)
	checkoutH := handler.NewCheckout(repo, gitrunner.NewShell(), workspaceDir)

	// === Asynq server ===
	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr},
		asynq.Config{
			Concurrency: concurrency,
			Queues: map[string]int{
				tasks.QueueCritical: 6,
				tasks.QueueDefault:  3,
				tasks.QueueLow:      1,
			},
			RetryDelayFunc: func(n int, _ error, _ *asynq.Task) time.Duration {
				// 指数退避: 10s, 30s, 90s
				return time.Duration(10*pow3(n)) * time.Second
			},
			Logger: stdLogger{},
			// 重试耗尽 hook — 各 handler 自己实现 OnRetryExhausted, 通过任务类型分发
			IsFailure: func(err error) bool {
				return err != nil // 默认即可
			},
		},
	)

	mux := asynq.NewServeMux()
	// 用 wrapper 在 retry 用尽时调 handler 的 OnRetryExhausted
	mux.Handle(tasks.TypeGitCheckout, withExhaustHook(checkoutH, func(ctx context.Context, t *asynq.Task, err error) {
		checkoutH.OnRetryExhausted(ctx, t, err)
	}))

	// === 启动 + signal ===
	go func() {
		log.Printf("worker server starting: redis=%s concurrency=%d workspace=%s",
			redisAddr, concurrency, workspaceDir)
		if err := srv.Start(mux); err != nil {
			log.Fatalf("asynq server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutdown signal received, draining...")
	srv.Shutdown()
	log.Println("helios-worker stopped")
}

// withExhaustHook 包一层 asynq.Handler, 当 asynq 把任务标记 archived (重试用尽) 后
// 调用 hook。asynq 本身没有 "用尽" 钩子,我们用 GetTaskInfo 检查 retry_count 来判定。
func withExhaustHook(h asynq.Handler, hook func(context.Context, *asynq.Task, error)) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
		err := h.ProcessTask(ctx, t)
		if err == nil {
			return nil
		}
		// asynq 把 retry_count / max_retry 放在 context (ResultWriter).
		// 用 asynq.GetRetryCount / GetMaxRetry 取。
		retry, _ := asynq.GetRetryCount(ctx)
		maxRetry, _ := asynq.GetMaxRetry(ctx)
		if retry+1 >= maxRetry {
			// 这是最后一次重试且失败 → 触发 hook
			hook(ctx, t, err)
		}
		return err
	})
}

// stdLogger 把 asynq 日志走标准 log。
type stdLogger struct{}

func (stdLogger) Debug(args ...any) { log.Println(append([]any{"[asynq DEBUG]"}, args...)...) }
func (stdLogger) Info(args ...any)  { log.Println(append([]any{"[asynq INFO]"}, args...)...) }
func (stdLogger) Warn(args ...any)  { log.Println(append([]any{"[asynq WARN]"}, args...)...) }
func (stdLogger) Error(args ...any) { log.Println(append([]any{"[asynq ERROR]"}, args...)...) }
func (stdLogger) Fatal(args ...any) { log.Fatalln(append([]any{"[asynq FATAL]"}, args...)...) }

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("%s required", k)
	}
	return v
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func pow3(n int) int {
	if n <= 0 {
		return 1
	}
	r := 1
	for i := 0; i < n; i++ {
		r *= 3
	}
	return r
}
