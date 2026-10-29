package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func TestF025FileMetadata(t *testing.T) {
	root := fileFixtureRoot(t)
	path := fileFixture(t, root, "entry", "payload")
	source := fmt.Sprintf(`flow Fields(entry: File.Entry) -> Record{owner: Int, changed: Int, modified: Int} { return Record.new(owner: entry.owner_uid, changed: entry.changed_unix_ns, modified: entry.modified_unix_ns) }; flow Main() -> List<Record{owner: Int, changed: Int, modified: Int}>! { entries = File.list(File.Path.new(%q)); return List.map(entries, Fields) }; return Main()`, root)
	command := runtimeCommand(t, "run", sourceFile(t, source), "--list-root", root, "--format", "json")
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

func TestF025InjectedFileMetadata(t *testing.T) {
	root := fileFixtureRoot(t)
	path := fileFixture(t, root, "entry", "payload")
	modified := time.Unix(1234, 987654321)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	filesystem := &scheduledFileIO{FileIO: adapter.NativeFileIO()}
	filesystem.stat = func(dir *os.Root, name string, occurrence int) (os.FileInfo, error) {
		info, err := filesystem.FileIO.Lstat(dir, name)
		if err == nil && name == "entry" {
			return f025FixedMetadata(info), nil
		}
		return info, err
	}
	source := fmt.Sprintf(`flow Fields(entry: File.Entry) -> Record{owner: Int, accessed: Int, changed: Int, modified: Int} { return Record.new(owner: entry.owner_uid, accessed: entry.accessed_unix_ns, changed: entry.changed_unix_ns, modified: entry.modified_unix_ns) }; flow Main() -> List<Record{owner: Int, accessed: Int, changed: Int, modified: Int}>! { entries = File.list(File.Path.new(%q)); return List.map(entries, Fields) }; return Main()`, root)
	output, err, diagnostics := injectedFileProgram(t, context.Background(), root, source, filesystem)
	if err != nil {
		t.Fatalf("injected file metadata: %v %+v", err, diagnostics)
	}
	result, failure := output.(value.Fallible).Outcome()
	if failure != nil {
		t.Fatalf("injected file metadata failed: %v", failure)
	}
	serialized, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var got runtimeValue
	if err := json.Unmarshal(serialized, &got); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, got, `[{"type":"Record{accessed: Int, changed: Int, modified: Int, owner: Int}","value":{"owner":{"type":"Int","value":"1001"},"accessed":{"type":"Int","value":"-876543211"},"changed":{"type":"Int","value":"-1000000000"},"modified":{"type":"Int","value":"1234987654321"}}}]`)
	if filesystem.stats["entry"] != 1 {
		t.Fatalf("metadata snapshot count: %v", filesystem.stats)
	}
}

