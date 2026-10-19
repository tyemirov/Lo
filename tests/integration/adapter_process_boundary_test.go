package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type processContractBoundary struct {
	execution.Boundary
	calls int
}

func (boundary *processContractBoundary) Commit(context.Context, value.Target) (value.Value, error) {
	boundary.calls++
	return nil, fmt.Errorf("unexpected boundary failure: %s %s %s", processArgumentSecret, processEnvironmentSecret, processStdinSecret)
}

func TestAdapterProcessBoundaryContract(t *testing.T) {
	root := fileFixtureRoot(t)
	text, err := source.New(filepath.Join(root, "program.lo"), `flow Recover(error: Error) -> Int { return 73 }; flow Main() -> Int onError Recover { `+processGateSource(root)+` outcome = commit target; return 1 }; return Main()`)
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
	for _, wrapped := range []bool{false, true} {
		t.Run(fmt.Sprintf("wrapped-%t", wrapped), func(t *testing.T) {
			injected := &processContractBoundary{}
			var boundary execution.Boundary = injected
			if wrapped {
				boundary, err = adapter.WithProcess(boundary)
				if err != nil {
					t.Fatal(err)
				}
			}
			output, runErr := program.Run(context.Background(), boundary, execution.DefaultBudget(), device.DefaultConfig())
			if output != nil || runErr == nil {
				t.Fatalf("unqualified boundary returned output=%v, error=%v", output, runErr)
			}
			var stderr bytes.Buffer
			status := runner.ReportRuntime(&stderr, runner.JsonFormat, runErr)
			got := result{status: status, stderr: stderr.String()}
			processNoSecrets(t, got.stderr)
			if wrapped {
				assertProcessUnavailable(t, got)
				if injected.calls != 0 {
					t.Fatal("standard Process gate delegated command activation")
				}
				return
			}
			diagnostic := receiptHostDiagnostic(t, got)
			if status != 2 || diagnostic.Code != "runtime.host-failure" || diagnostic.Error != nil || diagnostic.LastCompleted != nil || injected.calls != 1 {
				t.Fatalf("invalid boundary failure was not a host contract error: %+v, calls=%d", got, injected.calls)
			}
		})
	}
}
