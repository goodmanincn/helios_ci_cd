// Package main worker 占位 — 后续 M1 接入 Asynq
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

var Version = "dev"

func main() {
	log.Printf("helios-worker starting (version=%s)", Version)
	log.Println("worker is a placeholder; will host Asynq handlers from M1")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("helios-worker stopping")
}
