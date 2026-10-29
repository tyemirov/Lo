package integration_test

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func fileReadProgram(t *testing.T) execution.Program {
	t.Helper()
	input, err := source.New("file-read-allocation.lo", `return File.read(File.Path.new("payload"))`)
	if err != nil {
		t.Fatal(err)
	}
	checked, problem := compiler.Check(input)
	if problem != nil {
		t.Fatal(problem)
	}
	program, err := eval.Program(checked)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func TestFileReadValidationAllocation(t *testing.T) {
	const payloadSize = 4 << 20
	program := fileReadProgram(t)
	payload := bytes.Repeat([]byte("x"), payloadSize)
	immutable := value.NewBytes(payload)
	boundary := &observedBoundary{read: func(string, int) (value.Bytes, error) { return immutable, nil }}
	budget, config := execution.DefaultBudget(), device.DefaultConfig()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	output, err := program.Run(context.Background(), boundary, budget, config)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	result, failure := output.(value.Fallible).Outcome()
	if failure != nil || !bytes.Equal(result.(value.Bytes).Bytes(), payload) {
		t.Fatalf("read result changed: failure=%v", failure)
	}
	if len(boundary.reads) != 1 || boundary.reads[0] != "payload" || boundary.commits != 0 {
		t.Fatalf("read boundary events changed: reads=%v commits=%d", boundary.reads, boundary.commits)
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("File.read payload=%d allocation_bytes=%d allocation_count=%d", payloadSize, allocated, after.Mallocs-before.Mallocs)
	if allocated >= payloadSize/4 {
		t.Fatalf("read validation allocated %d bytes for a preconstructed %d-byte immutable payload", allocated, payloadSize)
	}
}

func TestFileReadBoundaryRejectsOversizedBytes(t *testing.T) {
	program := fileReadProgram(t)
	immutable := value.NewBytes(make([]byte, execution.MaxBytes+1))
	boundary := &observedBoundary{read: func(string, int) (value.Bytes, error) { return immutable, nil }}
	output, err := program.Run(context.Background(), boundary, execution.DefaultBudget(), device.DefaultConfig())
	var host execution.HostError
	if output != nil || !errors.As(err, &host) || host.Code() != "runtime.limit" || !strings.Contains(err.Error(), "file read bytes") {
		t.Fatalf("oversized boundary read lost its typed size rejection: output=%v error=%v", output, err)
	}
	if len(boundary.reads) != 1 || boundary.commits != 0 {
		t.Fatalf("oversized read boundary events changed: reads=%v commits=%d", boundary.reads, boundary.commits)
	}
}
