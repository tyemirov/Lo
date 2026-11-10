package integration_test

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const f034RowType = "Record{relative: String, entry: File.Entry}"
const f034Predicates = `flow Descend(entry: File.Entry) -> Bool { return true }
flow Eligible(row: Record{relative: String, entry: File.Entry}) -> Bool { return true }
`

func f034Program(parent, resultType, before, after string) string {
	return fmt.Sprintf(`flow Main() -> %s! {
%s
parent = File.directory(File.Path.new(%q))
root = File.Directory.entry(parent, "tree")
tree = File.Tree.snapshot(parent, root, Descend)
%s
}; return Main()`, resultType, before, parent, after)
}

func f034RemoveSource(parent, before, after string) string {
	return f034Predicates + f034Program(parent, "File.RemoveReceipt", before, after+`
target = File.Tree.Remove.checked(tree, Eligible)
receipt = commit target
return receipt`)
}

func f034Failure(t *testing.T, got result, status int, kind string) {
	t.Helper()
	if got.status != status || !strings.Contains(got.stderr, kind) || strings.Contains(got.stderr, `"code":"check.`) {
		t.Fatalf("expected runtime status %d and %s: %+v", status, kind, got)
	}
}

func TestF034SnapshotRowsAndDescent(t *testing.T) {
	for _, rejected := range []string{"none", "b", "tree"} {
		t.Run(rejected, func(t *testing.T) {
			parent := fileFixtureRoot(t)
			tree := f033Directory(t, parent, "tree")
			fileFixture(t, tree, "a", "leaf")
			branch := f033Directory(t, tree, "b")
			fileFixture(t, branch, "inner", "nested")
			outside := fileFixtureRoot(t)
			preserved := fileFixture(t, outside, "preserved", "outside")
			if err := os.Symlink(outside, filepath.Join(tree, "c-link")); err != nil {
				t.Fatal(err)
			}
			f033Directory(t, tree, "d-empty")
			source := fmt.Sprintf(`flow Descend(rejected: String, entry: File.Entry) -> Bool { return Match.new(String.eq(entry.kind, "directory"), false: false, _: Match.new(String.eq(entry.name, rejected), true: false, _: true)) }
flow Row(row: %s) -> Record{relative: String, kind: String} { return Record.new(relative: row.relative, kind: row.entry.kind) }
flow Main() -> Record{complete: Bool, rows: List<Record{relative: String, kind: String}>}! {
parent = File.directory(File.Path.new(%q))
root = File.Directory.entry(parent, "tree")
tree = File.Tree.snapshot(parent, root, %q, Descend)
return Record.new(complete: tree.complete, rows: List.map(tree.entries, Row))
}; return Main()`, f034RowType, parent, rejected)
			value := fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--observe-tree-root", parent, "--format", "json"))
			valueJSON(t, recordField(t, value, "complete"), fmt.Sprint(rejected == "none"))
			var rows []runtimeValue
			if err := json.Unmarshal(recordField(t, value, "rows").Value, &rows); err != nil {
				t.Fatal(err)
			}
			want := [][2]string{{".", "directory"}, {"a", "regular"}, {"b", "directory"}, {"b/inner", "regular"}, {"c-link", "symlink"}, {"d-empty", "directory"}}
			if rejected == "b" {
				want = append(want[:3], want[4:]...)
			} else if rejected == "tree" {
				want = want[:1]
			}
			if len(rows) != len(want) {
				t.Fatalf("snapshot row count: %d, expected %d", len(rows), len(want))
			}
			for index, expected := range want {
				valueJSON(t, recordField(t, rows[index], "relative"), fmt.Sprintf("%q", expected[0]))
				valueJSON(t, recordField(t, rows[index], "kind"), fmt.Sprintf("%q", expected[1]))
			}
			macCleanupPresent(t, preserved, true)
		})
	}
}

func TestF034IncompleteAndPureConstruction(t *testing.T) {
	for _, rejected := range []string{"tree", "empty", "none"} {
		t.Run(rejected, func(t *testing.T) {
			parent := fileFixtureRoot(t)
			tree := f033Directory(t, parent, "tree")
			f033Directory(t, tree, "empty")
			source := fmt.Sprintf(`flow Descend(entry: File.Entry) -> Bool { return Match.new(String.eq(entry.name, %q), true: false, _: true) }
flow Eligible(row: %s) -> Bool { return false }
`, rejected, f034RowType) + f034Program(parent, "Bool", "", `target = File.Tree.Remove.checked(tree, Eligible); return true`)
			got := runProgram(t, sourceFile(t, source), "run", "--observe-tree-root", parent, "--format", "json")
			if rejected == "none" {
				valueJSON(t, fileOutput(t, got), "true")
			} else {
				f034Failure(t, got, 1, "File.InvalidSelection")
			}
			macCleanupPresent(t, tree, true)
		})
	}
}

