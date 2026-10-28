package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/runtimeops"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func TestAdapterProcessActivationContract(t *testing.T) {
	operation, ok := catalog.Lookup(catalog.ProcessCommandNew)
	if !ok || operation.Capability() != catalog.ProcessRun || !reflect.DeepEqual(operation.ActivationKinds(), []string{"Process.ExecutionFailed"}) {
		t.Fatal("Process command does not declare its recoverable activation error")
	}
}

func TestAdapterProcessCompletionBeforeResultCharge(t *testing.T) {
	root := fileFixtureRoot(t)
	path, err := value.NewText(types.Path(), filepath.Join(root, "inert"))
	if err != nil {
		t.Fatal(err)
	}
	executable, err := value.NewProcessExecutable(path)
	if err != nil {
		t.Fatal(err)
	}
	argumentsType, err := types.NewList(types.String())
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := value.NewList(argumentsType, nil)
	if err != nil {
		t.Fatal(err)
	}
	environmentType, err := types.NewMap(types.String(), types.String())
	if err != nil {
		t.Fatal(err)
	}
	environment, err := value.NewMap(environmentType, nil)
	if err != nil {
		t.Fatal(err)
	}
	processContext, err := value.NewProcessContext(environment, path)
	if err != nil {
		t.Fatal(err)
	}
	command, err := value.NewProcessCommand(processContext, executable, arguments, value.NewBytes(nil))
	if err != nil {
		t.Fatal(err)
	}
	termination, err := value.NewProcessExited(value.Int64(7))
	if err != nil {
		t.Fatal(err)
	}
	result, err := value.NewProcessResult(termination, value.NewBytes([]byte(processArgumentSecret)), value.NewBytes([]byte(processEnvironmentSecret)))
	if err != nil {
		t.Fatal(err)
	}
	span, err := source.NewSpan(filepath.Join(root, "program.lo"), source.Position{Line: 1, Column: 1}, source.Position{Offset: 9, Line: 1, Column: 10})
	if err != nil {
		t.Fatal(err)
	}
	program, err := execution.NewProgram(func(session execution.Session) (value.Value, error) {
		charge := value.NewBytes(make([]byte, execution.MaxValueWeight-16))
		for index := 0; index < 4; index++ {
			if err := session.ChargeValue(charge); err != nil {
				return nil, err
			}
		}
		return runtimeops.Commit(session, command, span)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, runErr := program.Run(context.Background(), processResultBoundary{output: result}, execution.DefaultBudget(), device.DefaultConfig())
	if output != nil || runErr == nil {
		t.Fatalf("output=%v error=%v", output, runErr)
	}
	var stderr bytes.Buffer
	status := runner.ReportRuntime(&stderr, runner.JsonFormat, runErr)
	processNoSecrets(t, stderr.String())
	var report struct {
		Diagnostics []struct {
			Code      string `json:"code"`
			Completed struct {
				Operation  string       `json:"operation"`
				ResultType string       `json:"result_type"`
				Process    runtimeValue `json:"process"`
			} `json:"last_completed_activation"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if status != 2 || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "runtime.limit" {
		t.Fatalf("missing post-completion limit: %s", stderr.String())
	}
	completed := report.Diagnostics[0].Completed
	if completed.Operation != "Process.run" || completed.ResultType != "Process.Result" {
		t.Fatalf("missing Process completion: %s", stderr.String())
	}
	fields := processFields(t, completed.Process)
	if len(fields) != 3 || fields["termination"].Type != "Process.Termination" {
		t.Fatalf("unsafe completion shape: %s", stderr.String())
	}
}
