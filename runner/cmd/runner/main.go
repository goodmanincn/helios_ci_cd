// Package main runner 占位 — 后续 M1 接入 Docker runner
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

var Version = "dev"

func main() {
	log.Printf("helios-runner starting (version=%s)", Version)
	log.Println("runner is a placeholder; will host Docker/SSH executors from M1")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("helios-runner stopping")
}
