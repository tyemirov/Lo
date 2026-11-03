package integration_test

import (
	"encoding/json"
	"fmt"

	"path/filepath"
	"testing"
)

func TestProcessContextDerivedInspection(t *testing.T) {
	root := fileFixtureRoot(t)
	source := processContextProgram("Process.Command", fmt.Sprintf(`initial = Process.Context.current(); patched = Process.Context.override_environment(initial, Map.new(Record.new(key: "CI", value: "1"))); derived = Process.Context.with_directory(patched, File.Path.new(%q)); executable = Process.Executable.new(File.Path.new(%q)); return Process.Command.new(derived, executable, List.new(), Bytes.new(""))`, root, filepath.Join(root, "missing")))
	_, _, output := inspectSource(t, source)
	var document struct {
		Report struct {
			Targets []struct{ Process *processInspection }
		}
	}
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Report.Targets) != 1 || document.Report.Targets[0].Process == nil {
		t.Fatalf("missing Process inputs: %s", output)
	}
	inputs := document.Report.Targets[0].Process.Context
	if inputs.Origin != "derived" || inputs.Directory == nil || inputs.Directory.Value != root || inputs.Base == nil || inputs.Base.Updates == nil || inputs.Base.Base == nil || inputs.Base.Base.Origin != "inherited" {
		t.Fatalf("missing context origin or overrides: %s", output)
	}
}
