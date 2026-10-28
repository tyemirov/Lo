package integration_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const processContextSecret = "f024-inherited-secret-must-stay-redacted"

func TestProcessContextEnvironment(t *testing.T) {
	t.Setenv("LO_F024_INHERITED", processContextSecret)
	root := fileFixtureRoot(t)
	cases := []struct{ name, typ, source, expected string }{
		{"inherited", "String", `context = Process.Context.current(); return Process.Context.environment_value(context, "LO_F024_INHERITED")`, processLiteral(t, processContextSecret)},
		{"override", "Record{initial: String, derived: String}", `initial = Process.Context.current(); derived = Process.Context.override_environment(initial, Map.new(Record.new(key: "LO_F024_INHERITED", value: "override"))); return Record.new(initial: Process.Context.environment_value(initial, "LO_F024_INHERITED"), derived: Process.Context.environment_value(derived, "LO_F024_INHERITED"))`, `{"initial":{"type":"String","value":"` + processContextSecret + `"},"derived":{"type":"String","value":"override"}}`},
		{"empty-value", "String", fmt.Sprintf(`context = Process.Context.new(Map.new(Record.new(key: "EMPTY", value: "")), File.Path.new(%s)); return Process.Context.environment_value(context, "EMPTY")`, processLiteral(t, root)), `""`},
		{"independent-removal", "String", `initial = Process.Context.current(); derived = Process.Context.without_environment(initial, List.new("LO_F024_INHERITED", "ABSENT_F024")); return Process.Context.environment_value(initial, "LO_F024_INHERITED")`, processLiteral(t, processContextSecret)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := fileOutput(t, run(t, "run", sourceFile(t, processContextProgram(test.typ, test.source)), "--format", "json"))
			valueJSON(t, got, test.expected)
		})
	}
}

func TestProcessContextAbsentEnvironment(t *testing.T) {
	root := fileFixtureRoot(t)
	for _, source := range []string{
		fmt.Sprintf(`context = Process.Context.new(Map.new(), File.Path.new(%s)); return Process.Context.environment_value(context, "ABSENT_F024")`, processLiteral(t, root)),
		`initial = Process.Context.current(); context = Process.Context.without_environment(initial, List.new("LO_F024_INHERITED")); return Process.Context.environment_value(context, "LO_F024_INHERITED")`,
	} {
		t.Setenv("LO_F024_INHERITED", processContextSecret)
		processConstructorFailure(t, processContextProgram("String", source), "Process.EnvironmentAbsent")
	}
}

func TestProcessContextInvalidInitialInputs(t *testing.T) {
	t.Setenv("LO_F024_INVALID", "private"+string([]byte{255}))
	processConstructorFailure(t, `return Process.Context.current()`, "Process.ContextUnavailable")
	got := fileOutput(t, run(t, "run", sourceFile(t, `return "ordinary"`), "--format", "json"))
	valueJSON(t, got, `"ordinary"`)
}

func TestProcessContextInvalidChanges(t *testing.T) {
	root := fileFixtureRoot(t)
	for _, operation := range []string{
		`Process.Context.with_directory(initial, File.Path.new("relative"))`,
		`Process.Context.override_environment(initial, Map.new(Record.new(key: "", value: "secret")))`,
		`Process.Context.without_environment(initial, List.new("BAD=NAME"))`,
	} {
		source := processContextProgram("Process.Context", fmt.Sprintf(`initial = Process.Context.new(Map.new(), File.Path.new(%q)); return %s`, root, operation))
		processConstructorFailure(t, source, "Process.InvalidContext")
	}
}

func TestProcessContextStructuralRedaction(t *testing.T) {
	t.Setenv("LO_F024_INHERITED", processContextSecret)
	root := fileFixtureRoot(t)
	for _, test := range []struct{ typ, expression string }{
		{"Process.Context", `context`},
		{"Record{context: Process.Context, nested: List<Process.Context>}", `Record.new(context: context, nested: List.new(context))`},
		{"Process.Command", fmt.Sprintf(`Process.Command.new(context, Process.Executable.new(File.Path.new(%s)), List.new(), Bytes.new(""))`, processLiteral(t, root+"/missing"))},
		{"Error", `Error.new("Probe", "safe", Record.new(context: context, nested: List.new(context)))`},
	} {
		source := processContextProgram(test.typ, `context = Process.Context.current(); return `+test.expression)
		got := run(t, "run", sourceFile(t, source), "--format", "json")
		if got.status != 0 {
			t.Fatalf("context output: %+v", got)
		}
		if strings.Contains(got.stdout+got.stderr, processContextSecret) {
			t.Fatalf("inherited context leaked: %+v", got)
		}
	}
}

func processContextProgram(typ, body string) string {
	return "flow Main() -> " + typ + "! { " + body + " }; return Main()"
}

func TestProcessContextObservationPurity(t *testing.T) {
	for _, source := range []string{
		`flow Observe(number: Int) -> Process.Context! { return Process.Context.current() }; return List.map(List.new(1), Observe)`,
		`flow Resolve(context: Process.Context) -> Process.Executable! { return Process.Executable.resolve(context, "tool") }; flow Main() -> List<Process.Executable>! { context = Process.Context.current(); return List.map(List.new(context), Resolve) }; return Main()`,
	} {
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, "check.effectful-callback") {
			t.Fatalf("expected observation rejection: %+v", got)
		}
	}
}

