// Package main 启动 Helios API 服务器
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/authstore"
	"github.com/helios-cicd/helios/api/internal/db"
	"github.com/helios-cicd/helios/api/internal/handler"
	logsh "github.com/helios-cicd/helios/api/internal/handler/logs"
	webhookh "github.com/helios-cicd/helios/api/internal/handler/webhook"
	"github.com/helios-cicd/helios/api/internal/middleware"
	"github.com/helios-cicd/helios/api/internal/repository"
	"github.com/helios-cicd/helios/api/internal/service"
	"github.com/helios-cicd/helios/api/pkg/git"
	heliosjwt "github.com/helios-cicd/helios/api/pkg/jwt"
	"github.com/helios-cicd/helios/api/pkg/logarchive"
	"github.com/helios-cicd/helios/api/pkg/logstream"
	"github.com/helios-cicd/helios/api/pkg/queue"
	"github.com/helios-cicd/helios/api/pkg/runstate"
)

// Version 通过 ldflags 注入,默认 dev
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	var (
		migrateOnly = flag.Bool("migrate", false, "run DB migrations and exit")
		skipMigrate = flag.Bool("skip-migrate", false, "skip auto-migration on startup")
	)
	flag.Parse()

	dsn := os.Getenv("HELIOS_DB_DSN")

	if *migrateOnly {
		if dsn == "" {
			log.Fatal("--migrate requires HELIOS_DB_DSN env var")
		}
		log.Println("running database migrations...")
		if err := db.Migrate(dsn); err != nil {
			log.Fatalf("migrate failed: %v", err)
		}
		v, dirty, err := db.Version(dsn)
		if err != nil {
			log.Fatalf("version check failed: %v", err)
		}
		log.Printf("migrations applied: schema version=%d dirty=%v", v, dirty)
		return
	}

	if !*skipMigrate && dsn != "" {
		log.Println("auto-migrating database...")
		if err := db.Migrate(dsn); err != nil {
			log.Fatalf("startup migration failed: %v", err)
		}
	}

	// ===== 依赖装配 =====
	gdb := mustOpenGorm(dsn)
	rdb := mustOpenRedis()
	issuer := mustNewIssuer()
	userSvc := service.NewUserService(gdb)
	projectSvc := service.NewProjectService(repository.NewProjectRepository(gdb))
	store := authstore.New(rdb)
	authH := handler.NewAuthHandler(userSvc, issuer, store, gdb)
	enq := queue.New(os.Getenv("HELIOS_REDIS_ADDR"))
	defer func() { _ = enq.Close() }()

	// runstate.Machine 需要原生 *sql.DB (走 SELECT ... FOR UPDATE 事务)
	sqlDB, dbErr := gdb.DB()
	if dbErr != nil {
		log.Fatalf("get *sql.DB from gorm: %v", dbErr)
	}
	runMachine := runstate.New(sqlDB)
	projectH := handler.NewProjectHandlerWithQueue(projectSvc, enq)
	gh := git.NewGitHubProvider(git.GitHubConfig{
		Token: os.Getenv("HELIOS_GITHUB_TOKEN"), // 可空,目前 webhook 接收不需要 token
	})
	webhookH := webhookh.NewGitHubHandler(webhookh.NewGormRunStore(gdb), gh, enq, os.Getenv("HELIOS_WEBHOOK_DEV_SECRET"))

	// T1.5.3/4: 日志归档 fallback. backend 与 worker 共享路径.
	archiveRoot := envOr("HELIOS_LOG_ARCHIVE_DIR", "/tmp/helios/logs")
	archiveSvc := &logarchive.Service{
		Reader:  logstream.NewReader(rdb),
		Writer:  logstream.NewWriter(rdb, logstream.Config{}),
		Backend: logarchive.NewLocalFS(archiveRoot),
	}
	logsH := logsh.New(logstream.NewReader(rdb), issuer, logsh.WithArchive(archiveSvc))
	log.Printf("api logarchive backend=localfs root=%s", archiveRoot)
	authMW := middleware.RequireAuth(middleware.AuthDeps{Issuer: issuer, Authstore: store})

	// ===== 路由 =====
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestLogger())
	r.Use(corsMiddleware())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok", "service": "helios-api",
			"version": Version, "build_time": BuildTime, "git_commit": GitCommit,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})
	r.GET("/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"version": Version, "build_time": BuildTime, "git_commit": GitCommit})
	})

	v1 := r.Group("/api/v1")
	authH.Register(v1.Group("/auth"), authMW)

	// Webhook 端点 — 公开 (走 HMAC 签名校验,不能加 auth 中间件)
	webhookH.Register(v1)

	// 日志端点 — 公开 (内部 ?token= 自校, M1 dev 允许匿名)
	logsH.Register(v1)

	// 项目资源 — 全部需要登录
	protected := v1.Group("")
	protected.Use(authMW)
	projectH.Register(protected)
	handler.NewRunHandler(gdb).WithRunControl(runMachine, enq).Register(protected)
	handler.NewMeHandler(gdb).Register(protected)

	addr := os.Getenv("HELIOS_API_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		log.Printf("helios-api listening on %s (version=%s)", addr, Version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutdown signal received")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("forced shutdown: %v", err)
	}
	log.Println("server stopped cleanly")
}

