// Command metalbuild builds the compiler frontend with verified precompiled kernels.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 1 {
		return fmt.Errorf("build Metal compiler: unexpected arguments")
	}
	for _, args := range [][]string{
		{"python3", "-B", "tools/nativeruntime/metal_assets.py", "verify"},
		{filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-trimpath", "-o", "bin/lo-metal", "./cmd/lo"},
	} {
		command := exec.Command(args[0], args[1:]...)
		command.Env = append(os.Environ(), "CGO_ENABLED=0")
		command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("build Metal compiler %s: %w", args[0], err)
		}
	}
	return nil
}
