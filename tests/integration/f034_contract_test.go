package integration_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func f034Progress(t *testing.T, document string, completed, total, failedIndex int, failedRelative, stage, kind string) map[string]runtimeValue {
	t.Helper()
	var report struct {
		Diagnostics []struct {
			Error    *runtimeValue
			Progress *runtimeValue `json:"removal_progress"`
		}
	}
	if err := json.Unmarshal([]byte(document), &report); err != nil {
		t.Fatalf("tree progress JSON: %v: %s", err, document)
	}
	if len(report.Diagnostics) != 1 {
		t.Fatalf("missing tree diagnostic: %s", document)
	}
	diagnostic := report.Diagnostics[0]
	var progress runtimeValue
	if kind == "" {
		if diagnostic.Progress == nil {
			t.Fatalf("missing terminal tree progress: %s", document)
		}
		progress = *diagnostic.Progress
	} else {
		if diagnostic.Error == nil || diagnostic.Error.Kind != kind || diagnostic.Error.Input == nil {
			t.Fatalf("tree error kind differs from %s: %s", kind, document)
		}
		progress = recordField(t, *diagnostic.Error.Input, "progress")
	}
	var fields map[string]runtimeValue
	if err := json.Unmarshal(progress.Value, &fields); err != nil {
		t.Fatalf("tree progress fields: %v: %s", err, document)
	}
	if len(fields) != 6 {
		t.Fatalf("tree progress must have six canonical fields: %s", progress.Value)
	}
	valueJSON(t, fields["completed_count"], fmt.Sprintf(`"%d"`, completed))
	valueJSON(t, fields["total_count"], fmt.Sprintf(`"%d"`, total))
	valueJSON(t, fields["stage"], fmt.Sprintf("%q", stage))
	if failedIndex != -2 {
		valueJSON(t, fields["failed_index"], fmt.Sprintf(`"%d"`, failedIndex))
		valueJSON(t, fields["failed_relative"], fmt.Sprintf("%q", failedRelative))
	}
	return fields
}

func TestF034TypedContracts(t *testing.T) {
	for _, source := range []string{
		f034Predicates + `flow Read(parent: File.Directory, root: File.Entry) -> File.Tree! { return File.Tree.snapshot(parent, root, Descend) }; return 0`,
		f034Predicates + `flow Build(tree: File.Tree) -> File.Tree.Remove! { return File.Tree.Remove.checked(tree, Eligible) }; return 0`,
		`flow Read(tree: File.Tree) -> List<Record{relative: String, entry: File.Entry}> { return tree.entries }; return 0`,
		`flow Read(tree: File.Tree) -> Bool { return tree.complete }; return 0`,
		`flow Apply(target: File.Tree.Remove) -> File.RemoveReceipt! { receipt = commit target; return receipt }; return 0`,
		`flow Descend(owner: Int, entry: File.Entry) -> Bool { return Math.eq(owner, entry.owner_uid) }; flow Eligible(owner: Int, row: Record{relative: String, entry: File.Entry}) -> Bool { return Math.eq(owner, row.entry.owner_uid) }; flow Build(parent: File.Directory, root: File.Entry) -> File.Tree.Remove! { tree = File.Tree.snapshot(parent, root, 7, Descend); return File.Tree.Remove.checked(tree, 7, Eligible) }; return 0`,
	} {
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 0 || got.stderr != "" {
			t.Fatalf("tree public type contract: %+v", got)
		}
	}
}

func TestF034RejectsInvalidCallbackContracts(t *testing.T) {
	for _, fixture := range []struct{ name, flow, call string }{
		{"descend-return", `flow Callback(entry: File.Entry) -> String { return entry.name }`, "File.Tree.snapshot(parent, root, Callback)"},
		{"descend-fallible", `flow Callback(entry: File.Entry) -> Bool! { return true }`, "File.Tree.snapshot(parent, root, Callback)"},
		{"descend-effect", `flow Recover(error: Error) -> Bool { return false }; flow Callback(entry: File.Entry) -> Bool onError Recover { return Math.gt(Clock.now_unix_ns(), 0) }`, "File.Tree.snapshot(parent, root, Callback)"},
		{"eligible-row", `flow Callback(entry: File.Entry) -> Bool { return true }`, "File.Tree.Remove.checked(tree, Callback)"},
		{"eligible-fallible", `flow Callback(row: Record{relative: String, entry: File.Entry}) -> Bool! { return true }`, "File.Tree.Remove.checked(tree, Callback)"},
		{"eligible-effect", `flow Recover(error: Error) -> Bool { return false }; flow Callback(row: Record{relative: String, entry: File.Entry}) -> Bool onError Recover { return Math.gt(Clock.now_unix_ns(), 0) }`, "File.Tree.Remove.checked(tree, Callback)"},
		{"eligible-context", `flow Callback(count: Int, row: Record{relative: String, entry: File.Entry}) -> Bool { return true }`, `File.Tree.Remove.checked(tree, "wrong", Callback)`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source := fixture.flow + `; flow Build(parent: File.Directory, root: File.Entry, tree: File.Tree) -> Bool! { target = ` + fixture.call + `; return true }; return 0`
			got := run(t, "check", sourceFile(t, source), "--format", "json")
			if got.status != 1 || !strings.Contains(got.stderr, `"code":"check.`) || strings.Contains(got.stderr, "unknown-flow") || strings.Contains(got.stderr, "unknown-type") {
				t.Fatalf("invalid callback did not fail at the typed contract: %+v", got)
			}
			if strings.HasSuffix(fixture.name, "-effect") && !strings.Contains(got.stderr, "check.effectful-callback") {
				t.Fatalf("callback effect did not produce its specific diagnostic: %+v", got)
			}
		})
	}
}

