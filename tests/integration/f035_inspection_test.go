package integration_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

type commandPolicyInspection struct {
	processInspection
	Timeout inspectionDescription `json:"timeout_ms"`
	Output  inspectionDescription `json:"output"`
}

type commandPolicyResource struct {
	ID, Operation string
	Process       *commandPolicyInspection
}

func commandPolicyReport(t *testing.T, source string) (inspectionReport, []commandPolicyResource, []commandPolicyResource, string) {
	t.Helper()
	report, _, output := inspectSource(t, source)
	var document struct {
		Report struct {
			Targets, Capabilities []commandPolicyResource
		}
	}
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		t.Fatal(err)
	}
	return report, document.Report.Targets, document.Report.Capabilities, output
}

func TestF035InspectionPreservesImmutableCommandPolicies(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "absent-command")
	source := processCommandSource(t, executable, `List.new("literal argument")`, `Map.new()`, root, `Bytes.new("input")`, `
timed = Process.Command.with_timeout(command, 30000)
forwarded = Process.Command.with_output(timed, "forward")
retimed = Process.Command.with_timeout(forwarded, 60000)
captured = Process.Command.with_output(retimed, "capture")
original_result = commit command
timed_result = commit timed
forwarded_result = commit forwarded
retimed_result = commit retimed
captured_result = commit captured
return captured_result`)
	source = strings.Replace(source, "flow Main() -> Process.Command!", "flow Main() -> Process.Result!", 1)
	report, _, capabilities, output := commandPolicyReport(t, source)
	want := []struct{ timeout, output string }{{"0", "capture"}, {"30000", "capture"}, {"30000", "forward"}, {"60000", "forward"}, {"60000", "capture"}}
	if len(capabilities) != len(want) || len(report.Activations) != len(want) || len(report.Reads) != 0 {
		t.Fatalf("command policy changed activation or read count: %s", output)
	}
	for index, expected := range want {
		capability := capabilities[index]
		process := capability.Process
		if capability.Operation != "Process.run" || process == nil || process.Timeout.Value != expected.timeout || process.Output.Value != expected.output {
			t.Fatalf("activation %d lost immutable command policy: %s", index, output)
		}
		if process.Executable.Value != executable || process.Context.Directory == nil || process.Context.Directory.Value != root || process.Arguments.Node == "" || process.Stdin.Node == "" {
			t.Fatalf("activation %d lost command input provenance: %s", index, output)
		}
	}
}

func TestF035InspectionTransformsDynamicCommand(t *testing.T) {
	source := `flow Invoke(command: Process.Command, duration: Int) -> Process.Result! {
timed = Process.Command.with_timeout(command, duration)
configured = Process.Command.with_output(timed, "forward")
result = commit configured
return result
}`
	report, targets, capabilities, output := commandPolicyReport(t, source)
	if len(capabilities) != 0 || len(report.Activations) != 1 || len(report.Activations[0].Targets) != 1 {
		t.Fatalf("unused dynamic flow gained authority or lost activation: %s", output)
	}
	var process *commandPolicyInspection
	for _, target := range targets {
		if target.ID == report.Activations[0].Targets[0].ID {
			process = target.Process
		}
	}
	if process == nil || process.Output.Value != "forward" || process.Timeout.Kind != "dynamic" || process.Timeout.Expression != "duration" {
		t.Fatalf("dynamic command lost transformed policy: %s", output)
	}
	for _, input := range []inspectionDescription{process.Context.Source, process.Executable, process.Arguments, process.Stdin} {
		if input.Kind != "dynamic" || input.Node == "" || len(input.Dependencies) == 0 {
			t.Fatalf("dynamic command acquired invented inputs: %s", output)
		}
	}
}