func TestF034RemovalAuthority(t *testing.T) {
	for _, grant := range []string{"none", "exact-parent", "exact-root", "recursive-parent", "recursive-root", "recursive-child"} {
		t.Run(grant, func(t *testing.T) {
			parent := fileFixtureRoot(t)
			tree := f033Directory(t, parent, "tree")
			child := f033Directory(t, tree, "child")
			leaf := fileFixture(t, child, "leaf", "contents")
			sibling := fileFixture(t, parent, "sibling", "outside selection")
			flags := []string{"--observe-tree-root", parent, "--format", "json"}
			switch grant {
			case "exact-parent":
				flags = append(flags, "--remove-root", parent)
			case "exact-root":
				flags = append(flags, "--remove-root", tree)
			case "recursive-parent":
				flags = append(flags, "--remove-tree-root", parent)
			case "recursive-root":
				flags = append(flags, "--remove-tree-root", tree)
			case "recursive-child":
				flags = append(flags, "--remove-tree-root", child)
			}
			got := runProgram(t, sourceFile(t, f034RemoveSource(parent, "", "")), "run", flags...)
			allowed := grant == "recursive-parent" || grant == "recursive-root"
			if allowed {
				valueJSON(t, recordField(t, fileOutput(t, got), "completed_count"), `"3"`)
			} else {
				f034Failure(t, got, 2, "runtime.capability-denied")
			}
			macCleanupPresent(t, tree, !allowed)
			macCleanupPresent(t, leaf, !allowed)
			macCleanupPresent(t, sibling, true)
		})
	}
}

func TestF034SnapshotRequiresRecursiveObservation(t *testing.T) {
	parent := fileFixtureRoot(t)
	f033Directory(t, parent, "tree")
	source := sourceFile(t, f034Predicates+f034Program(parent, "File.Tree", "", "return tree"))
	for _, flags := range [][]string{{"--list-root", parent}, {"--list-root", parent, "--remove-tree-root", parent}} {
		f034Failure(t, runProgram(t, source, "run", flags...), 2, "runtime.capability-denied")
	}
}

func TestF034RowsHaveNoAuthority(t *testing.T) {
	parent := fileFixtureRoot(t)
	tree := f033Directory(t, parent, "tree")
	fileFixture(t, tree, "leaf", "contents")
	for _, test := range []struct {
		name, resultType, body, failure string
		status                          int
	}{
		{"observe", "List<File.Entry>", `row = List.at(tree.entries, 1); return File.Entry.observe(row.entry)`, "runtime.capability-denied", 2},
		{"direct-removal", "File.Remove", `row = List.at(tree.entries, 1); return File.Remove.new(List.new(row.entry))`, "File.InvalidSelection", 1},
		{"child", "File.Directory", `row = List.at(tree.entries, 0); return File.Directory.child(parent, row.entry)`, "runtime.capability-denied", 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := f034Predicates + f034Program(parent, test.resultType, "", test.body)
			got := runProgram(t, sourceFile(t, source), "run", "--observe-tree-root", parent, "--remove-tree-root", parent, "--format", "json")
			f034Failure(t, got, test.status, test.failure)
			macCleanupPresent(t, tree, true)
		})
	}
}

func TestF034CompleteEligibilityPrecedesRemoval(t *testing.T) {
	parent := fileFixtureRoot(t)
	tree := f033Directory(t, parent, "tree")
	first := fileFixture(t, tree, "a", "first")
	last := fileFixture(t, tree, "z", "last")
	source := `flow Descend(entry: File.Entry) -> Bool { return true }
flow Eligible(rejected: String, row: Record{relative: String, entry: File.Entry}) -> Bool { return Match.new(String.eq(row.relative, rejected), true: false, _: true) }
` + f034Program(parent, "File.RemoveReceipt", "", `target = File.Tree.Remove.checked(tree, "z", Eligible); receipt = commit target; return receipt`)
	got := runProgram(t, sourceFile(t, source), "run", "--observe-tree-root", parent, "--remove-tree-root", tree, "--format", "json")
	f034Failure(t, got, 1, "File.EntryChanged")
	f034Progress(t, got.stderr, 0, 3, 2, "z", "preflight", "File.EntryChanged")
	if !strings.Contains(got.stderr, "predicate-false") {
		t.Fatalf("eligibility failure lost condition: %+v", got)
	}
	macCleanupPresent(t, first, true)
	macCleanupPresent(t, last, true)
}

