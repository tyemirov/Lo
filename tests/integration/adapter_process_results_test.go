package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/codegen"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type processResultBoundary struct {
	execution.Boundary
	output value.ProcessResult
}

func (b processResultBoundary) Commit(context.Context, value.Target) (value.Value, error) {
	return b.output, nil
}

func TestAdapterProcessResultExecution(t *testing.T) {
	root := fileFixtureRoot(t)
	for _, test := range []struct{ name, constructor, expression string }{
		{"exited", "NewProcessExited", `Process.Termination.exit_code(result.termination)`},
		{"signaled", "NewProcessSignaled", `Process.Termination.signal(result.termination)`},
		{"wrong-exited", "NewProcessSignaled", `Process.Termination.exit_code(result.termination)`},
		{"wrong-signaled", "NewProcessExited", `Process.Termination.signal(result.termination)`},
		{"stdout", "NewProcessExited", `result.stdout`},
		{"stderr", "NewProcessExited", `result.stderr`},
		{"kind", "NewProcessSignaled", `result.termination.kind`},
		{"result", "NewProcessExited", `result`},
		{"snapshot", "NewProcessExited", `Error.new("Probe","safe",Record.new(result: result,nested: List.new(result)))`},
	} {
		t.Run(test.name, func(t *testing.T) {
			resultType := "Int"
			switch test.name {
			case "stdout", "stderr":
				resultType = "Bytes"
			case "kind":
				resultType = "String"
			case "result":
				resultType = "Process.Result"
			case "snapshot":
				resultType = "Error"
			}
			text, err := source.New(filepath.Join(root, "result.lo"), fmt.Sprintf(`flow Main() -> %s! { %s result = commit target; return %s }; return Main()`, resultType, processGateSource(root), test.expression))
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
			var termination value.ProcessTermination
			if test.constructor == "NewProcessExited" {
				termination, err = value.NewProcessExited(value.Int64(7))
			} else {
				termination, err = value.NewProcessSignaled(value.Int64(7))
			}
			if err != nil {
				t.Fatal(err)
			}
			output, err := value.NewProcessResult(termination, value.NewBytes([]byte(processArgumentSecret)), value.NewBytes([]byte(processEnvironmentSecret)))
			if err != nil {
				t.Fatal(err)
			}
			got, err := program.Run(context.Background(), processResultBoundary{output: output}, execution.DefaultBudget(), device.DefaultConfig())
			wrongVariant := strings.HasPrefix(test.name, "wrong-")
			if wrongVariant {
				var failure execution.Failure
				if got != nil || !errors.As(err, &failure) || failure.Value().Kind() != "Process.TerminationKind" {
					t.Fatalf("wrong variant: output=%v error=%v", got, err)
				}
				got = failure.Value()
			} else if err != nil {
				t.Fatal(err)
			}
			serialized, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if !wrongVariant {
				outcome, cause := got.(value.Fallible).Outcome()
				if cause != nil {
					t.Fatalf("unexpected failure: %s", serialized)
				}
				switch test.name {
				case "exited", "signaled":
					if outcome.(value.Integer).Big().Int64() != 7 {
						t.Fatalf("payload: %s", serialized)
					}
				case "kind":
					if outcome.(value.Text).Text() != value.ProcessSignaled {
						t.Fatalf("kind: %s", serialized)
					}
				case "snapshot":
					processNoSecrets(t, string(serialized))
				case "result":
					fields := processFields(t, mustRuntimeProcessValue(t, outcome))
					if len(fields) != 3 || fields["termination"].Type != "Process.Termination" {
						t.Fatalf("result: %s", serialized)
					}
				}
			}
			generated, err := codegen.Generate(checked, "main")
			if err != nil {
				t.Fatal(err)
			}
			directory := t.TempDir()
			relative, err := filepath.Rel(repository, directory)
			if err != nil || strings.HasPrefix(relative, "..") {
				t.Fatalf("native fixture outside repository: %s", directory)
			}
			harness := fmt.Sprintf(`package main
import("context";"encoding/json";"errors";"fmt";"github.com/MarcoPoloResearchLab/Lo/internal/device";"github.com/MarcoPoloResearchLab/Lo/internal/execution";"github.com/MarcoPoloResearchLab/Lo/internal/value")
type boundary struct { execution.Boundary }
func(boundary)Commit(context.Context,value.Target)(value.Value,error){termination,err:=value.%s(value.Int64(7));if err!=nil{return nil,err};return value.NewProcessResult(termination,value.NewBytes([]byte(%q)),value.NewBytes([]byte(%q)))}
func main(){program,err:=New();if err!=nil{panic(err)};result,err:=program.Run(context.Background(),boundary{},execution.DefaultBudget(),device.DefaultConfig());if err!=nil{var failure execution.Failure;if !errors.As(err,&failure){panic(err)};result=failure.Value()};data,err:=json.Marshal(result);if err!=nil{panic(err)};fmt.Print(string(data))}
`, test.constructor, processArgumentSecret, processEnvironmentSecret)
			for name, data := range map[string][]byte{"program.go": generated, "main.go": []byte(harness)} {
				if err := os.WriteFile(filepath.Join(directory, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command("go", "run", ".")
			command.Dir = directory
			native, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("native execution: %v: %s", err, native)
			}
			if string(native) != string(serialized) {
				t.Fatalf("native=%s interpreter=%s", native, serialized)
			}
		})
	}
}

func mustRuntimeProcessValue(t *testing.T, input value.Value) runtimeValue {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var result runtimeValue
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAdapterProcessResultSnapshotWeight(t *testing.T) {
	termination, err := value.NewProcessSignaled(value.Int64(15))
	if err != nil {
		t.Fatal(err)
	}
	for _, length := range []int{0, 1, 4096} {
		output, err := value.NewProcessResult(termination, value.NewBytes([]byte(strings.Repeat("a", length))), value.NewBytes([]byte(strings.Repeat("b", length))))
		if err != nil {
			t.Fatal(err)
		}
		listType, err := types.NewList(types.ProcessResult())
		if err != nil {
			t.Fatal(err)
		}
		list, err := value.NewList(listType, []value.Value{output})
		if err != nil {
			t.Fatal(err)
		}
		nested, err := value.NewRecord([]value.Field{{Name: "outputs", Value: list}})
		if err != nil {
			t.Fatal(err)
		}
		for _, original := range []value.Value{output, list, nested, termination} {
			snapshot := value.Snapshot(original)
			if snapshot.Weight() != value.SnapshotWeight(original) || !types.Equal(snapshot.Type(), original.Type()) {
				t.Fatalf("snapshot invariant: %s", original.Type())
			}
		}
	}
}

func TestAdapterProcessTerminationVariants(t *testing.T) {
	for _, test := range []struct {
		name        string
		constructor func(value.Integer) (value.ProcessTermination, error)
		input       value.Integer
		valid       bool
	}{
		{"zero-exit", value.NewProcessExited, value.Int64(0), true},
		{"nonzero-exit", value.NewProcessExited, value.Int64(73), true},
		{"negative-exit", value.NewProcessExited, value.Int64(-1), false},
		{"missing-exit", value.NewProcessExited, nil, false},
		{"signal", value.NewProcessSignaled, value.Int64(15), true},
		{"zero-signal", value.NewProcessSignaled, value.Int64(0), false},
		{"negative-signal", value.NewProcessSignaled, value.Int64(-1), false},
		{"missing-signal", value.NewProcessSignaled, nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			termination, err := test.constructor(test.input)
			if !test.valid {
				if err == nil || termination != nil {
					t.Fatal("invalid termination accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			fields := processFields(t, mustRuntimeProcessValue(t, termination))
			if len(fields) != 2 || fields["kind"].Type != "String" {
				t.Fatalf("termination shape: %+v", fields)
			}
			payload := "exit_code"
			if termination.Kind().Text() == value.ProcessSignaled {
				payload = "signal"
			}
			if fields[payload].Type != "Int" {
				t.Fatalf("missing variant payload: %+v", fields)
			}
			valueJSON(t, fields[payload], fmt.Sprintf("%q", test.input.Big().String()))
		})
	}
}

func TestAdapterProcessResultChecker(t *testing.T) {
	for _, test := range []struct{ name, source string }{
		{"termination-field", `flow Read(result: Process.Result) -> Process.Termination { return result.termination }`},
		{"stdout-field", `flow Read(result: Process.Result) -> Bytes { return result.stdout }`},
		{"stderr-field", `flow Read(result: Process.Result) -> Bytes { return result.stderr }`},
		{"termination-kind", `flow Read(termination: Process.Termination) -> String { return termination.kind }`},
		{"exit-code", `flow Read(termination: Process.Termination) -> Int! { return Process.Termination.exit_code(value: termination) }`},
		{"signal", `flow Read(termination: Process.Termination) -> Int! { return Process.Termination.signal(value: termination) }`},
		{"nested-accessor", `flow Read(result: Process.Result) -> Int! { return Process.Termination.exit_code(result.termination) }`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := run(t, "check", sourceFile(t, test.source), "--format", "json")
			if got.status != 0 {
				t.Fatalf("Process result contract rejected: %+v", got)
			}
		})
	}
}

func TestAdapterProcessResultCheckerRejectsInvalidSurface(t *testing.T) {
	for _, test := range []struct{ name, source string }{
		{"result-constructor", `return Process.Result.new()`},
		{"termination-constructor", `return Process.Termination.new()`},
		{"exited-constructor", `return Process.Termination.exited(0)`},
		{"signaled-constructor", `return Process.Termination.signaled(15)`},
		{"result-flat-exit-code", `flow Read(result: Process.Result) -> Int { return result.exit_code }`},
		{"termination-direct-exit-code", `flow Read(termination: Process.Termination) -> Int { return termination.exit_code }`},
		{"termination-direct-signal", `flow Read(termination: Process.Termination) -> Int { return termination.signal }`},
		{"structural-termination", `return Process.Termination.exit_code(Record.new(kind: "exited", exit_code: 0))`},
		{"wrong-accessor-input", `flow Read(result: Process.Result) -> Int! { return Process.Termination.signal(result) }`},
		{"exit-code-is-fallible", `flow Read(termination: Process.Termination) -> Int { return Process.Termination.exit_code(termination) }`},
		{"signal-is-fallible", `flow Read(termination: Process.Termination) -> Int { return Process.Termination.signal(termination) }`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := run(t, "check", sourceFile(t, test.source), "--format", "json")
			if got.status == 0 {
				t.Fatalf("invalid Process result surface accepted: %s", test.source)
			}
		})
	}
}
