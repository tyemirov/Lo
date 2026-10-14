// This executable controls only the external toolchain boundary in CLI tests.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func record(path, text string) {
	file, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	if _, err = file.WriteString(text); err != nil {
		panic(err)
	}
	if err = file.Close(); err != nil {
		panic(err)
	}
}
func main() {
	state := os.Getenv("LO_TEST_TOOL_STATE")
	switch os.Args[1] {
	case "version":
		if os.Getenv("LO_TEST_TOOL_ACTION") == "version-overflow" {
			cwd, err := os.Getwd()
			if err != nil { panic(err) }
			record(filepath.Join(state,"work"),cwd)
			fmt.Print(strings.Repeat("x", 2<<20))
			return
		}
		fmt.Println("go version go1.27.1 darwin/arm64")
	case "child":
		time.Sleep(700 * time.Millisecond)
		record(filepath.Join(state, "child-survived"), "unexpected child completion")
	case "build":
		cwd, err := os.Getwd()
		if err != nil {
			panic(err)
		}
		record(filepath.Join(state, "work"), cwd)
		if os.Getenv("LO_TEST_TOOL_ACTION") == "overflow" {
			fmt.Fprint(os.Stderr, strings.Repeat("x", 2<<20))
			time.Sleep(10 * time.Second)
			return
		}
		child := exec.Command(os.Args[0], "child")
		child.Env = os.Environ()
		if err := child.Start(); err != nil {
			panic(err)
		}
		record(filepath.Join(state, "ready"), "ready")
		time.Sleep(10 * time.Second)
	}
}
