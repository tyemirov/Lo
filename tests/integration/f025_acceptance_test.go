package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestF025ProjectionContracts(t *testing.T) {
	for _, fixture := range []struct{ name, source string }{
		{"entry", `flow Project(entry: File.Entry) -> Record{owner: Int, accessed: Int, modified: Int, changed: Int} { return Record.new(owner: entry.owner_uid, accessed: entry.accessed_unix_ns, modified: entry.modified_unix_ns, changed: entry.changed_unix_ns) }; return 0`},
		{"directory", `flow Project(directory: File.Directory) -> Record{path: File.Path, owner: Int, accessed: Int, modified: Int, changed: Int} { return Record.new(path: directory.path, owner: directory.owner_uid, accessed: directory.accessed_unix_ns, modified: directory.modified_unix_ns, changed: directory.changed_unix_ns) }; return 0`},
		{"scan", `flow Project(scan: File.Scan) -> Record{directory: File.Directory, entries: List<File.Entry>, vanished: List<String>} { return Record.new(directory: scan.directory, entries: scan.entries, vanished: scan.vanished_names) }; return 0`},
		{"invocation", `flow Project(initial: Host.Invocation) -> Record{arguments: List<String>, home: File.Path, real: Int, effective: Int, now: Int} { return Record.new(arguments: initial.arguments, home: initial.home, real: initial.real_uid, effective: initial.effective_uid, now: initial.now_unix_ns) }; return 0`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			got := run(t, "check", sourceFile(t, fixture.source), "--format", "json")
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("pure projection contract: %+v", got)
			}
		})
	}
}

func TestF025GuardStaticContracts(t *testing.T) {
	valid := `flow Accept(expected: Int, original: File.Entry, current: File.Entry) -> Bool { return Math.eq(expected, current.owner_uid) }; flow Main() -> Int! { unused = File.Remove.checked(List.new(), 1, Accept); return 0 }; return Main()`
	got := run(t, "check", sourceFile(t, valid), "--format", "json")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("valid empty guarded target: %+v", got)
	}
	for _, fixture := range []struct{ name, declaration, arguments string }{
		{"missing-current", `flow Guard(original: File.Entry) -> Bool { return true }`, `Guard`},
		{"wrong-original", `flow Guard(original: Int, current: File.Entry) -> Bool { return true }`, `Guard`},
		{"wrong-current", `flow Guard(original: File.Entry, current: Int) -> Bool { return true }`, `Guard`},
		{"wrong-context", `flow Guard(expected: Int, original: File.Entry, current: File.Entry) -> Bool { return true }`, `"wrong", Guard`},
		{"non-bool", `flow Guard(original: File.Entry, current: File.Entry) -> Int { return 1 }`, `Guard`},
		{"fallible-bool", `flow Guard(original: File.Entry, current: File.Entry) -> Bool! { return true }`, `Guard`},
		{"transitive-read", `flow Failed(error: Error) -> Bool { return false }; flow Observe() -> Bool onError Failed { data = File.read(File.Path.new("must-not-read")); return true }; flow Guard(original: File.Entry, current: File.Entry) -> Bool { return Observe() }`, `Guard`},
		{"transitive-commit", `flow Failed(error: Error) -> Bool { return false }; flow Activate() -> Bool onError Failed { target = File.Write.new(File.Path.new("must-not-write"), Bytes.new("x")); commit target; return true }; flow Guard(original: File.Entry, current: File.Entry) -> Bool { return Activate() }`, `Guard`},
		{"cached-host-read", `flow Failed(error: Error) -> Bool { return false }; flow Observe() -> Bool onError Failed { initial = Host.Invocation.current(); return Math.eq(initial.real_uid, initial.effective_uid) }; flow Guard(original: File.Entry, current: File.Entry) -> Bool { return Observe() }`, `Guard`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source := fixture.declaration + `; flow Main() -> Int! { unused = File.Remove.checked(List.new(), ` + fixture.arguments + `); return 0 }; return Main()`
			got := run(t, "check", sourceFile(t, source), "--format", "json")
			if got.status != 1 || diagnostics(t, got.stderr).Diagnostics[0].Stage != "check" {
				t.Fatalf("invalid empty/unused removal guard accepted: %+v", got)
			}
		})
	}
}