func TestF034SnapshotIsHostReadAndConstructorIsPure(t *testing.T) {
	read := f034Predicates + `flow Read(item: Record{parent: File.Directory, root: File.Entry}) -> File.Tree! { return File.Tree.snapshot(item.parent, item.root, Descend) }; flow Transform(items: List<Record{parent: File.Directory, root: File.Entry}>) -> List<File.Tree>! { return List.map(items, Read) }; return 0`
	got := run(t, "check", sourceFile(t, read), "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, "check.effectful-callback") {
		t.Fatalf("snapshot accepted in a pure list callback: %+v", got)
	}
	pure := f034Predicates + `flow Build(tree: File.Tree) -> File.Tree.Remove! { return File.Tree.Remove.checked(tree, Eligible) }; flow Transform(trees: List<File.Tree>) -> List<File.Tree.Remove>! { return List.map(trees, Build) }; return 0`
	got = run(t, "check", sourceFile(t, pure), "--format", "json")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("pure tree constructor rejected: %+v", got)
	}
}

func TestF034Inspection(t *testing.T) {
	parent := fileFixtureRoot(t)
	source := f034RemoveSource(parent, "", "")
	report, _, output := inspectSource(t, source)
	if len(report.Reads) != 3 || len(report.Activations) != 1 || len(report.Capabilities) != 4 {
		t.Fatalf("tree observations or activation missing: %s", output)
	}
	if report.Reads[2].Operation != "File.Tree.snapshot" || report.Reads[2].Target.Value != parent || report.Capabilities[2].Operation != "File.observe_tree" || report.Capabilities[3].Operation != "File.remove_tree" {
		t.Fatalf("tree authority metadata: %s", output)
	}
	want := map[string][]string{
		"File.Tree.snapshot":       {"File.DirectoryChanged", "File.DirectoryAbsent", "File.DirectoryFailed", "File.ListFailed", "File.InvalidName"},
		"File.Tree.Remove.checked": {"File.InvalidSelection"},
	}
	activation := false
	for _, source := range report.ErrorSources {
		if source.Category == "activation" {
			if source.Operation != "File.Tree.Remove.checked" || !slices.Equal(source.Kinds, []string{"File.EntryChanged", "File.RemoveFailed"}) {
				t.Fatalf("tree activation failure metadata: %s", output)
			}
			activation = true
			continue
		}
		if expected, ok := want[source.Operation]; ok {
			if !slices.Equal(source.Kinds, expected) {
				t.Fatalf("tree failure metadata for %s: %s", source.Operation, output)
			}
			delete(want, source.Operation)
		}
	}
	if len(want) != 0 || !activation {
		t.Fatalf("missing tree failure metadata %v: %s", want, output)
	}
}

func TestF034DynamicTargetInspection(t *testing.T) {
	source := `flow Apply(target: File.Tree.Remove) -> File.RemoveReceipt! { receipt = commit target; return receipt }`
	report, _, output := inspectSource(t, source)
	if len(report.Reads) != 0 || len(report.Activations) != 1 || len(report.Targets) != 1 {
		t.Fatalf("dynamic tree target metadata: %s", output)
	}
	if report.Targets[0].Operation != "File.Tree.Remove.checked" || report.Targets[0].Target.Kind != "dynamic" || report.Targets[0].Target.Value != "" {
		t.Fatalf("dynamic tree target invented observation authority: %s", output)
	}
}

func TestF034CumulativeRowsAndBounds(t *testing.T) {
	for _, shape := range []string{"wide", "deep", "pending-ancestors"} {
		t.Run(shape, func(t *testing.T) {
			parent := fileFixtureRoot(t)
			tree := f033Directory(t, parent, "tree")
			current, rows := tree, 1
			for index := 0; index < 160; index++ {
				name := "a"
				if shape == "wide" {
					current, name = tree, fmt.Sprintf("child-%03d", index)
				}
				child := f033Directory(t, current, name)
				rows++
				if shape == "pending-ancestors" {
					f033Directory(t, current, "z")
					rows++
				}
				current = child
			}
			source := sourceFile(t, f034Predicates+f034Program(parent, "Int", "", "return List.length(tree.entries)"))
			flags := []string{"--observe-tree-root", parent, "--max-steps", "10000000", "--max-live-bytes", "16777216", "--max-file-directories", "4", "--format", "json"}
			valueJSON(t, fileOutput(t, runProgram(t, source, "run", flags...)), fmt.Sprintf(`"%d"`, rows))
			limited := append(append([]string{}, flags...), "--max-file-entries", fmt.Sprint(rows-1))
			f034Failure(t, runProgram(t, source, "run", limited...), 2, "runtime.limit")
			allowed := append(append([]string{}, flags...), "--max-file-entries", fmt.Sprint(rows))
			valueJSON(t, fileOutput(t, runProgram(t, source, "run", allowed...)), fmt.Sprintf(`"%d"`, rows))
		})
	}
}