// ===== 依赖工厂 =====

func mustOpenGorm(dsn string) *gorm.DB {
	if dsn == "" {
		log.Fatal("HELIOS_DB_DSN required")
	}
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("gorm open: %v", err)
	}
	return gdb
}

func mustOpenRedis() *redis.Client {
	addr := os.Getenv("HELIOS_REDIS_ADDR")
	if addr == "" {
		log.Fatal("HELIOS_REDIS_ADDR required")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("redis ping: %v", err)
	}
	return rdb
}

func mustNewIssuer() *heliosjwt.Issuer {
	privPath := envOr("HELIOS_JWT_PRIVATE_KEY_PATH", "./.helios/jwt-private.pem")
	pubPath := envOr("HELIOS_JWT_PUBLIC_KEY_PATH", "./.helios/jwt-public.pem")
	priv, err := heliosjwt.LoadPrivateKeyPEM(privPath)
	if err != nil {
		log.Fatalf("load jwt private key (%s): %v", privPath, err)
	}
	pub, err := heliosjwt.LoadPublicKeyPEM(pubPath)
	if err != nil {
		log.Fatalf("load jwt public key (%s): %v", pubPath, err)
	}
	iss, err := heliosjwt.NewIssuer(heliosjwt.Config{
		PrivateKey: priv, PublicKey: pub,
		Issuer:     envOr("HELIOS_JWT_ISSUER", "helios-dev"),
		AccessTTL:  parseDur("HELIOS_JWT_ACCESS_TTL", 30*time.Minute),
		RefreshTTL: parseDur("HELIOS_JWT_REFRESH_TTL", 7*24*time.Hour),
	})
	if err != nil {
		log.Fatalf("new issuer: %v", err)
	}
	return iss
}

// ===== 工具 =====

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func parseDur(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("warn: parse %s=%q: %v (using default %s)", k, v, err, def)
		return def
	}
	return d
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Printf("%s %s -> %d (%s)",
			c.Request.Method, c.Request.URL.Path, c.Writer.Status(), time.Since(start))
	}
}

// corsMiddleware 简单 CORS,允许 NEXT_PUBLIC_API_BASE 的 origin (dev 用)。
func corsMiddleware() gin.HandlerFunc {
	allow := os.Getenv("HELIOS_CORS_ALLOW_ORIGIN")
	if allow == "" {
		allow = "http://localhost:3000,http://localhost:3001"
	}
	allowList := strings.Split(allow, ",")
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		for _, a := range allowList {
			if strings.TrimSpace(a) == origin {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
				c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type")
				c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
				break
			}
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
