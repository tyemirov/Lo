package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func f033Directory(t *testing.T, parent, name string) string {
	t.Helper()
	created, err := os.MkdirTemp(parent, "directory-")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(parent, name)
	if err := os.Rename(created, destination); err != nil {
		t.Fatal(err)
	}
	return destination
}

func f033Program(root, resultType, body string) string {
	return fmt.Sprintf(`flow Main() -> %s! { directory = File.directory(File.Path.new(%q)); %s }; return Main()`, resultType, root, body)
}

func TestF033RetainedObservationContracts(t *testing.T) {
	for _, source := range []string{
		`flow Read(directory: File.Directory) -> File.Scan! { return File.Directory.scan(directory) }; return 0`,
		`flow Read(directory: File.Directory) -> File.Directory! { return File.Directory.refresh(directory) }; return 0`,
		`flow Read(directory: File.Directory, entry: File.Entry) -> File.Directory! { return File.Directory.child(directory, entry) }; return 0`,
		`flow Read(directory: File.Directory) -> File.Entry! { return File.Directory.entry(directory, "child") }; return 0`,
		`flow Read(entry: File.Entry) -> List<File.Entry>! { return File.Entry.observe(entry) }; return 0`,
	} {
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 0 || got.stderr != "" {
			t.Fatalf("retained observation contract: %+v", got)
		}
	}
}

func TestF033RetainedObservationsAreHostReads(t *testing.T) {
	for _, test := range []struct{ input, output, expression string }{
		{"File.Directory", "File.Scan", "File.Directory.scan(value)"},
		{"File.Directory", "File.Directory", "File.Directory.refresh(value)"},
		{"File.Directory", "File.Entry", `File.Directory.entry(value, "child")`},
		{"Record{directory: File.Directory, entry: File.Entry}", "File.Directory", "File.Directory.child(value.directory, value.entry)"},
		{"File.Entry", "List<File.Entry>", "File.Entry.observe(value)"},
	} {
		source := fmt.Sprintf(`flow Read(value: %s) -> %s! { return %s }; flow Transform(value: %s) -> List<%s>! { return List.map(List.new(value), Read) }; return 0`, test.input, test.output, test.expression, test.input, test.output)
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, "check.effectful-callback") {
			t.Fatalf("retained host read accepted as pure callback: %+v", got)
		}
	}
}

func TestF033ExactRetainedObservations(t *testing.T) {
	root := fileFixtureRoot(t)
	fileFixture(t, root, "b", "second")
	fileFixture(t, root, "a", "first")
	source := `flow Name(entry: File.Entry) -> String { return entry.name }; ` + f033Program(root, "Record{names: List<String>, same: Bool, name: String, size: Int}", `
fresh = File.Directory.refresh(directory)
scan = File.Directory.scan(fresh)
entry = File.Directory.entry(directory, "a")
observed = File.Entry.observe(entry)
current = List.at(observed, 0)
return Record.new(names: List.map(scan.entries, Name), same: Math.eq(directory.inode, fresh.inode), name: current.name, size: current.size_bytes)`)
	got := fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--list-root", root, "--format", "json"))
	valueJSON(t, recordField(t, got, "names"), `[{"type":"String","value":"a"},{"type":"String","value":"b"}]`)
	valueJSON(t, recordField(t, got, "same"), "true")
	valueJSON(t, recordField(t, got, "name"), `"a"`)
	valueJSON(t, recordField(t, got, "size"), `"5"`)
}

func TestF033ExactGrantCannotDescend(t *testing.T) {
	root := fileFixtureRoot(t)
	child := f033Directory(t, root, "child")
	fileFixture(t, child, "inside", "contents")
	source := sourceFile(t, f033Program(root, "Int", `entry = File.Directory.entry(directory, "child"); child = File.Directory.child(directory, entry); scan = File.Directory.scan(child); return List.length(scan.entries)`))
	for _, flags := range [][]string{
		{"--list-root", root},
		{"--list-root", root, "--remove-root", root},
		{"--list-root", root, "--remove-tree-root", root},
		{"--remove-tree-root", root},
	} {
		got := runProgram(t, source, "run", flags...)
		if got.status != 2 || !strings.Contains(got.stderr, "runtime.capability-denied") {
			t.Fatalf("exact or removal grant authorized descendant observation: %+v", got)
		}
	}
	for _, flags := range [][]string{
		{"--observe-tree-root", root},
		{"--list-root", root, "--observe-tree-root", root},
	} {
		args := append(flags, "--format", "json")
		valueJSON(t, fileOutput(t, runProgram(t, source, "run", args...)), `"1"`)
	}
	for _, operation := range []string{"File.directory", "File.list", "File.scan"} {
		source := sourceFile(t, fmt.Sprintf("return %s(File.Path.new(%q))", operation, child))
		got := runProgram(t, source, "run", "--observe-tree-root", root)
		if got.status != 2 || !strings.Contains(got.stderr, "runtime.capability-denied") {
			t.Fatalf("recursive grant authorized string-path descent through %s: %+v", operation, got)
		}
	}
	usage := sourceFile(t, fmt.Sprintf(`return File.System.usage(File.Path.new(%q))`, root))
	got := runProgram(t, usage, "run", "--observe-tree-root", root)
	if got.status != 2 || !strings.Contains(got.stderr, "runtime.capability-denied") {
		t.Fatalf("recursive observation supplied volume authority: %+v", got)
	}
}

