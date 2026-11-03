package integration_test

import (
	"fmt"

	"os"
	"strings"
	"testing"
)

func TestHostInvocationArguments(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments []string
		expected  string
	}{
		{"absent", nil, `[]`},
		{"literals", []string{"--", "literal argument", "--flag", ""}, `[{"type":"String","value":"literal argument"},{"type":"String","value":"--flag"},{"type":"String","value":""}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := `flow Main() -> List<String>! { initial = Host.Invocation.current(); return initial.arguments }; return Main()`
			arguments := []string{"run", sourceFile(t, source), "--format", "json"}
			arguments = append(arguments, test.arguments...)
			valueJSON(t, fileOutput(t, runProgram(t, arguments[1], arguments[0], arguments[2:]...)), test.expected)
		})
	}
}

func TestHostInvocationCaptureAndRedaction(t *testing.T) {
	const privateArgument = "f025-private-program-argument"
	home := fileFixtureRoot(t)
	t.Setenv("HOME", home)
	source := `flow Main() -> Record{same_clock: Bool, same_uid: Bool, snapshot: Host.Invocation, nested: List<Host.Invocation>}! { first = Host.Invocation.current(); second = Host.Invocation.current(); return Record.new(same_clock: Math.eq(first.now_unix_ns, second.now_unix_ns), same_uid: Math.eq(first.real_uid, second.real_uid), snapshot: first, nested: List.new(first)) }; return Main()`
	got := runProgram(t, sourceFile(t, source), "run", "--format", "json", "--", privateArgument)
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("host invocation output: %+v", got)
	}
	if strings.Contains(got.stdout, home) || strings.Contains(got.stdout, privateArgument) {
		t.Fatalf("host invocation exposed private captured inputs: %s", got.stdout)
	}
	fields := processFields(t, fileOutput(t, got))
	valueJSON(t, fields["same_clock"], `true`)
	valueJSON(t, fields["same_uid"], `true`)
	explicit := `flow Main() -> File.Path! { initial = Host.Invocation.current(); return initial.home }; return Main()`
	valueJSON(t, fileOutput(t, runProgram(t, sourceFile(t, explicit), "run", "--format", "json")), fmt.Sprintf(`%q`, home))
}

func TestHostInvocationDeferredCaptureFailure(t *testing.T) {
	for _, test := range []struct {
		name string
		home string
	}{
		{"relative", "relative-private-home"}, {"empty", ""}, {"missing", ""}, {"invalid-utf8", string([]byte{'/', 255})},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HOME", test.home)
			if test.name == "missing" {
				if err := os.Unsetenv("HOME"); err != nil {
					t.Fatal(err)
				}
			}
			valueJSON(t, fileOutput(t, runProgram(t, sourceFile(t, `return "ordinary"`), "run", "--format", "json")), `"ordinary"`)
			processConstructorFailure(t, `return Host.Invocation.current()`, "Host.InvocationFailed")
		})
	}
}

func TestHostInvocationArgumentAdmission(t *testing.T) {
	source := sourceFile(t, `return "ordinary"`)
	got := runProgram(t, source, "run", "--format", "json", "--", strings.Repeat("x", 65536))
	if got.status != 2 {
		t.Fatalf("oversized program argument must fail admission: %+v", got)
	}
}

func TestHostInvocationObservationPurity(t *testing.T) {
	source := `flow Observe(index: Int) -> Host.Invocation! { return Host.Invocation.current() }; return List.map(List.new(1), Observe)`
	got := run(t, "check", sourceFile(t, source), "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, "check.effectful-callback") {
		t.Fatalf("host observation in pure callback: %+v", got)
	}
}

func TestNativeHostInvocationArgumentAdmission(t *testing.T) {
	artifact, built := buildForTest(t, sourceFile(t, `return "ordinary"`))
	if built.status != 0 {
		t.Fatalf("build artifact: %+v", built)
	}
	for _, arguments := range [][]string{{strings.Repeat("x", 65536)}, make([]string, 4097), {string([]byte{255})}} {
		args := append([]string{"run", "--format", "json", "--"}, arguments...)
		got := runExecutable(t, artifact, args...)
		if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "cli.invalid-invocation" {
			t.Fatalf("native host argument admission: %+v", got)
		}
	}
}