func TestProcessContextPureConstruction(t *testing.T) {
	root := fileFixtureRoot(t)
	source := fmt.Sprintf(`flow Build(path: File.Path) -> Process.Command! { context = Process.Context.new(Map.new(), path); executable = Process.Executable.new(path); return Process.Command.new(context, executable, List.new(), Bytes.new("")) }; return List.map(List.new(File.Path.new(%s)), Build)`, processLiteral(t, root+"/never-created"))
	got := fileOutput(t, run(t, "run", sourceFile(t, source), "--format", "json"))
	if got.Type != "List<Process.Command>" {
		t.Fatalf("pure construction: %+v", got)
	}
}

func TestProcessExecutableDiscovery(t *testing.T) {
	first, second := fileFixtureRoot(t), fileFixtureRoot(t)
	for _, root := range []string{first, second} {
		if err := os.Link(binary, filepath.Join(root, "fixture-tool")); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(binary, filepath.Join(first, "linked-tool")); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, path, lookup, executable string }{
		{"first-candidate", first + ":" + second, "fixture-tool", filepath.Join(first, "fixture-tool")},
		{"next-directory", first + "/absent:" + second, "fixture-tool", filepath.Join(second, "fixture-tool")},
		{"resolved-symlink", first, "linked-tool", binary},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := processContextProgram("Process.Executable", fmt.Sprintf(`context = Process.Context.new(Map.new(Record.new(key: "PATH", value: %s)), File.Path.new(%s)); return Process.Executable.resolve(context, %s)`, processLiteral(t, test.path), processLiteral(t, first), processLiteral(t, test.lookup)))
			got := fileOutput(t, run(t, "run", sourceFile(t, source), "--format", "json"))
			fields := processFields(t, got)
			valueJSON(t, fields["path"], processLiteral(t, test.executable))
		})
	}
}

func TestProcessExecutableDiscoveryFailures(t *testing.T) {
	root := fileFixtureRoot(t)
	symlink := filepath.Join(root, "linked-tool")
	if err := os.Symlink(symlink, symlink); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, environment, executable, kind string }{
		{"missing-path", `Map.new()`, "tool", "Process.ExecutableAbsent"},
		{"empty-path", `Map.new(Record.new(key: "PATH", value: ""))`, "tool", "Process.DiscoveryFailed"},
		{"relative-path-entry", `Map.new(Record.new(key: "PATH", value: "relative"))`, "tool", "Process.DiscoveryFailed"},
		{"empty-path-entry", fmt.Sprintf(`Map.new(Record.new(key: "PATH", value: %s))`, processLiteral(t, root+":")), "tool", "Process.DiscoveryFailed"},
		{"absent-tool", fmt.Sprintf(`Map.new(Record.new(key: "PATH", value: %s))`, processLiteral(t, root)), "tool", "Process.ExecutableAbsent"},
		{"relative-explicit", `Map.new()`, "./tool", "Process.DiscoveryFailed"},
		{"root-directory", `Map.new()`, "/", "Process.ExecutableAbsent"},
		{"symlink-loop", `Map.new()`, symlink, "Process.DiscoveryFailed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := processContextProgram("Process.Executable", fmt.Sprintf(`context = Process.Context.new(%s, File.Path.new(%s)); return Process.Executable.resolve(context, %s)`, test.environment, processLiteral(t, root), processLiteral(t, test.executable)))
			processConstructorFailure(t, source, test.kind)
		})
	}
}

func TestProcessExecutableDiscoveryUnauthorizedFirst(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires the selected Darwin Process profile")
	}
	first, second := fileFixtureRoot(t), fileFixtureRoot(t)
	for _, root := range []string{first, second} {
		if err := os.Link(binary, filepath.Join(root, "fixture-tool")); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	source := processContextProgram("Process.Result", fmt.Sprintf(`context = Process.Context.new(Map.new(Record.new(key: "PATH", value: %q)), File.Path.new(%q)); executable = Process.Executable.resolve(context, "fixture-tool"); command = Process.Command.new(context, executable, List.new(), Bytes.new("")); result = commit command; return result`, first+":"+second, first))
	got := run(t, "run", sourceFile(t, source), "--process-allow", fmt.Sprintf("%x,%s", digest, filepath.Join(second, "fixture-tool")), "--process-read-root", first, "--process-launcher", filepath.Join(repository, "bin", "lo-process-launcher"), "--format", "json")
	diagnostic := receiptHostDiagnostic(t, got)
	if got.status != 2 || diagnostic.Code != "runtime.capability-denied" || diagnostic.Error != nil || diagnostic.ProcessProgress == nil {
		t.Fatalf("unauthorized first candidate: %+v", got)
	}
	progress := processFields(t, *diagnostic.ProcessProgress)
	valueJSON(t, progress["start_confirmed"], `false`)
}