func TestF034ExpectedTopologyAndMetadata(t *testing.T) {
	for _, change := range []string{"add", "replace", "append", "remove"} {
		t.Run(change, func(t *testing.T) {
			parent := fileFixtureRoot(t)
			tree := f033Directory(t, parent, "tree")
			first := fileFixture(t, tree, "a", "first")
			last := fileFixture(t, tree, "z", "last")
			mutation := fmt.Sprintf(`write = File.Write.new(File.Path.new(%q), Bytes.new("replacement")); commit write`, last)
			if change == "add" {
				mutation = fmt.Sprintf(`write = File.Write.new(File.Path.new(%q), Bytes.new("addition")); commit write`, filepath.Join(tree, "new"))
			} else if change == "append" {
				mutation = fmt.Sprintf(`write = File.Append.new(File.Path.new(%q), Bytes.new("addition")); commit write`, last)
			} else if change == "remove" {
				mutation = `child = File.Directory.child(parent, root); entry = File.Directory.entry(child, "z"); removal = File.Remove.new(List.new(entry)); commit removal`
			}
			got := runProgram(t, sourceFile(t, f034RemoveSource(parent, "", mutation)), "run", "--observe-tree-root", parent, "--remove-tree-root", parent, "--write-root", tree, "--format", "json")
			f034Failure(t, got, 1, "File.EntryChanged")
			failedIndex := -2
			if change == "add" || change == "remove" {
				failedIndex = -1
			}
			fields := f034Progress(t, got.stderr, 0, 3, failedIndex, "", "preflight", "File.EntryChanged")
			valueJSON(t, fields["completed_bytes"], `"0"`)
			macCleanupPresent(t, first, true)
			macCleanupPresent(t, last, change != "remove")
		})
	}
}

func TestF034CheckedRemovalRetainsTensorContext(t *testing.T) {
	parent := fileFixtureRoot(t)
	tree := f033Directory(t, parent, "tree")
	leaf := fileFixture(t, tree, "leaf", "payload")
	source := `flow Descend(entry: File.Entry) -> Bool { return true }
flow Reject(error: Error) -> Bool { return false }
flow Eligible(tensor: Tensor<FP32>, row: Record{relative: String, entry: File.Entry}) -> Bool onError Reject {
values = Tensor.values(tensor)
return Math.eq(List.length(values), 1)
}
flow Build(tree: File.Tree) -> File.Tree.Remove! {
tensor = Tensor.new(List.new(1.0), List.new(1))
return File.Tree.Remove.checked(tree, tensor, Eligible)
}
` + f034Program(parent, "Int", "", `target = Build(tree); receipt = commit target; return receipt.completed_count`)
	got := fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--observe-tree-root", parent, "--remove-tree-root", tree, "--format", "json"))
	valueJSON(t, got, `"2"`)
	macCleanupPresent(t, leaf, false)
	macCleanupPresent(t, tree, false)
}

func TestF034HardlinksSymlinksAndOtherKinds(t *testing.T) {
	parent := fileFixtureRoot(t)
	tree := f033Directory(t, parent, "tree")
	first := fileFixture(t, tree, "a", "shared")
	if err := os.Link(first, filepath.Join(tree, "b")); err != nil {
		t.Fatal(err)
	}
	branch := f033Directory(t, tree, "c-branch")
	branchAlias := filepath.Join(branch, "alias")
	if err := os.Link(first, branchAlias); err != nil {
		t.Fatal(err)
	}
	outside := fileFixtureRoot(t)
	preserved := filepath.Join(outside, "alias")
	if err := os.Link(first, preserved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(tree, "c-link")); err != nil {
		t.Fatal(err)
	}
	socketRoot, err := os.MkdirTemp(filepath.Join(repository, "_scratch"), "f034-socket-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketRoot) })
	// Bind under a short repository path, then move the directory entry into the tree.
	socketPath := filepath.Join(socketRoot, "socket")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Rename(socketPath, filepath.Join(tree, "d-socket")); err != nil {
		t.Fatal(err)
	}
	paths := []string{tree, first, filepath.Join(tree, "b"), branch, branchAlias, filepath.Join(tree, "c-link"), filepath.Join(tree, "d-socket")}
	var expectedBytes int64
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		expectedBytes += info.Size()
	}
	got := fileOutput(t, runProgram(t, sourceFile(t, f034RemoveSource(parent, "", "")), "run", "--observe-tree-root", parent, "--remove-tree-root", tree, "--format", "json"))
	valueJSON(t, recordField(t, got, "total_count"), `"7"`)
	valueJSON(t, recordField(t, got, "completed_count"), `"7"`)
	valueJSON(t, recordField(t, got, "observed_bytes"), fmt.Sprintf(`"%d"`, expectedBytes))
	macCleanupPresent(t, tree, false)
	remaining, err := os.ReadFile(preserved)
	if err != nil || string(remaining) != "shared" {
		t.Fatalf("outside hardlink changed: %q, %v", remaining, err)
	}
}

