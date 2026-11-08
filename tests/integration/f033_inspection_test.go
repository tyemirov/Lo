package integration_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
)

func TestF033InspectionRetainsObservationRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "unobserved-root")
	for _, acquisition := range []string{
		fmt.Sprintf(`directory = File.directory(File.Path.new(%q))`, root),
		fmt.Sprintf(`initial = File.scan(File.Path.new(%q)); directory = initial.directory`, root),
	} {
		source := `flow Main() -> File.RemoveReceipt! { ` + acquisition + `
fresh = File.Directory.refresh(directory)
selected = File.Directory.entry(fresh, "child")
child = File.Directory.child(fresh, selected)
scan = File.Directory.scan(child)
entry = List.at(scan.entries, 0)
observed = File.Entry.observe(entry)
pending = File.Remove.new(observed)
receipt = commit pending
return receipt
}; return Main()`
		report, _, output := inspectSource(t, source)
		if len(report.Reads) != 6 || len(report.Capabilities) != 7 || len(report.Activations) != 1 {
			t.Fatalf("retained operations lost read or activation metadata: %s", output)
		}
		for index, read := range report.Reads {
			if index == 0 {
				continue
			}
			if read.Target.Kind != "calculated" || read.Target.Value != root || read.Target.Node == "" || len(read.Target.Dependencies) == 0 {
				t.Fatalf("retained observation lost its authority root: %s", output)
			}
		}
		for index, capability := range report.Capabilities {
			want := "File.list"
			if index == 3 {
				want = "File.observe_tree"
			} else if index == 6 {
				want = "File.remove"
			}
			if capability.Operation != want || capability.Target.Value != root {
				t.Fatalf("retained authority mismatch at %d: %s", index, output)
			}
		}
	}
}

func TestF033InspectionDynamicObservationsAndFailures(t *testing.T) {
	for _, test := range []struct {
		operation, input, output, arguments string
		errors                              []string
	}{
		{"File.Directory.scan", "File.Directory", "File.Scan", "value", []string{"File.DirectoryChanged", "File.ListFailed", "File.InvalidName"}},
		{"File.Directory.refresh", "File.Directory", "File.Directory", "value", []string{"File.DirectoryChanged", "File.DirectoryFailed"}},
		{"File.Directory.entry", "File.Directory", "File.Entry", `value, "child"`, []string{"File.DirectoryChanged", "File.EntryAbsent", "File.InvalidName", "File.ListFailed"}},
		{"File.Directory.child", "Record{directory: File.Directory, entry: File.Entry}", "File.Directory", "value.directory, value.entry", []string{"File.DirectoryChanged", "File.DirectoryFailed", "File.DirectoryAbsent"}},
		{"File.Entry.observe", "File.Entry", "List<File.Entry>", "value", []string{"File.DirectoryChanged", "File.ListFailed"}},
	} {
		t.Run(test.operation, func(t *testing.T) {
			source := fmt.Sprintf(`flow Read(value: %s) -> %s! { return %s(%s) }`, test.input, test.output, test.operation, test.arguments)
			report, _, output := inspectSource(t, source)
			if len(report.Reads) != 1 || len(report.Capabilities) != 0 || len(report.Activations) != 0 {
				t.Fatalf("unused retained read gained authority: %s", output)
			}
			read := report.Reads[0]
			if read.Operation != test.operation || read.Target.Kind != "dynamic" || read.Target.Value != "" || read.Target.Node == "" {
				t.Fatalf("dynamic observation acquired an invented root: %s", output)
			}
			if len(report.ErrorSources) != 1 || report.ErrorSources[0].Operation != test.operation || !slices.Equal(report.ErrorSources[0].Kinds, test.errors) {
				t.Fatalf("retained observation failure metadata mismatch: %s", output)
			}
		})
	}
}
