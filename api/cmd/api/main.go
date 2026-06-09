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
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/helios-cicd/helios/api/internal/db"
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

	// 模式 1: 仅迁移
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

	// 模式 2: 启动 server (默认先跑迁移)
	if !*skipMigrate && dsn != "" {
		log.Println("auto-migrating database...")
		if err := db.Migrate(dsn); err != nil {
			log.Fatalf("startup migration failed: %v", err)
		}
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestLogger())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":     "ok",
			"service":    "helios-api",
			"version":    Version,
			"build_time": BuildTime,
			"git_commit": GitCommit,
			"timestamp":  time.Now().UTC().Format(time.RFC3339),
		})
	})

	r.GET("/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"version":    Version,
			"build_time": BuildTime,
			"git_commit": GitCommit,
		})
	})

	addr := os.Getenv("HELIOS_API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

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

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Printf("%s %s -> %d (%s)",
			c.Request.Method, c.Request.URL.Path,
			c.Writer.Status(), time.Since(start))
	}
}
