package integration_test

import (
	"testing"
)

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
