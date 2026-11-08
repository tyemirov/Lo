package integration_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func f035Constructor(t *testing.T, finish string) string {
	t.Helper()
	root := t.TempDir()
	return processCommandSource(t, filepath.Join(root, "absent-command"), `List.new("literal argument")`, `Map.new()`, root, `Bytes.new("input")`, finish)
}

func TestF035CommandTransformConstruction(t *testing.T) {
	source := f035Constructor(t, `timed = Process.Command.with_timeout(command, 1800000); configured = Process.Command.with_output(timed, "forward"); return configured`)
	value := fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--format", "json"))
	if value.Type != "Process.Command" {
		t.Fatalf("transformation lost the nominal command: %+v", value)
	}
	fields := processFields(t, value)
	valueJSON(t, fields["arguments"], `[{"type":"String","value":"literal argument"}]`)
	valueJSON(t, fields["stdin"], `"aW5wdXQ="`)
	valueJSON(t, fields["context"], `{"origin":"explicit","redacted":true}`)
}

func TestF035TimeoutConstructionSigned64Range(t *testing.T) {
	source := f035Constructor(t, `return Process.Command.with_timeout(command, 9223372036854775807)`)
	value := fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--format", "json"))
	valueJSON(t, processFields(t, value)["timeout_ms"], `"9223372036854775807"`)
	processConstructorFailure(t, f035Constructor(t, `return Process.Command.with_timeout(command,9223372036854775808)`), "Process.InvalidCommand")
}

func TestF035CommandTransformsRemainPure(t *testing.T) {
	root := t.TempDir()
	source := fmt.Sprintf(`
flow Configure(path: File.Path) -> Process.Command! {
    context = Process.Context.new(Map.new(), path)
    executable = Process.Executable.new(path)
    command = Process.Command.new(context, executable, List.new(), Bytes.new(""))
    timed = Process.Command.with_timeout(command, 30000)
    return Process.Command.with_output(timed, "forward")
}
return List.map(List.new(File.Path.new(%q)), Configure)`, filepath.Join(root, "not-created"))
	value := fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--format", "json"))
	if value.Type != "List<Process.Command>" {
		t.Fatalf("pure command callback: %+v", value)
	}
}

func TestF035InvalidCommandOptions(t *testing.T) {
	for _, test := range []struct{ name, expression string }{
		{"zero-timeout", `Process.Command.with_timeout(command, 0)`},
		{"negative-timeout", `Process.Command.with_timeout(command, -1)`},
		{"overflow-timeout", `Process.Command.with_timeout(command, 18446744073709551616)`},
		{"empty-output", `Process.Command.with_output(command, "")`},
		{"obsolete-output", `Process.Command.with_output(command, "inherit")`},
		{"unknown-output", `Process.Command.with_output(command, "discard")`},
	} {
		t.Run(test.name, func(t *testing.T) {
			processConstructorFailure(t, f035Constructor(t, "return "+test.expression), "Process.InvalidCommand")
		})
	}
}

func TestF035CommandOptionTypes(t *testing.T) {
	for _, expression := range []string{
		`Process.Command.with_timeout(command, "30")`,
		`Process.Command.with_timeout(command, 30.0)`,
		`Process.Command.with_output(command, true)`,
	} {
		got := run(t, "check", sourceFile(t, f035Constructor(t, "return "+expression)), "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, "check.type-mismatch") {
			t.Fatalf("invalid option type: %+v", got)
		}
	}
}

func TestF035CommandOptionInspection(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "absent-command")
	source := processContextProgram("Process.Result", fmt.Sprintf(`context = Process.Context.new(Map.new(), File.Path.new(%q)); executable = Process.Executable.new(File.Path.new(%q)); command = Process.Command.new(context, executable, List.new("argument"), Bytes.new("input")); timed = Process.Command.with_timeout(command, 30000); configured = Process.Command.with_output(timed, "forward"); result = commit configured; return result`, root, executable))
	report, _, output := inspectSource(t, source)
	var document struct {
		Report struct {
			Capabilities []struct {
				Operation string
				Process   *struct {
					processInspection
					Timeout inspectionDescription `json:"timeout_ms"`
					Output  inspectionDescription `json:"output"`
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Report.Capabilities) != 1 || document.Report.Capabilities[0].Operation != "Process.run" || document.Report.Capabilities[0].Process == nil {
		t.Fatalf("transformed command lost activation authority: %s", output)
	}
	process := document.Report.Capabilities[0].Process
	if process.Executable.Value != executable || process.Context.Directory == nil || process.Context.Directory.Value != root || process.Timeout.Value != "30000" || process.Output.Value != "forward" {
		t.Fatalf("transformed command lost inputs or requested policy: %s", output)
	}
	if len(report.Reads) != 0 || len(report.Activations) != 1 {
		t.Fatalf("transform changed activation or observation count: %s", output)
	}
}

func TestF035HostLimitArguments(t *testing.T) {
	artifact, built := buildForTest(t, sourceFile(t, "return 1"))
	if built.status != 0 {
		t.Fatalf("build host-limit fixture: %+v", built)
	}
	for _, test := range []struct {
		name  string
		args  []string
		valid bool
	}{
		{"long-duration", []string{"--max-process-ms", "1800000"}, true},
		{"explicit-limits", []string{"--max-process-ms", "60000", "--max-process-cleanup-ms", "2000", "--max-process-output-bytes", "2097152", "--process-forward-output"}, true},
		{"zero-duration", []string{"--max-process-ms", "0"}, false},
		{"zero-cleanup", []string{"--max-process-cleanup-ms", "0"}, false},
		{"zero-output", []string{"--max-process-output-bytes", "0"}, false},
		{"overflow-duration", []string{"--max-process-ms", "18446744073709551616"}, false},
		{"negative-output", []string{"--max-process-output-bytes", "-1"}, false},
		{"duplicate-duration", []string{"--max-process-ms", "30000", "--max-process-ms", "60000"}, false},
		{"duplicate-forward", []string{"--process-forward-output", "--process-forward-output"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := runExecutable(t, artifact, append([]string{"run"}, test.args...)...)
			if test.valid {
				if got.status != 0 || got.stdout != "1\n" || got.stderr != "" {
					t.Fatalf("valid host limits: %+v", got)
				}
			} else if got.status != 2 || got.stdout != "" {
				t.Fatalf("invalid host limits: %+v", got)
			}
		})
	}
}
