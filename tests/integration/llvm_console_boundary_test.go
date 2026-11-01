package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	encodingbinary "encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLLVMConsoleEmbeddingBoundary(t *testing.T) {
	names := []string{"partial-progress", "partial-write-failure", "flush-failure", "cooperative-cancel", "logical-quota", "disabled-stream", "invalid-callback-progress", "terminal-zero", "terminal-one", "terminal-two", "terminal-flush-failure", "terminal-cancel", "terminal-reservation", "terminal-disabled-stream", "empty-root-registration-gate", "terminal-zero-flush-failure", "terminal-one-flush-failure"}
	var scenarios []llvmRuntimeScenario
	for number, name := range names {
		source := cliStackProgram("Int", `line=Console.Line.new("stdout","é",true);receipt=commit line;return receipt.bytes_written`)
		if number >= 7 {
			code := 0
			if number == 8 {
				code = 1
			}
			if number >= 9 {
				code = 2
			}
			if number == 15 {
				code = 0
			}
			if number == 16 {
				code = 1
			}
			source = cliStackProgram("Program.Exit", fmt.Sprintf(`return Program.Exit.new(%d,Bytes.new("out"),Bytes.new("err"))`, code))
		}
		if number == 14 {
			source = ""
		}
		scenarios = append(scenarios, llvmRuntimeScenario{name: name, source: source, number: number, harness: "testdata/native-runtime/console-boundary.c"})
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}

func TestLLVMConsoleCompletionCleanupPrecedence(t *testing.T) {
	var scenarios []llvmRuntimeScenario
	for code := 0; code <= 2; code++ {
		source := cliStackProgram("Program.Exit", fmt.Sprintf(`tensor=Tensor.to(Tensor.new(List.new(1.0),List.new(1)),Device.new("metal:0"));return Program.Exit.new(%d,Bytes.new("out"),Bytes.new("err"))`, code))
		scenarios = append(scenarios, llvmRuntimeScenario{name: fmt.Sprintf("status-%d", code), source: source, number: code, harness: "testdata/native-runtime/console-cleanup.c"})
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}

func TestLLVMConsoleCompletionFlushCancellation(t *testing.T) {
	var scenarios []llvmRuntimeScenario
	for code := 0; code <= 2; code++ {
		for stream, stderr := range []string{"", "err"} {
			name := "stdout"
			if stream == 1 {
				name = "stderr"
			}
			source := cliStackProgram("Program.Exit", fmt.Sprintf(`return Program.Exit.new(%d,Bytes.new("out"),Bytes.new(%q))`, code, stderr))
			scenarios = append(scenarios, llvmRuntimeScenario{
				name:    fmt.Sprintf("status-%d/%s", code, name),
				source:  source,
				number:  17 + 2*code + stream,
				harness: "testdata/native-runtime/console-boundary.c",
			})
		}
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}

func TestLLVMConsoleWriterParentLiveness(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires LLVM runtime inputs")
	}
	artifact, built := buildForTest(t, sourceFile(t, "return 0"))
	if built.status != 0 {
		t.Fatalf("build writer fixture: %+v", built)
	}
	outputRead, outputWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer outputRead.Close()
	defer outputWrite.Close()
	ackRead, ackWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer ackRead.Close()
	defer ackWrite.Close()
	liveRead, liveWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer liveRead.Close()
	defer liveWrite.Close()
	body := bytes.Repeat([]byte("x"), 1<<20)
	frame := append([]byte("LCW1"), 1, 0, 0, 0, 0)
	encodingbinary.BigEndian.PutUint32(frame[5:9], uint32(len(body)))
	frame = append(frame, body...)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, artifact, "--lo-console-writer")
	command.Stdin = bytes.NewReader(frame)
	command.Stdout = outputWrite
	command.Stderr = io.Discard
	command.ExtraFiles = []*os.File{ackWrite, liveRead}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	ackWrite.Close()
	liveRead.Close()
	outputWrite.Close()
	joined := make(chan error, 1)
	go func() { joined <- command.Wait() }()
	select {
	case err := <-joined:
		t.Fatalf("writer exited before parent liveness ended: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := liveWrite.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-joined:
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 125 {
			t.Fatalf("writer liveness exit: %v", err)
		}
	case <-time.After(2 * time.Second):
		cancel()
		<-joined
		t.Fatal("blocked writer survived parent liveness EOF")
	}
}

func TestLLVMConsoleWriterImageProtection(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires LLVM runtime inputs")
	}
	for _, operation := range []string{"Write", "Append"} {
		t.Run(operation, func(t *testing.T) {
			source := `flow Last(state:String,item:String)->String{return item};` + cliStackProgram("Int", fmt.Sprintf(`invocation=Host.Invocation.current();name=List.fold(invocation.arguments,"",Last);target=File.%s.new(File.Path.new(name),Bytes.new("replace"));receipt=commit target;return receipt.bytes_written`, operation))
			artifact, built := buildForTest(t, sourceFile(t, source))
			if built.status != 0 {
				t.Fatalf("build image mutation fixture: %+v", built)
			}
			before, err := os.ReadFile(artifact)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(before)
			aliases := t.TempDir()
			hardlink, symlink := filepath.Join(aliases, "hardlink"), filepath.Join(aliases, "symlink")
			if err := os.Link(artifact, hardlink); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(artifact, symlink); err != nil {
				t.Fatal(err)
			}
			for _, target := range []string{artifact, hardlink, symlink} {
				got := runExecutable(t, artifact, "run", "--write-root", filepath.Dir(target), "--", target)
				if got.status == 0 || got.stdout != "" {
					t.Fatalf("writer image mutation accepted for %s: %+v", target, got)
				}
				after, err := os.ReadFile(artifact)
				if err != nil || sha256.Sum256(after) != digest {
					t.Fatalf("writer image changed for %s: %v", target, err)
				}
			}
		})
	}
	t.Run("Remove", func(t *testing.T) {
		source := `flow Last(state:String,item:String)->String{return item};` + cliStackProgram("Int", `invocation=Host.Invocation.current();root=List.fold(invocation.arguments,"",Last);entries=File.list(File.Path.new(root));target=File.Remove.new(entries);commit target;return 0`)
		artifact, built := buildForTest(t, sourceFile(t, source))
		if built.status != 0 {
			t.Fatalf("build image removal fixture: %+v", built)
		}
		before, err := os.ReadFile(artifact)
		if err != nil {
			t.Fatal(err)
		}
		root := filepath.Dir(artifact)
		got := runExecutable(t, artifact, "run", "--list-root", root, "--remove-root", root, "--", root)
		if got.status != 2 || got.stdout != "" || !strings.Contains(got.stderr, "protected") {
			t.Fatalf("writer image removal: %+v", got)
		}
		after, err := os.ReadFile(artifact)
		if err != nil || sha256.Sum256(after) != sha256.Sum256(before) {
			t.Fatalf("writer image changed: %v", err)
		}
	})
}