func TestF034EscapingObservationAndTarget(t *testing.T) {
	for _, escape := range []string{"tree", "target"} {
		t.Run(escape, func(t *testing.T) {
			parent := fileFixtureRoot(t)
			tree := f033Directory(t, parent, "tree")
			fileFixture(t, tree, "leaf", "contents")
			source := f034EscapingSource(parent, escape)
			got := fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--observe-tree-root", parent, "--remove-tree-root", tree, "--max-file-directories", "4", "--format", "json"))
			valueJSON(t, recordField(t, got, "completed_count"), `"2"`)
			macCleanupPresent(t, tree, false)
		})
	}
}

func f034EscapingSource(parent, escape string) string {
	observe := fmt.Sprintf(`flow Observe() -> File.Tree! { parent = File.directory(File.Path.new(%q)); root = File.Directory.entry(parent, "tree"); return File.Tree.snapshot(parent, root, Descend) }
flow Build() -> File.Tree.Remove! { tree = Observe(); return File.Tree.Remove.checked(tree, Eligible) }
`, parent)
	body := `tree = Observe(); target = File.Tree.Remove.checked(tree, Eligible)`
	if escape == "target" {
		body = `target = Build()`
	}
	return f034Predicates + observe + `flow Main() -> File.RemoveReceipt! { ` + body + `; receipt = commit target; return receipt }; return Main()`
}

func TestF034SnapshotRootProvenance(t *testing.T) {
	for _, kind := range []string{"regular", "symlink", "wrong-parent"} {
		t.Run(kind, func(t *testing.T) {
			parent := fileFixtureRoot(t)
			if kind == "regular" {
				fileFixture(t, parent, "tree", "not a directory")
			} else if kind == "symlink" {
				if err := os.Symlink(fileFixtureRoot(t), filepath.Join(parent, "tree")); err != nil {
					t.Fatal(err)
				}
			} else {
				f033Directory(t, parent, "tree")
				f033Directory(t, parent, "other")
			}
			selectedParent := "parent"
			selection := ""
			if kind == "wrong-parent" {
				selectedParent = "other"
				selection = `entry = File.Directory.entry(parent, "other"); other = File.Directory.child(parent, entry); `
			}
			source := f034Predicates + fmt.Sprintf(`flow Main() -> File.Tree! { parent = File.directory(File.Path.new(%q)); root = File.Directory.entry(parent, "tree"); %sreturn File.Tree.snapshot(%s, root, Descend) }; return Main()`, parent, selection, selectedParent)
			got := runProgram(t, sourceFile(t, source), "run", "--observe-tree-root", parent, "--format", "json")
			if kind == "wrong-parent" {
				f034Failure(t, got, 2, "runtime.capability-denied")
			} else {
				f034Failure(t, got, 1, "File.DirectoryFailed")
			}
		})
	}
}

func TestF034ProtectedResourcePreflight(t *testing.T) {
	for _, protected := range []string{"program-entry", "active-lock"} {
		t.Run(protected, func(t *testing.T) {
			parent := fileFixtureRoot(t)
			tree := f033Directory(t, parent, "tree")
			first := fileFixture(t, tree, "a-candidate", "preserve before protected row")
			artifact := filepath.Join(parent, "program")
			before := ""
			flags := []string{"run", "--observe-tree-root", parent, "--remove-tree-root", tree, "--format", "json"}
			protectedPath := artifact
			if protected == "active-lock" {
				protectedPath = fileFixture(t, parent, "lock", "lock bytes")
				before = fmt.Sprintf(`lock = Lock.File.new(File.Path.new(%q)); lease = commit lock`, protectedPath)
				flags = append(flags, "--lock-file", protectedPath)
			}
			source := sourceFile(t, f034RemoveSource(parent, before, ""))
			built := run(t, "build", source, "--output", artifact, "--format", "json")
			if built.status != 0 || built.stderr != "" {
				t.Fatalf("build protected tree fixture: %+v", built)
			}
			alias := filepath.Join(tree, "z-protected")
			if err := os.Link(protectedPath, alias); err != nil {
				t.Fatal(err)
			}
			got := runExecutable(t, artifact, flags...)
			f034Failure(t, got, 2, "runtime.capability-denied")
			f034Progress(t, got.stderr, 0, 3, 2, "z-protected", "preflight", "")
			macCleanupPresent(t, first, true)
			macCleanupPresent(t, alias, true)
		})
	}
}