func TestF025CheckedConstructionIsPure(t *testing.T) {
	source := `flow Accept(expected: Int, original: File.Entry, current: File.Entry) -> Bool { return Math.eq(expected, current.owner_uid) }; flow Build(number: Int) -> File.Remove! { return File.Remove.checked(List.new(), number, Accept) }; return List.map(List.new(7), Build)`
	got := fileOutput(t, run(t, "run", sourceFile(t, source), "--format", "json"))
	if got.Type != "List<File.Remove>" {
		t.Fatalf("pure constructor result: %+v", got)
	}
}

func TestF025FileObservationPurity(t *testing.T) {
	for _, fixture := range []struct{ operation, result string }{
		{"File.directory", "File.Directory"},
		{"File.scan", "File.Scan"},
	} {
		source := fmt.Sprintf(`flow Observe(path: File.Path) -> %s! { return %s(path) }; return List.map(List.new(File.Path.new("unobserved")), Observe)`, fixture.result, fixture.operation)
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 1 || diagnostics(t, got.stderr).Diagnostics[0].Code != "check.effectful-callback" {
			t.Fatalf("observation permitted in pure callback: %+v", got)
		}
	}
}

func TestF025ScanAndDirectoryAuthority(t *testing.T) {
	for _, operation := range []string{"File.directory", "File.scan"} {
		t.Run(operation, func(t *testing.T) {
			root := fileFixtureRoot(t)
			source := sourceFile(t, fmt.Sprintf(`return %s(File.Path.new(%q))`, operation, root))
			denied := run(t, "run", source, "--format", "json")
			if denied.status != 2 || diagnostics(t, denied.stderr).Diagnostics[0].Code != "runtime.capability-denied" {
				t.Fatalf("missing exact listing grant: %+v", denied)
			}
			readOnly := run(t, "run", source, "--read-root", root, "--format", "json")
			if readOnly.status != 2 || diagnostics(t, readOnly.stderr).Diagnostics[0].Code != "runtime.capability-denied" {
				t.Fatalf("read grant supplied listing authority: %+v", readOnly)
			}
			child, err := os.MkdirTemp(root, "child-")
			if err != nil {
				t.Fatal(err)
			}
			childSource := sourceFile(t, fmt.Sprintf(`return %s(File.Path.new(%q))`, operation, child))
			parentOnly := run(t, "run", childSource, "--list-root", root, "--format", "json")
			if parentOnly.status != 2 || diagnostics(t, parentOnly.stderr).Diagnostics[0].Code != "runtime.capability-denied" {
				t.Fatalf("parent grant supplied child-root authority: %+v", parentOnly)
			}
		})
	}
}

func TestF025ObservationFailures(t *testing.T) {
	for _, operation := range []string{"File.directory", "File.scan"} {
		t.Run(operation, func(t *testing.T) {
			root := fileFixtureRoot(t)
			missing := filepath.Join(root, "missing")
			source := sourceFile(t, fmt.Sprintf(`return %s(File.Path.new(%q))`, operation, missing))
			got := run(t, "run", source, "--list-root", missing, "--format", "json")
			diagnostic := receiptHostDiagnostic(t, got)
			if got.status != 1 || diagnostic.Error == nil || diagnostic.Error.Kind != "File.DirectoryAbsent" {
				t.Fatalf("missing-directory error: %+v", got)
			}
			link := filepath.Join(root, "linked-root")
			if err := os.Symlink(fileFixtureRoot(t), link); err != nil {
				t.Fatal(err)
			}
			linked := run(t, "run", sourceFile(t, fmt.Sprintf(`return %s(File.Path.new(%q))`, operation, link)), "--list-root", link, "--format", "json")
			if linked.status == 0 {
				t.Fatalf("symlinked root accepted: %+v", linked)
			}
		})
	}
}

func TestF025ScanOrdering(t *testing.T) {
	root := fileFixtureRoot(t)
	for _, name := range []string{"z", "é", ".hidden", "a"} {
		fileFixture(t, root, name, "payload")
	}
	source := fmt.Sprintf(`flow Name(entry: File.Entry) -> String { return entry.name }; flow Main() -> Record{names: List<String>, vanished: List<String>, owner: Int}! { scan = File.scan(File.Path.new(%q)); return Record.new(names: List.map(scan.entries, Name), vanished: scan.vanished_names, owner: scan.directory.owner_uid) }; return Main()`, root)
	got := fileOutput(t, run(t, "run", sourceFile(t, source), "--list-root", root, "--format", "json"))
	fields := processFields(t, got)
	valueJSON(t, fields["names"], `[{"type":"String","value":".hidden"},{"type":"String","value":"a"},{"type":"String","value":"z"},{"type":"String","value":"é"}]`)
	valueJSON(t, fields["vanished"], `[]`)
	valueJSON(t, fields["owner"], fmt.Sprintf(`"%d"`, os.Getuid()))
}

