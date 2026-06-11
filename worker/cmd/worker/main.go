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
	"github.com/redis/go-redis/v9"

	"github.com/helios-cicd/helios/api/pkg/approval"
	"github.com/helios-cicd/helios/api/pkg/git"
	"github.com/helios-cicd/helios/api/pkg/logarchive"
	"github.com/helios-cicd/helios/api/pkg/logstream"
	"github.com/helios-cicd/helios/api/pkg/projectrepo"
	"github.com/helios-cicd/helios/api/pkg/queue"
	"github.com/helios-cicd/helios/api/pkg/runrepo"
	"github.com/helios-cicd/helios/api/pkg/runstate"
	"github.com/helios-cicd/helios/api/pkg/tasks"
	"github.com/helios-cicd/helios/worker/internal/clusterhealth"
	"github.com/helios-cicd/helios/worker/internal/dockerrun"
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
	enq := queue.New(redisAddr)
	defer func() { _ = enq.Close() }()
	checkoutH := handler.NewCheckout(repo, gitrunner.NewShell(), workspaceDir, enq)

	machine := runstate.New(db)
	buildTimeout := 5 * time.Minute
	if v := os.Getenv("HELIOS_BUILD_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			buildTimeout = d
		}
	}
	projRepo := projectrepo.New(db)

	// T1.5.1: 日志流写入器 (Redis Stream, 与 asynq 共用 redis).
	logsRedis := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer func() { _ = logsRedis.Close() }()
	logsWriter := logstream.NewWriter(logsRedis, logstream.Config{MaxLen: 10000})
	log.Printf("worker logstream redis=%s maxlen=10000", redisAddr)

	// T1.5.3: 日志归档服务 (本地文件, M2 加 MinIO).
	archiveRoot := envOr("HELIOS_LOG_ARCHIVE_DIR", "/tmp/helios/logs")
	if err := os.MkdirAll(archiveRoot, 0o755); err != nil {
		log.Fatalf("mkdir archive root %s: %v", archiveRoot, err)
	}
	archiveSvc := &logarchive.Service{
		Reader:  logstream.NewReader(logsRedis),
		Writer:  logsWriter,
		Backend: logarchive.NewLocalFS(archiveRoot),
	}
	log.Printf("worker logarchive backend=localfs root=%s", archiveRoot)

	// runtime 选择: HELIOS_BUILD_RUNTIME = host (默认) | docker
	buildOpts := []handler.BuildOption{
		handler.WithLogStream(logsWriter),
		handler.WithLogArchive(archiveSvc),
	}
	runtime := envOr("HELIOS_BUILD_RUNTIME", "host")
	switch runtime {
	case "docker":
		dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
		dc, derr := dockerrun.New(dctx, dockerrun.ClientConfig{
			Host:          os.Getenv("DOCKER_HOST"),
			NegotiateOnce: true,
			RequestTO:     30 * time.Second,
		})
		dcancel()
		if derr != nil {
			log.Fatalf("HELIOS_BUILD_RUNTIME=docker but docker unreachable: %v", derr)
		}
		log.Printf("worker build runtime=docker (host=%s)", dc.Host())
		buildOpts = append(buildOpts, handler.WithDockerRuntime(dockerrun.NewExecutor(dc)))
		defer func() { _ = dc.Close() }()
	case "host":
		log.Printf("worker build runtime=host (set HELIOS_BUILD_RUNTIME=docker to use containers)")
	default:
		log.Fatalf("HELIOS_BUILD_RUNTIME=%q invalid, want host|docker", runtime)
	}

	buildH := handler.NewBuild(projRepo, machine, workspaceDir, buildTimeout, buildOpts...)
	ghProvider := git.NewGitHubProvider(git.GitHubConfig{
		Token: os.Getenv("HELIOS_GITHUB_TOKEN"),
	})
	webhookRegH := handler.NewWebhookRegister(
		projRepo,
		ghProvider,
		os.Getenv("HELIOS_PUBLIC_API_BASE"),
		os.Getenv("HELIOS_WEBHOOK_DEV_SECRET"),
	)

	// T2.6.3: 审批超时 handler (asynq critical 队列).
	approvalTimeoutH := handler.NewApprovalTimeout(approval.NewTimeouter(db, machine))

	// T4.1.4: 集群健康检查定时任务.
	clusterhealth.Start(db)

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
	mux.Handle(tasks.TypeWebhookRegister, withExhaustHook(webhookRegH, func(ctx context.Context, t *asynq.Task, err error) {
		webhookRegH.OnRetryExhausted(ctx, t, err)
	}))
	// build handler (T1.3.2): build_command 失败由 handler 自己 mark failed,
	// retry 用尽的兜底就是 mark failed (但绝大多数 user error 已 SkipRetry)。
	mux.Handle(tasks.TypeRunBuild, withExhaustHook(buildH, func(ctx context.Context, t *asynq.Task, err error) {
		if p, perr := tasks.UnmarshalRunBuild(t.Payload()); perr == nil {
			_ = machine.MarkFailed(ctx, p.RunID, "build retry exhausted: "+err.Error(), runstate.TransitionOpts{ProjectID: &p.ProjectID})
		}
	}))

	// 审批超时 handler (T2.6.3): MaxRetry=0, 不接 exhaust hook (handler 自身幂等).
	mux.Handle(tasks.TypeApprovalTimeout, approvalTimeoutH)

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
