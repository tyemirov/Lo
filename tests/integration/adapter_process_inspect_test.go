package integration_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

type processInspection struct {
	Executable  inspectionDescription `json:"executable"`
	Arguments   inspectionDescription `json:"arguments"`
	Environment inspectionDescription `json:"environment"`
	Directory   inspectionDescription `json:"directory"`
	Stdin       inspectionDescription `json:"stdin"`
	Profile     struct {
		Required bool   `json:"required"`
		Status   string `json:"status"`
	} `json:"execution_profile"`
}

func TestAdapterProcessInspection(t *testing.T) {
	root := fileFixtureRoot(t)
	program := fmt.Sprintf(`flow Main() -> Process.Result! { executable = Process.Executable.new(File.Path.new(%q)); arguments = List.new("arg"); environment = Map.new(Record.new(key: "ENV",value: "value")); directory = File.Path.new(%q); stdin = UTF8.Bytes.new("input"); target = Process.Command.new(executable,arguments,environment,directory,stdin); outcome = commit target; return outcome }; return Main()`, filepath.Join(root, "absent-executable"), filepath.Join(root, "absent-directory"))
	report, path, output := inspectSource(t, program)
	var document struct {
		Report struct {
			Targets []struct {
				Operation string
				Process   *processInspection
			}
			Capabilities []struct {
				Operation string
				Process   *processInspection
			}
		}
	}
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Report.Targets) != 1 || document.Report.Targets[0].Operation != "Process.Command.new" || document.Report.Targets[0].Process == nil {
		t.Fatalf("command construction absent: %s", output)
	}
	if len(document.Report.Capabilities) != 1 || document.Report.Capabilities[0].Operation != "Process.run" || document.Report.Capabilities[0].Process == nil {
		t.Fatalf("Process capability absent: %s", output)
	}
	for _, process := range []*processInspection{document.Report.Targets[0].Process, document.Report.Capabilities[0].Process} {
		if !process.Profile.Required || process.Profile.Status != "unavailable" {
			t.Fatalf("inspection implies available Process enforcement: %s", output)
		}
		inputs := []struct {
			name, kind string
			value      inspectionDescription
		}{{"executable", "Process.Executable", process.Executable}, {"arguments", "List<String>", process.Arguments}, {"environment", "Map<String, String>", process.Environment}, {"directory", "File.Path", process.Directory}, {"stdin", "Bytes", process.Stdin}}
		for _, input := range inputs {
			if input.value.Type != input.kind || input.value.Node == "" || len(input.value.Span) == 0 || len(input.value.Dependencies) == 0 {
				t.Fatalf("missing %s description: %s", input.name, output)
			}
			found := false
			for _, binding := range report.Bindings {
				if binding.Name == input.name {
					for _, dependency := range input.value.Dependencies {
						found = found || dependency == binding.Node
					}
				}
			}
			if !found {
				t.Fatalf("missing %s input provenance: %s", input.name, output)
			}
		}
		if process.Executable.Value != filepath.Join(root, "absent-executable") || process.Directory.Value != filepath.Join(root, "absent-directory") {
			t.Fatalf("lost declared paths: %s", output)
		}
	}
	if report.ResultType != "Process.Result!" || len(report.Reads) != 0 || len(report.Activations) != 1 {
		t.Fatalf("incorrect pure construction or activation result: %s", output)
	}
	typedCommit := false
	for _, node := range report.Nodes {
		typedCommit = typedCommit || node.Kind == "commit" && node.Type == "Process.Result"
	}
	if !typedCommit {
		t.Fatalf("missing nominal commit result: %s", output)
	}
	text := run(t, "inspect", path)
	if text.status != 0 || !strings.Contains(text.stdout, "execution_profile=required/unavailable") {
		t.Fatalf("text inspection omits unmet profile: %+v", text)
	}
	for _, name := range []string{"executable", "arguments", "environment", "directory", "stdin"} {
		if !strings.Contains(text.stdout, name+"=") {
			t.Fatalf("text inspection omits %s: %+v", name, text)
		}
	}
}

func TestAdapterProcessInspectionDynamicTarget(t *testing.T) {
	_, _, output := inspectSource(t, `flow Invoke(command: Process.Command) -> Process.Result! { outcome = commit command; return outcome }`)
	var document struct {
		Report struct {
			Targets []struct {
				Origin  string
				Process *processInspection
			}
			Capabilities []json.RawMessage
		}
	}
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Report.Targets) != 1 || document.Report.Targets[0].Origin != "dynamic-target" || document.Report.Targets[0].Process == nil || len(document.Report.Capabilities) != 0 {
		t.Fatalf("unknown command lost inspection: %s", output)
	}
	process := document.Report.Targets[0].Process
	if !process.Profile.Required || process.Profile.Status != "unavailable" {
		t.Fatalf("unknown command lost execution gate: %s", output)
	}
	for _, input := range []inspectionDescription{process.Executable, process.Arguments, process.Environment, process.Directory, process.Stdin} {
		if input.Kind != "dynamic" || input.Node == "" || len(input.Dependencies) != 2 || input.Expression != "outcome = commit command" {
			t.Fatalf("unknown input provenance lost: %s", output)
		}
	}
}
