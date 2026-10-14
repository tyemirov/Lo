// Command nativecache supplies an owned Go build cache for build qualification.
package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

func main() {
	tool, cache := os.Getenv("LO_BENCH_GO"), os.Getenv("LO_BENCH_CACHE")
	if tool == "" || cache == "" {
		fmt.Fprintln(os.Stderr, "LO_BENCH_GO and LO_BENCH_CACHE are required")
		os.Exit(2)
	}
	env := []string{}
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "GOCACHE=") {
			env = append(env, item)
		}
	}
	env = append(env, "GOCACHE="+cache)
	if err := syscall.Exec(tool, append([]string{tool}, os.Args[1:]...), env); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
