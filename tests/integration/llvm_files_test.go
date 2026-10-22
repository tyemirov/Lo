package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLLVMCheckpointFileCancellation(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			directory := t.TempDir()
			pipe := filepath.Join(directory, "pipe")
			if err := syscall.Mkfifo(pipe, uint32(os.ModePerm)); err != nil {
				t.Fatalf("create local pipe: %v", err)
			}
			peer, err := os.OpenFile(pipe, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			later := filepath.Join(directory, "later")
			var expression string
			if operation == "read" {
				expression = fmt.Sprintf("data=File.read(File.Path.new(%q));", pipe)
			} else {
				input := filepath.Join(directory, "input")
				writeLLVMFixture(t, input, bytes.Repeat([]byte("x"), 1<<20))
				expression = fmt.Sprintf("data=File.read(File.Path.new(%q)); pipeTarget=File.Write.new(File.Path.new(%q),data); commit pipeTarget;", input, pipe)
			}
			source := sourceFile(t, fmt.Sprintf(`flow Handle(e:Error)->Int{return 99}
flow Run()->Int onError Handle{%s target=File.Write.new(File.Path.new(%q),Bytes.new("wrong")); commit target; return 0}
return Run()`, expression, later))
			artifact, built := buildForTest(t, source)
			if built.status != 0 {
				t.Fatalf("build file cancellation fixture: %+v", built)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, artifact, "run", "--format", "json", "--read-root", directory, "--write-root", directory)
			command.Dir = directory
			command.Env = os.Environ()
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			finished := false
			defer func() {
				if !finished {
					cancel()
					_ = peer.Close()
					<-done
				}
			}()
			started := make(chan error, 1)
			go func() {
				if operation == "read" {
					// A full megabyte exceeds the pipe buffer and proves active reads.
					_, err := peer.Write(bytes.Repeat([]byte("x"), 1<<20))
					started <- err
				} else {
					var first [1]byte
					_, err := peer.Read(first[:])
					started <- err
				}
			}()
			select {
			case err := <-started:
				if err != nil {
					t.Fatal(err)
				}
			case err := <-done:
				finished = true
				t.Fatalf("file operation exited before cancellation: %v: %s %s", err, stdout.String(), stderr.String())
			case <-ctx.Done():
				t.Fatal("file operation did not start")
			}
			if err := command.Process.Signal(os.Interrupt); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
				finished = true
			case <-time.After(2 * time.Second):
				t.Fatal("file operation did not stop after cancellation")
			}
			if command.ProcessState.ExitCode() != 2 || stdout.Len() != 0 ||
				!strings.Contains(stderr.String(), `"code":"runtime.cancelled"`) {
				t.Fatalf("file cancellation entered handler or lost host failure: %d: %s %s",
					command.ProcessState.ExitCode(), stdout.String(), stderr.String())
			}
			if _, err := os.Stat(later); !os.IsNotExist(err) {
				t.Fatalf("commit after cancellation: %v", err)
			}
		})
	}
}

func TestLLVMCheckpointFileDiagnostics(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	directory := t.TempDir()
	child := filepath.Join(directory, "child")
	if err := os.MkdirAll(child, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	parentLink := filepath.Join(child, "parent")
	if err := os.Symlink("..", parentLink); err != nil {
		t.Fatal(err)
	}
	for index, source := range []string{
		fmt.Sprintf(`return File.read(File.Path.new(%q))`, filepath.Join(directory, "missing")),
		fmt.Sprintf(`return File.read(File.Path.new(%q))`, directory),
		fmt.Sprintf(`target=File.Write.new(File.Path.new(%q),Bytes.new("x")); commit target`, directory),
		fmt.Sprintf(`target=File.Write.new(File.Path.new(%q),Bytes.new("x")); commit target`, parentLink),
	} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			input := sourceFile(t, source)
			artifact, built := buildForTest(t, input)
			if built.status != 0 {
				t.Fatalf("build file diagnostic fixture: %+v", built)
			}
			arguments := []string{"--read-root", directory, "--write-root", directory, "--format", "json"}
			expected := runExecutable(t, binary, append([]string{"run", input}, arguments...)...)
			actual := runExecutable(t, artifact, append([]string{"run"}, arguments...)...)
			if actual != expected {
				t.Fatalf("file diagnostics differ: native %+v evaluator %+v", actual, expected)
			}
		})
	}
}

func TestLLVMCheckpointFileCleanup(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmFileCleanupScenarios())
}

func TestLLVMLinuxCheckpointFileCleanup(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmFileCleanupScenarios())
}

func llvmFileCleanupScenarios() []llvmRuntimeScenario {
	scenarios := []llvmRuntimeScenario{
		{name: "recoverable", source: `return Math.div(1,0)`, number: 0},
		{name: "success", source: `return 1`, number: 1},
		{name: "host-limit", source: `return Math.add(1,2)`, number: 2},
	}
	for index := range scenarios {
		scenarios[index].harness = "testdata/native-runtime/file-cleanup.c"
	}
	return scenarios
}

func TestLLVMLinuxCheckpointFileCancellation(t *testing.T) {
	const tail = `target=File.Write.new(File.Path.new("later"),Bytes.new("wrong")); commit target; return 0}; return Run()`
	const head = `flow Handle(e:Error)->Int{return 99}; flow Run()->Int onError Handle{`
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", []llvmRuntimeScenario{
		{name: "read", source: head + `data=File.read(File.Path.new("pipe"));` + tail, number: 0, harness: "testdata/native-runtime/file-cancellation-linux.c"},
		{name: "write", source: head + `data=File.read(File.Path.new("input")); pipeTarget=File.Write.new(File.Path.new("pipe"),data); commit pipeTarget;` + tail, number: 1, harness: "testdata/native-runtime/file-cancellation-linux.c"},
	})
}

func TestLLVMFileReaderDisconnect(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmFileReaderDisconnectScenarios())
}

func TestLLVMLinuxFileReaderDisconnect(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmFileReaderDisconnectScenarios())
}

func llvmFileReaderDisconnectScenarios() []llvmRuntimeScenario {
	const write = `data=File.read(File.Path.new("input")); target=File.Write.new(File.Path.new("pipe"),data); commit target; return "unexpected"`
	return []llvmRuntimeScenario{
		{name: "unhandled", source: `flow Run()->String!{` + write + `}; return Run()`, number: 0, harness: "testdata/native-runtime/file-sigpipe.c"},
		{name: "handled", source: `flow Handle(e:Error)->String{return e.kind}; flow Run()->String onError Handle{` + write + `}; return Run()`, number: 1, harness: "testdata/native-runtime/file-sigpipe.c"},
	}
}