func TestF033RecursiveRemovalAuthority(t *testing.T) {
	for _, authority := range []string{"none", "exact", "exact-child", "recursive"} {
		t.Run(authority, func(t *testing.T) {
			root := fileFixtureRoot(t)
			child := f033Directory(t, root, "child")
			selected := fileFixture(t, child, "selected", "payload")
			source := sourceFile(t, f033Program(root, "Int", `entry = File.Directory.entry(directory, "child"); child = File.Directory.child(directory, entry); scan = File.Directory.scan(child); target = File.Remove.new(scan.entries); receipt = commit target; return receipt.completed_count`))
			recursive := authority == "recursive"
			authorized := recursive || authority == "exact-child"
			flags := []string{"--observe-tree-root", root, "--format", "json"}
			if recursive {
				flags = append(flags, "--remove-tree-root", root)
			} else if authority == "exact" {
				flags = append(flags, "--remove-root", root)
			} else if authority == "exact-child" {
				flags = append(flags, "--remove-root", child)
			}
			got := runProgram(t, source, "run", flags...)
			if authorized {
				valueJSON(t, fileOutput(t, got), `"1"`)
			} else if got.status != 2 || !strings.Contains(got.stderr, "runtime.capability-denied") {
				t.Fatalf("%s removal authority authorized descendant mutation: %+v", authority, got)
			}
			_, err := os.Lstat(selected)
			if authorized && !os.IsNotExist(err) || !authorized && err != nil {
				t.Fatalf("selected file state disagrees with removal authority: %v", err)
			}
		})
	}
}

func TestF033RecursiveRootAbsenceAndSymlinks(t *testing.T) {
	parent := fileFixtureRoot(t)
	missing := filepath.Join(parent, "missing")
	link := filepath.Join(parent, "link")
	if err := os.Symlink(fileFixtureRoot(t), link); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"File.directory", "File.list", "File.scan"} {
		t.Run(operation, func(t *testing.T) {
			source := sourceFile(t, fmt.Sprintf(`return %s(File.Path.new(%q))`, operation, missing))
			got := runProgram(t, source, "run", "--observe-tree-root", missing, "--format", "json")
			if got.status != 1 || !strings.Contains(got.stderr, "File.DirectoryAbsent") {
				t.Fatalf("recursive missing root: %+v", got)
			}
			source = sourceFile(t, fmt.Sprintf(`return %s(File.Path.new(%q))`, operation, link))
			got = runProgram(t, source, "run", "--observe-tree-root", link, "--format", "json")
			if got.status == 0 || strings.Contains(got.stdout, `"status":"ok"`) {
				t.Fatalf("recursive symlink root accepted: %+v", got)
			}
		})
	}
}

func TestF033EntryFreshnessAndAbsence(t *testing.T) {
	root := fileFixtureRoot(t)
	fileFixture(t, root, "entry", "old")
	source := sourceFile(t, f033Program(root, "Record{before: Int, after: Int, remaining: Int}", fmt.Sprintf(`
entry = File.Directory.entry(directory, "entry")
write = File.Write.new(File.Path.new(%q), Bytes.new("replacement"))
commit write
observed = File.Entry.observe(entry)
current = List.at(observed, 0)
target = File.Remove.new(observed)
commit target
absent = File.Entry.observe(current)
return Record.new(before: entry.size_bytes, after: current.size_bytes, remaining: List.length(absent))`, filepath.Join(root, "entry"))))
	got := fileOutput(t, runProgram(t, source, "run", "--list-root", root, "--write-root", root, "--remove-root", root, "--format", "json"))
	valueJSON(t, recordField(t, got, "before"), `"3"`)
	valueJSON(t, recordField(t, got, "after"), `"11"`)
	valueJSON(t, recordField(t, got, "remaining"), `"0"`)
	source = sourceFile(t, f033Program(root, "File.Entry", `return File.Directory.entry(directory, "entry")`))
	failed := runProgram(t, source, "run", "--list-root", root)
	if failed.status != 1 || !strings.Contains(failed.stderr, "File.EntryAbsent") {
		t.Fatalf("strict entry absence: %+v", failed)
	}
}