func TestF025CheckedRemovalFallibleContext(t *testing.T) {
	for _, argument := range []string{"7", "Math.div(1, 0)"} {
		t.Run(argument, func(t *testing.T) {
			root := fileFixtureRoot(t)
			path := fileFixture(t, root, "entry", "payload")
			source := fmt.Sprintf(`flow Guard(number: Int!, original: File.Entry, current: File.Entry) -> Bool { return Math.eq(List.length(List.new(number)), 1) }; flow Main() -> Int! { entries = File.list(File.Path.new(%q)); target = File.Remove.checked(entries, %s, Guard); receipt = commit target; return receipt.completed_count }; return Main()`, root, argument)
			got := fileOutput(t, run(t, "run", sourceFile(t, source), "--list-root", root, "--remove-root", root, "--format", "json"))
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
	got := fileOutput(t, run(t, "run", sourceFile(t, source), "--list-root", root, "--remove-root", root, "--format", "json"))
	valueJSON(t, got, `"1"`)
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("guarded entry survived: %v", err)
	}
}

func TestF025ScanReportsVanishedChildren(t *testing.T) {
	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprint(strict), func(t *testing.T) {
			root := fileFixtureRoot(t)
			fileFixture(t, root, "gone", "gone")
			fileFixture(t, root, "present", "present")
			filesystem := &scheduledFileIO{FileIO: adapter.NativeFileIO()}
			filesystem.stat = func(dir *os.Root, name string, occurrence int) (os.FileInfo, error) {
				if name == "gone" {
					return nil, os.ErrNotExist
				}
				return filesystem.FileIO.Lstat(dir, name)
			}
			source := fmt.Sprintf(`flow Main() -> List<String>! { scan = File.scan(File.Path.new(%q)); return scan.vanished_names }; return Main()`, root)
			if strict {
				source = fmt.Sprintf(`return File.list(File.Path.new(%q))`, root)
			}
			output, err, got := injectedFileProgram(t, context.Background(), root, source, filesystem)
			if strict {
				if err == nil || !strings.Contains(got.stderr, "File.ListFailed") {
					t.Fatalf("strict listing lost the vanished-child failure: %v %+v", err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("scan disappearance: %v %+v", err, got)
			}
			result, failure := output.(value.Fallible).Outcome()
			if failure != nil || result.(value.List).Len() != 1 || result.(value.List).Items()[0].(value.Text).Text() != "gone" {
				t.Fatalf("vanished names: %v %v", result, failure)
			}
		})
	}
}

func TestF025ScanPreservesOtherErrors(t *testing.T) {
	root := fileFixtureRoot(t)
	fileFixture(t, root, "entry", "payload")
	filesystem := &scheduledFileIO{FileIO: adapter.NativeFileIO()}
	filesystem.stat = func(dir *os.Root, name string, occurrence int) (os.FileInfo, error) {
		if name == "entry" {
			return nil, errors.New("injected metadata failure")
		}
		return filesystem.FileIO.Lstat(dir, name)
	}
	_, err, got := injectedFileProgram(t, context.Background(), root, fmt.Sprintf(`return File.scan(File.Path.new(%q))`, root), filesystem)
	if err == nil || !strings.Contains(got.stderr, "File.ListFailed") {
		t.Fatalf("scan suppressed unrelated failure: %v %+v", err, got)
	}
}

func TestF025CheckedRemovalRejectsAtPreflight(t *testing.T) {
	root := fileFixtureRoot(t)
	fileFixture(t, root, "entry", "payload")
	source := fmt.Sprintf(`flow Reject(original: File.Entry, current: File.Entry) -> Bool { return false }; flow Main() -> File.RemoveReceipt! { entries = File.list(File.Path.new(%q)); target = File.Remove.checked(entries, Reject); receipt = commit target; return receipt }; return Main()`, root)
	_, err, got := injectedFileProgram(t, context.Background(), root, source, adapter.NativeFileIO())
	if err == nil {
		t.Fatal("false predicate permitted removal")
	}
	progressFields(t, got, 0, 1, "preflight", "File.EntryChanged")
	if !strings.Contains(got.stderr, "predicate-false") {
		t.Fatalf("guard rejection lacks fixed condition: %+v", got)
	}
}

// Metadata changes are injected into the same native metadata snapshot without
// changing ownership or other external state.
func TestF025CheckedRemovalMetadataChanges(t *testing.T) {
	for _, field := range []string{"owner_uid", "accessed_unix_ns", "changed_unix_ns", "link_count"} {
		t.Run(field, func(t *testing.T) {
			root := fileFixtureRoot(t)
			fileFixture(t, root, "a", "a")
			fileFixture(t, root, "b", "bb")
			filesystem := &scheduledFileIO{FileIO: adapter.NativeFileIO()}
			filesystem.stat = func(dir *os.Root, name string, occurrence int) (os.FileInfo, error) {
				info, err := filesystem.FileIO.Lstat(dir, name)
				if err == nil && name == "b" && occurrence == 3 {
					return f025ChangedMetadata(info, field), nil
				}
				return info, err
			}
			source := fmt.Sprintf(`flow Eligible(original: File.Entry, current: File.Entry) -> Bool { return Math.eq(original.%s, current.%s) }; flow Main() -> File.RemoveReceipt! { entries = File.list(File.Path.new(%q)); target = File.Remove.checked(entries, Eligible); receipt = commit target; return receipt }; return Main()`, field, field, root)
			_, err, got := injectedFileProgram(t, context.Background(), root, source, filesystem)
			if err == nil {
				t.Fatal("changed metadata passed removal guard")
			}
			progressFields(t, got, 1, 2, "remove", "File.EntryChanged")
			if !strings.Contains(got.stderr, "predicate-false") {
				t.Fatalf("missing guard rejection: %+v", got)
			}
			if _, err := os.Lstat(filepath.Join(root, "a")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("first selected entry survived: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(root, "b")); err != nil {
				t.Fatalf("rejected entry changed: %v", err)
			}
			if filesystem.stats["a"] != 3 || filesystem.stats["b"] != 3 || len(filesystem.removals) != 1 {
				t.Fatalf("guard repeated metadata lookups or mutation: stats=%v removals=%v", filesystem.stats, filesystem.removals)
			}
		})
	}
}
func TestF025DirectoryMetadataFailure(t *testing.T) {
	root := fileFixtureRoot(t)
	filesystem := &scheduledFileIO{FileIO: adapter.NativeFileIO()}
	filesystem.stat = func(dir *os.Root, name string, occurrence int) (os.FileInfo, error) {
		if name == "." {
			return nil, errors.New("injected directory metadata failure")
		}
		return filesystem.FileIO.Lstat(dir, name)
	}
	_, err, got := injectedFileProgram(t, context.Background(), root, fmt.Sprintf(`return File.directory(File.Path.new(%q))`, root), filesystem)
	if err == nil || !strings.Contains(got.stderr, "File.DirectoryFailed") {
		t.Fatalf("directory metadata failure: %v %+v", err, got)
	}
}
func TestF025ScanInvalidName(t *testing.T) {
	root := fileFixtureRoot(t)
	filesystem := &scheduledFileIO{FileIO: adapter.NativeFileIO()}
	filesystem.readDir = func(*os.File, int) ([]os.DirEntry, error) { return []os.DirEntry{invalidNameEntry{}}, io.EOF }
	_, err, got := injectedFileProgram(t, context.Background(), root, fmt.Sprintf(`return File.scan(File.Path.new(%q))`, root), filesystem)
	if err == nil || !strings.Contains(got.stderr, "File.InvalidName") {
		t.Fatalf("scan invalid UTF-8 name: %v %+v", err, got)
	}
}

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

func TestF025ScanVanishedLimit(t *testing.T) {
	root := fileFixtureRoot(t)
	filesystem := &scheduledFileIO{FileIO: adapter.NativeFileIO()}
	enumerated := 0
	filesystem.readDir = func(_ *os.File, count int) ([]os.DirEntry, error) {
		entries := make([]os.DirEntry, 0, count)
		for len(entries) < count && enumerated < 10_001 {
			entries = append(entries, f025NamedEntry{name: fmt.Sprintf("vanished-%05d", enumerated)})
			enumerated++
		}
		return entries, nil
	}
	filesystem.stat = func(dir *os.Root, name string, occurrence int) (os.FileInfo, error) {
		if name != "." {
			return nil, os.ErrNotExist
		}
		return filesystem.FileIO.Lstat(dir, name)
	}
	_, err, got := injectedFileProgram(t, context.Background(), root, fmt.Sprintf(`return File.scan(File.Path.new(%q))`, root), filesystem)
	if err == nil || !strings.Contains(got.stderr, "runtime.limit") || !strings.Contains(got.stderr, "directory entries") {
		t.Fatalf("vanished names escaped discovery bound: %v %+v", err, got)
	}
}
