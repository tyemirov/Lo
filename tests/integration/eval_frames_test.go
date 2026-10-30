package integration_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func TestEvaluatorRepeatedFramesPreserveFailures(t *testing.T) {
	text := `flow Recover(error: Error) -> Error { return error }; flow Divide(seed: Int, divisor: Int) -> Int! { numerator = Math.add(seed, 1); result = Math.div(numerator, divisor); return Math.add(result, seed) }; flow Capture(seed: Int, divisor: Int) -> Error onError Recover { result = Divide(seed, divisor); return Error.new("success", Int.to_string(result), Record.new(seed: seed, divisor: divisor)) }; return List.new(Capture(10, 0), Capture(20, 2), Capture(30, 0), Capture(40, 4))`
	got, err := evaluate(t, text, &observedBoundary{})
	if err != nil {
		t.Fatal(err)
	}
	items := got.(value.List).Items()
	if len(items) != 4 {
		t.Fatal(items)
	}
	for index, numerator := range map[int]string{0: "11", 2: "31"} {
		failure := items[index].(value.Error)
		if failure.Kind() != "Math.DivideByZero" || len(failure.Trace()) != 2 || value.TextResult(failure.Input().Field("left_value")) != numerator {
			t.Fatalf("failure %d lost its original input: %s", index, value.TextResult(failure))
		}
		for frame, name := range []string{"Divide", "Capture"} {
			if got := value.TextResult(failure.Trace()[frame].Field("flow")); got != name {
				t.Fatalf("failure %d frame %d: %q, want %q", index, frame, got, name)
			}
		}
	}
	for index, expected := range map[int]string{1: "30", 3: "50"} {
		result := items[index].(value.Error)
		if result.Kind() != "success" || result.Message() != expected || len(result.Trace()) != 0 {
			t.Fatalf("success %d reused a prior failure: %s", index, value.TextResult(result))
		}
	}
}

func TestEvaluatorProgramFramesAreInvocationLocal(t *testing.T) {
	input, err := source.New("concurrent-eval.lo", `flow Read(state: String, item: Int) -> String! { bytes = File.read(File.Path.new(Int.to_string(item))); return String.concat(state, UTF8.String.new(bytes), ";") }; return Flow.fold(List.new(1, 2, 3), "", Read)`)
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
	const count = 8
	arrived, release := make(chan struct{}, count), make(chan struct{})
	type result struct {
		tag string
		got value.Value
		err error
	}
	results := make(chan result, count)
	for index := range count {
		tag := fmt.Sprintf("run-%d", index)
		go func() {
			boundary := &observedBoundary{read: func(path string, n int) (value.Bytes, error) {
				if n == 1 {
					arrived <- struct{}{}
					<-release
				}
				return value.NewBytes([]byte(tag + ":" + path)), nil
			}}
			got, err := program.Run(context.Background(), boundary, execution.DefaultBudget(), device.DefaultConfig())
			results <- result{tag, got, err}
		}()
	}
	for range count {
		select {
		case <-arrived:
		case result := <-results:
			close(release)
			t.Fatalf("%s returned before concurrent reads began: %v", result.tag, result.err)
		}
	}
	close(release)
	for range count {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		got, failure := result.got.(value.Fallible).Outcome()
		if failure != nil {
			t.Fatalf("%s failed: %s", result.tag, value.TextResult(failure))
		}
		if observed, expected := value.TextResult(got), fmt.Sprintf("%s:1;%s:2;%s:3;", result.tag, result.tag, result.tag); observed != expected {
			t.Fatalf("shared program reused another invocation's frame: got %q, want %q", observed, expected)
		}
	}
}
