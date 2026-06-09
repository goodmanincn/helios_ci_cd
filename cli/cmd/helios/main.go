// Package main helios CLI 占位 — 后续 M8 接入 Cobra
package main

import (
	"fmt"
	"os"
)

var Version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("helios %s\n", Version)
		return
	}
	fmt.Println("helios CLI — placeholder. Full implementation lands in M8.")
	fmt.Println("Usage: helios [--version]")
}
