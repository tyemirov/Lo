package integration_test

import (
	"fmt"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"

	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileLanguageInspection(t *testing.T) {
	source := `flow Keep(entry: File.Entry) -> Bool { return String.eq(entry.kind, "regular") }
 flow Main() -> Int! { entries = File.list(File.Path.new("/fixture/logs"))
 selected = List.filter(entries, Keep)
 effect = File.Remove.new(selected)
 commit effect
 return List.length(selected) }; return Main()`
	report, _, _ := inspectSource(t, source)
	capabilities := map[string]inspectionResource{}
	for _, capability := range report.Capabilities {
		capabilities[capability.Operation] = capability
	}
	if _, ok := capabilities["File.list"]; !ok {
		t.Fatal("listing capability absent")
	}
	removal, ok := capabilities["File.remove"]
	if !ok || removal.Target.Kind != "calculated" || removal.Target.Value != "/fixture/logs" {
		t.Fatalf("removal target: %+v", removal)
	}
	preview, _, _ := inspectSource(t, `flow Main() -> Int! { entries = File.list(File.Path.new("/fixture/logs")); pending = File.Remove.new(entries); return List.length(entries) }; return Main()`)
	for _, capability := range preview.Capabilities {
		if capability.Operation == "File.remove" {
			t.Fatal("uncommitted removal requires activation capability")
		}
	}
}

func TestFileLanguageNominalAndCheckpointRejections(t *testing.T) {
	cases := []string{
		`flow F(entry: File.Entry) -> String { return entry.name }; return F(Record.new(path: File.Path.new("x"), name: "x", kind: "regular", size_bytes: 0, modified_unix_ns: 0, link_count: 1, owner_uid: 0, accessed_unix_ns: 0, changed_unix_ns: 0))`,
		`flow Main() -> Bytes! { entries = File.list(File.Path.new("/fixture")); return Checkpoint.encode(entries) }; return Main()`,
		`flow Main() -> Bytes! { entries = File.list(File.Path.new("/fixture")); return Checkpoint.encode(File.Remove.new(entries)) }; return Main()`,
		`flow F(entry: File.Entry) -> Bool { return Match.new(entry, entry: true, _: false) }; return 0`,
	}
	for _, source := range cases {
		t.Run(source[:20], func(t *testing.T) {
			got := run(t, "check", sourceFile(t, source), "--format", "json")
			if got.status == 0 || !strings.Contains(got.stdout+got.stderr, "check.type-") {
				t.Fatalf("expected type rejection: %+v", got)
			}
		})
	}
}

func TestFileLanguageSelectionValidation(t *testing.T) {
	for _, scenario := range []string{"duplicate", "directory", "symlink", "mixed"} {
		t.Run(scenario, func(t *testing.T) {
			root := fileFixtureRoot(t)
			fileFixture(t, root, "a", "one")
			var extraRoot string
			selectSource := fmt.Sprintf(`entries = File.list(File.Path.new(%q))`, root)
			switch scenario {
			case "duplicate":
				selectSource += `; selected = List.flat_map(entries, Duplicate)`
			case "directory":
				if _, err := os.MkdirTemp(root, "dir"); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(filepath.Join(root, "a"), filepath.Join(root, "b")); err != nil {
					t.Fatal(err)
				}
			case "mixed":
				other := fileFixtureRoot(t)
				extraRoot = other
				fileFixture(t, other, "b", "two")
				selectSource = fmt.Sprintf(`left = File.list(File.Path.new(%q)); right = File.list(File.Path.new(%q)); selected = List.flat_map(List.new(left, right), Entries)`, root, other)
			}
			if scenario != "duplicate" && scenario != "mixed" {
				selectSource += `; selected = entries`
			}
			source := `flow Handle(e: Error) -> String {return e.kind}; flow Duplicate(entry: File.Entry) -> List<File.Entry> {return List.new(entry,entry)}; flow Entries(entries: List<File.Entry>) -> List<File.Entry> { return entries }; flow Check() -> String onError Handle {` + selectSource + `; pending = File.Remove.new(selected); return "accepted"}; return Check()`
			args := []string{"run", sourceFile(t, source), "--list-root", root, "--format", "json"}
			if extraRoot != "" {
				args = append(args, "--list-root", extraRoot)
			}
			got := fileOutput(t, runProgram(t, args[1], args[0], args[2:]...))
			valueJSON(t, got, `"File.InvalidSelection"`)
			if _, err := os.Stat(filepath.Join(root, "a")); err != nil {
				t.Fatalf("construction mutated file: %v", err)
			}
		})
	}
	empty := sourceFile(t, `flow Main() -> String! {pending = File.Remove.new(List.new()); commit pending; return "empty"}; return Main()`)
	got := fileOutput(t, runProgram(t, empty, "run", "--format", "json"))
	valueJSON(t, got, `"empty"`)
}

func fileLanguageListType(t *testing.T) types.List {
	t.Helper()
	typ, err := types.NewList(types.FileEntry())
	if err != nil {
		t.Fatal(err)
	}
	return typ
}