func TestF025GuardRejectionPreservesFiles(t *testing.T) {
	root := fileFixtureRoot(t)
	selected := fileFixture(t, root, "selected", "payload")
	source := fmt.Sprintf(`flow Reject(original: File.Entry, current: File.Entry) -> Bool { return false }; flow Main() -> Int! { entries = File.list(File.Path.new(%q)); target = File.Remove.checked(entries, Reject); receipt = commit target; return receipt.completed_count }; return Main()`, root)
	got := run(t, "run", sourceFile(t, source), "--list-root", root, "--remove-root", root, "--format", "json")
	diagnostic := receiptHostDiagnostic(t, got)
	if got.status != 1 || diagnostic.Error == nil || diagnostic.Error.Kind != "File.EntryChanged" {
		t.Fatalf("guard rejection: %+v", got)
	}
	if !strings.Contains(got.stderr, "predicate-false") {
		t.Fatalf("guard rejection condition missing: %s", got.stderr)
	}
	data, err := os.ReadFile(selected)
	if err != nil || string(data) != "payload" {
		t.Fatalf("guard rejection changed selected file: %q %v", data, err)
	}
}

func TestF025CheckedSelectionValidation(t *testing.T) {
	root := fileFixtureRoot(t)
	selected := fileFixture(t, root, "selected", "payload")
	source := fmt.Sprintf(`flow Accepted(original: File.Entry, current: File.Entry) -> Bool { return true }; flow Duplicate(entry: File.Entry) -> List<File.Entry> { return List.new(entry, entry) }; flow Failed(error: Error) -> String { return error.kind }; flow Main() -> String onError Failed { entries = File.list(File.Path.new(%q)); duplicated = List.flat_map(entries, Duplicate); target = File.Remove.checked(duplicated, Accepted); return "accepted" }; return Main()`, root)
	valueJSON(t, fileOutput(t, run(t, "run", sourceFile(t, source), "--list-root", root, "--format", "json")), `"File.InvalidSelection"`)
	data, err := os.ReadFile(selected)
	if err != nil || string(data) != "payload" {
		t.Fatalf("invalid selection changed the observed file: %q %v", data, err)
	}
}

func TestF025GuardWorkExhaustion(t *testing.T) {
	root := fileFixtureRoot(t)
	selected := fileFixture(t, root, "selected", "payload")
	numbers := strings.TrimSuffix(strings.Repeat("1,", 300), ",")
	source := fmt.Sprintf(`flow Add(total: Int, number: Int) -> Int { return Math.add(total, number) }; flow Eligible(expected: Int, name: String, original: File.Entry, current: File.Entry) -> Bool { numbers = List.new(%s); total = List.fold(numbers, 0, Add); return Match.new(String.eq(name, current.name), true: Math.eq(total, expected), _: false) }; flow Failed(error: Error) -> Int { return -1 }; flow Main() -> Int onError Failed { entries = File.list(File.Path.new(%q)); target = File.Remove.checked(entries, 300, "selected", Eligible); receipt = commit target; return receipt.completed_count }; return Main()`, numbers, root)
	path := sourceFile(t, source)
	limited := run(t, "run", path, "--list-root", root, "--remove-root", root, "--max-steps", "100", "--format", "json")
	if limited.status != 2 || diagnostics(t, limited.stderr).Diagnostics[0].Code != "runtime.limit" {
		t.Fatalf("predicate work limit did not bypass the Lo handler: %+v", limited)
	}
	data, err := os.ReadFile(selected)
	if err != nil || string(data) != "payload" {
		t.Fatalf("predicate work exhaustion changed the file: %q %v", data, err)
	}
	valueJSON(t, fileOutput(t, run(t, "run", path, "--list-root", root, "--remove-root", root, "--max-steps", "10000", "--format", "json")), `"1"`)
	if _, err := os.Lstat(selected); !os.IsNotExist(err) {
		t.Fatalf("guard did not complete with sufficient work: %v", err)
	}
}