func TestF033EntryNamesAndSymlinks(t *testing.T) {
	root := fileFixtureRoot(t)
	for _, name := range []string{"", ".", "..", "child/entry", "/entry", "nul\x00name"} {
		source := sourceFile(t, f033Program(root, "File.Entry", fmt.Sprintf("return File.Directory.entry(directory, %s)", processLiteral(t, name))))
		got := runProgram(t, source, "run", "--list-root", root)
		if got.status != 1 || !strings.Contains(got.stderr, "File.InvalidName") {
			t.Fatalf("invalid basename %q: %+v", name, got)
		}
	}
	outside := fileFixtureRoot(t)
	fileFixture(t, outside, "preserved", "outside")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	source := sourceFile(t, f033Program(root, "String", `entry = File.Directory.entry(directory, "link"); observed = File.Entry.observe(entry); current = List.at(observed, 0); return current.kind`))
	valueJSON(t, fileOutput(t, runProgram(t, source, "run", "--list-root", root, "--format", "json")), `"symlink"`)
	source = sourceFile(t, f033Program(root, "File.Directory", `entry = File.Directory.entry(directory, "link"); return File.Directory.child(directory, entry)`))
	got := runProgram(t, source, "run", "--observe-tree-root", root)
	if got.status != 1 || !strings.Contains(got.stderr, "File.DirectoryFailed") {
		t.Fatalf("symlink child traversal: %+v", got)
	}
}

func TestF033MismatchedParent(t *testing.T) {
	root := fileFixtureRoot(t)
	f033Directory(t, root, "left")
	f033Directory(t, root, "right")
	source := sourceFile(t, f033Program(root, "File.Directory", `left = File.Directory.entry(directory, "left"); right = File.Directory.entry(directory, "right"); child = File.Directory.child(directory, right); return File.Directory.child(child, left)`))
	got := runProgram(t, source, "run", "--observe-tree-root", root)
	if got.status != 2 || !strings.Contains(got.stderr, "runtime.capability-denied") {
		t.Fatalf("mismatched parent accepted: %+v", got)
	}
}

func f033WalkSource(root string) string {
	return `flow Item(parent: File.Directory, entry: File.Entry) -> Record{parent: File.Directory, entry: File.Entry} { return Record.new(parent: parent, entry: entry) }
flow Visit(count: Int, item: Record{parent: File.Directory, entry: File.Entry}) -> Record{state: Int, items: List<Record{parent: File.Directory, entry: File.Entry}>, stop: Bool}! {
directory = File.Directory.child(item.parent, item.entry)
scan = File.Directory.scan(directory)
return Record.new(state: Math.add(count, 1), items: List.map(scan.entries, directory, Item), stop: false)
}
` + f033Program(root, "Int", `scan = File.Directory.scan(directory); return Flow.worklist(List.map(scan.entries, directory, Item), 1, Visit)`)
}

func TestF033BoundedRetainedTraversal(t *testing.T) {
	for _, shape := range []string{"wide", "deep", "pending-ancestors"} {
		t.Run(shape, func(t *testing.T) {
			root := fileFixtureRoot(t)
			parent, count := root, 1
			for index := 0; index < 160; index++ {
				name := "a"
				if shape == "wide" {
					parent, name = root, fmt.Sprintf("child-%03d", index)
				}
				child := f033Directory(t, parent, name)
				count++
				if shape == "pending-ancestors" {
					f033Directory(t, parent, "z")
					count++
				}
				parent = child
			}
			source := sourceFile(t, f033WalkSource(root))
			got := fileOutput(t, runProgram(t, source, "run", "--observe-tree-root", root, "--max-steps", "10000000", "--max-live-bytes", "8388608", "--format", "json"))
			valueJSON(t, got, fmt.Sprintf(`"%d"`, count))
		})
	}
}
