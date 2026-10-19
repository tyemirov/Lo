package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func processCompositionProgram(t *testing.T, root, code string) execution.Program {
	t.Helper()
	text, err := source.New(filepath.Join(root, "composition.lo"), code)
	if err != nil {
		t.Fatal(err)
	}
	checked, diagnostic := compiler.Check(text)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	program, err := eval.Program(checked)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func TestAdapterProcessFileComposition(t *testing.T) {
	for _, code := range []int64{0, 7} {
		t.Run(fmt.Sprintf("exit-%d", code), func(t *testing.T) {
			root := fileFixtureRoot(t)
			path := filepath.Join(root, "captured.bin")
			files, err := adapter.OpenFiles(adapter.FileRoots{Write: []string{root}}, adapter.NativeFileIO())
			if err != nil {
				t.Fatal(err)
			}
			defer files.Close()
			termination, err := value.NewProcessExited(value.Int64(code))
			if err != nil {
				t.Fatal(err)
			}
			payload := []byte{0, 255, 'a'}
			child := &lifecycleChild{input: &lifecycleInputPipe{}, output: io.NopCloser(bytes.NewReader(payload)), errors: io.NopCloser(bytes.NewReader(nil)), wait: func(context.Context) (value.ProcessTermination, error) { return termination, nil }, stop: func(context.Context) error { return nil }, release: func(context.Context) error { return nil }}
			driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) { return child, nil }}
			lifecycle, err := adapter.NewProcessLifecycle(driver, adapter.DefaultProcessLimits())
			if err != nil {
				t.Fatal(err)
			}
			boundary, err := adapter.WithProcessLifecycle(files, lifecycle)
			if err != nil {
				t.Fatal(err)
			}
			program := processCompositionProgram(t, root, fmt.Sprintf(`flow Empty() -> List<Process.Result> { return List.new() }
flow Save(total: Int, result: Process.Result) -> Int! { target = File.Write.new(File.Path.new(%q), result.stdout); receipt = commit target; return Math.add(total, receipt.bytes_written) }
flow Main() -> Int! { %s result = commit target; code = Process.Termination.exit_code(result.termination); selected = Match.new(code, 0: List.new(result), _: Empty()); return Flow.fold(selected, 0, Save) }; return Main()`, path, processGateSource(root)))
			output, runErr := program.Run(context.Background(), boundary, execution.DefaultBudget(), device.DefaultConfig())
			if runErr != nil {
				t.Fatal(runErr)
			}
			count, failure := output.(value.Fallible).Outcome()
			if failure != nil || driver.starts != 1 {
				t.Fatal("pipeline did not complete exactly one Process activation")
			}
			data, readErr := os.ReadFile(path)
			if code == 0 {
				if readErr != nil || !bytes.Equal(data, payload) || count.(value.Integer).Big().Int64() != int64(len(payload)) {
					t.Fatalf("Process output did not reach File: bytes=%v error=%v", data, readErr)
				}
			} else if !errors.Is(readErr, os.ErrNotExist) || count.(value.Integer).Big().Sign() != 0 {
				t.Fatal("nonzero exit unexpectedly wrote a file")
			}
		})
	}
}

func TestAdapterProcessLifecycleRecovery(t *testing.T) {
	for _, host := range []bool{false, true} {
		t.Run(fmt.Sprintf("host-%t", host), func(t *testing.T) {
			root := fileFixtureRoot(t)
			driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) {
				if host {
					return nil, execution.Limit("injected host limit")
				}
				return nil, errors.New(processStdinSecret)
			}}
			program := processCompositionProgram(t, root, `flow Recover(error: Error) -> String { return error.kind }; flow Main() -> String onError Recover { `+processGateSource(root)+` result = commit target; return "completed" }; return Main()`)
			output, runErr := program.Run(context.Background(), lifecycleBoundary(t, driver, 1024), execution.DefaultBudget(), device.DefaultConfig())
			if host {
				lifecycleHostFailure(t, output, runErr)
			} else if runErr != nil || output.(value.Text).Text() != "Process.ExecutionFailed" {
				t.Fatalf("onError did not recover ordinary Process failure: %v", runErr)
			}
		})
	}
}