func TestF025NominalObservationValues(t *testing.T) {
	for _, source := range []string{
		`flow Accept(directory: File.Directory) -> Int { return directory.owner_uid }; return Accept(Record.new(path: File.Path.new("root"), owner_uid: 1, accessed_unix_ns: 0, modified_unix_ns: 0, changed_unix_ns: 0))`,
		`flow EmptyEntries() -> List<File.Entry> { return List.new() }; flow Accept(scan: File.Scan) -> Int { return List.length(scan.vanished_names) }; flow Main() -> Int! { directory = File.directory(File.Path.new("root")); return Accept(Record.new(directory: directory, entries: EmptyEntries(), vanished_names: List.new("x"))) }; return Main()`,
		`flow Accept(initial: Host.Invocation) -> Int { return initial.real_uid }; return Accept(Record.new(arguments: List.new("argument"), home: File.Path.new("root"), real_uid: 1, effective_uid: 1, now_unix_ns: 0))`,
		`flow Main() -> Bytes! { directory = File.directory(File.Path.new("root")); return Checkpoint.encode(directory) }; return Main()`,
		`flow Main() -> Bytes! { scan = File.scan(File.Path.new("root")); return Checkpoint.encode(scan) }; return Main()`,
		`flow Main() -> Bytes! { initial = Host.Invocation.current(); return Checkpoint.encode(initial) }; return Main()`,
		`flow Main() -> File.Remove! { directory = File.directory(File.Path.new("root")); return File.Remove.new(List.new(directory)) }; return Main()`,
		`flow Guard(original: File.Entry, current: File.Entry) -> Bool { return true }; flow Main() -> File.Remove! { directory = File.directory(File.Path.new("root")); return File.Remove.checked(List.new(directory), Guard) }; return Main()`,
		`flow Equal(value: File.Directory) -> Bool { return Math.eq(value, value) }; return 0`,
		`flow Equal(value: File.Scan) -> Bool { return Math.eq(value, value) }; return 0`,
		`flow Equal(value: Host.Invocation) -> Bool { return Math.eq(value, value) }; return 0`,
	} {
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 1 || diagnostics(t, got.stderr).Diagnostics[0].Stage != "check" {
			t.Fatalf("nominal observation contract: %+v", got)
		}
	}
}

func TestF025InspectionDoesNotObserve(t *testing.T) {
	t.Setenv("HOME", "invalid-home-must-not-be-read-by-inspection")
	const source = `flow Eligible(original: File.Entry, current: File.Entry) -> Bool { return Math.eq(original.owner_uid, current.owner_uid) }; flow Main() -> Int! { initial = Host.Invocation.current(); directory = File.directory(File.Path.new("/not-an-observed-root")); scan = File.scan(directory.path); target = File.Remove.checked(scan.entries, Eligible); commit target; return List.length(scan.vanished_names) }; return Main()`
	report, _, raw := inspectSource(t, source)
	operations := map[string]bool{}
	for _, read := range report.Reads {
		operations[read.Operation] = true
	}
	for _, operation := range []string{"Host.Invocation.current", "File.directory", "File.scan"} {
		if !operations[operation] {
			t.Fatalf("inspection omitted %s: %s", operation, raw)
		}
	}
	var document struct {
		Report struct {
			Targets []struct {
				Operation string
				Guard     *struct {
					Flow, Phase string
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		t.Fatal(err)
	}
	predicate := ""
	for _, flow := range report.Flows {
		if flow.Name == "Eligible" {
			predicate = flow.ID
		}
	}
	if predicate == "" {
		t.Fatalf("inspection omitted the predicate flow: %s", raw)
	}
	foundTarget := false
	for _, target := range document.Report.Targets {
		if target.Operation == "File.Remove.checked" {
			if target.Guard == nil || target.Guard.Flow != predicate || target.Guard.Phase != "activation" {
				t.Fatalf("inspection guard description: %s", raw)
			}
			foundTarget = true
		}
	}
	if !foundTarget {
		t.Fatalf("inspection omitted the guarded target: %s", raw)
	}
	foundContext := false
	for _, context := range report.Contexts {
		if context.Flow == predicate {
			if context.Kind != "activation-guard" {
				t.Fatalf("predicate executes outside activation: %+v", context)
			}
			foundContext = true
		}
	}
	if !foundContext {
		t.Fatalf("inspection omitted activation guard evaluation: %s", raw)
	}
}