func TestF035InspectionKeepsUnrequestedPolicyDynamic(t *testing.T) {
	for _, test := range []struct {
		name, expression, field string
	}{
		{"forward", `Process.Command.with_output(command, "forward")`, "timeout"},
		{"timeout", `Process.Command.with_timeout(command, 30000)`, "output"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := fmt.Sprintf(`flow Invoke(command: Process.Command) -> Process.Result! { configured = %s; result = commit configured; return result }`, test.expression)
			report, targets, _, output := commandPolicyReport(t, source)
			if len(report.Activations) != 1 || len(report.Activations[0].Targets) != 1 {
				t.Fatalf("dynamic command lost activation: %s", output)
			}
			for _, target := range targets {
				if target.ID != report.Activations[0].Targets[0].ID {
					continue
				}
				if target.Process == nil {
					t.Fatalf("dynamic command lost process inputs: %s", output)
				}
				value := target.Process.Timeout
				if test.field == "output" {
					value = target.Process.Output
				}
				if value.Kind != "dynamic" || value.Value != "" {
					t.Fatalf("dynamic command acquired an unrequested %s policy: %s", test.field, output)
				}
				return
			}
			t.Fatalf("dynamic activation target missing: %s", output)
		})
	}
}

func TestF035InspectionPreservesCommandAlternatives(t *testing.T) {
	root := t.TempDir()
	source := fmt.Sprintf(`
flow Select(command: Process.Command, forward: Bool) -> Process.Command! {
return Match.new(forward, true: Process.Command.with_output(command, "forward"), _: command)
}
flow Main() -> Process.Result! {
context = Process.Context.new(Map.new(), File.Path.new(%q))
executable = Process.Executable.new(File.Path.new(%q))
command = Process.Command.new(context, executable, List.new(), Bytes.new(""))
selected = Select(command, true)
timed = Process.Command.with_timeout(selected, 30000)
result = commit timed
return result
}
return Main()`, root, filepath.Join(root, "absent-command"))
	report, _, capabilities, output := commandPolicyReport(t, source)
	if len(report.Activations) != 1 || len(capabilities) != 2 || len(report.Activations[0].Targets) != 2 {
		t.Fatalf("command alternatives lost: %s", output)
	}
	seen := map[string]bool{}
	for _, capability := range capabilities {
		if capability.Process == nil || capability.Process.Timeout.Value != "30000" || capability.Operation != "Process.run" {
			t.Fatalf("command alternative lost transformation: %s", output)
		}
		seen[capability.Process.Output.Value] = true
	}
	if !seen["capture"] || !seen["forward"] {
		t.Fatalf("output alternatives collapsed: %s", output)
	}
	for _, target := range report.Activations[0].Targets {
		conditional := false
		for _, condition := range target.Conditions {
			conditional = conditional || condition.Kind == "match-case"
		}
		if !conditional {
			t.Fatalf("command alternative lost branch condition: %s", output)
		}
	}
}

func TestF035ResultPolicyProjectionTypes(t *testing.T) {
	source := `flow Describe(result: Process.Result) -> Record{output: String, stdout_bytes: Int, stderr_bytes: Int} {
return Record.new(output: result.output, stdout_bytes: result.stdout_bytes, stderr_bytes: result.stderr_bytes)
}`
	got := run(t, "check", sourceFile(t, source), "--format", "json")
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, `"command":"check"`) {
		t.Fatalf("result policy fields unavailable: %+v", got)
	}
}

func TestF035ActivationFailureInspection(t *testing.T) {
	source := f035Constructor(t, `timed = Process.Command.with_timeout(command, 30000); configured = Process.Command.with_output(timed, "forward"); result = commit configured; return result`)
	source = strings.Replace(source, "flow Main() -> Process.Command!", "flow Main() -> Process.Result!", 1)
	report, _, output := inspectSource(t, source)
	for _, failure := range report.ErrorSources {
		if failure.Category != "activation" {
			continue
		}
		execution, timeout := false, false
		for _, kind := range failure.Kinds {
			execution = execution || kind == "Process.ExecutionFailed"
			timeout = timeout || kind == "Process.Timeout"
		}
		if failure.Operation != "Process.Command.new" || !execution || !timeout || len(failure.Kinds) != 2 {
			t.Fatalf("command activation omits recoverable failures: %s", output)
		}
		return
	}
	t.Fatalf("command activation failures absent: %s", output)
}
