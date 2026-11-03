package integration_test

import (
	"bytes"

	"errors"
	"fmt"

	"os"

	"testing"
	"time"
)

func TestF025FileMetadata(t *testing.T) {
	root := fileFixtureRoot(t)
	path := fileFixture(t, root, "entry", "payload")
	source := fmt.Sprintf(`flow Fields(entry: File.Entry) -> Record{owner: Int, changed: Int, modified: Int} { return Record.new(owner: entry.owner_uid, changed: entry.changed_unix_ns, modified: entry.modified_unix_ns) }; flow Main() -> List<Record{owner: Int, changed: Int, modified: Int}>! { entries = File.list(File.Path.new(%q)); return List.map(entries, Fields) }; return Main()`, root)
	command := programCommand(t, sourceFile(t, source), "run", "--list-root", root, "--format", "json")
	accessed := time.Unix(-1, 123456789)
	modified := time.Unix(1234, 987654321)
	if err := os.Chtimes(path, accessed, modified); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, change := f025NativeMetadata(info)
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "payload" {
		t.Fatalf("fixture content changed: %q %v", content, err)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("metadata execution: %v stdout=%s stderr=%s", err, &stdout, &stderr)
	}
	got := fileOutput(t, result{stdout.String(), stderr.String(), command.ProcessState.ExitCode()})
	valueJSON(t, got, fmt.Sprintf(`[{"type":"Record{changed: Int, modified: Int, owner: Int}","value":{"owner":{"type":"Int","value":"%d"},"changed":{"type":"Int","value":"%d"},"modified":{"type":"Int","value":"%d"}}}]`, owner, change, info.ModTime().UnixNano()))
}

func TestF025CheckedRemovalFallibleContext(t *testing.T) {
	for _, argument := range []string{"7", "Math.div(1, 0)"} {
		t.Run(argument, func(t *testing.T) {
			root := fileFixtureRoot(t)
			path := fileFixture(t, root, "entry", "payload")
			source := fmt.Sprintf(`flow Guard(number: Int!, original: File.Entry, current: File.Entry) -> Bool { return Math.eq(List.length(List.new(number)), 1) }; flow Main() -> Int! { entries = File.list(File.Path.new(%q)); target = File.Remove.checked(entries, %s, Guard); receipt = commit target; return receipt.completed_count }; return Main()`, root, argument)
			got := fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--list-root", root, "--remove-root", root, "--format", "json"))
			valueJSON(t, got, `"1"`)
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("guarded entry survived: %v", err)
			}
		})
	}
}

func TestF025CheckedRemovalRetainsTensorContext(t *testing.T) {
	root := fileFixtureRoot(t)
	path := fileFixture(t, root, "entry", "payload")
	source := fmt.Sprintf(`flow Reject(error: Error) -> Bool { return false }; flow Guard(tensor: Tensor<FP32>, original: File.Entry, current: File.Entry) -> Bool onError Reject { values = Tensor.values(tensor); return Math.eq(List.length(values), 1) }; flow Build(entries: List<File.Entry>) -> File.Remove! { tensor = Tensor.new(List.new(1.0), List.new(1)); return File.Remove.checked(entries, tensor, Guard) }; flow Main() -> Int! { entries = File.list(File.Path.new(%q)); target = Build(entries); receipt = commit target; return receipt.completed_count }; return Main()`, root)
	got := fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--list-root", root, "--remove-root", root, "--format", "json"))
	valueJSON(t, got, `"1"`)
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("guarded entry survived: %v", err)
	}
}

// Metadata changes are injected into the same native metadata snapshot without
// changing ownership or other external state.

type f025MetadataInfo struct {
	os.FileInfo
	native any
}

func (info f025MetadataInfo) Sys() any { return info.native }

type f025NamedEntry struct {
	os.DirEntry
	name string
}

func (entry f025NamedEntry) Name() string { return entry.name }
