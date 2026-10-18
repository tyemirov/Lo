package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fileFixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func fileFixture(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func fileOutput(t *testing.T, got result) runtimeValue {
	t.Helper()
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("file workflow: %+v", got)
	}
	var out struct{ Result runtimeValue }
	if err := json.Unmarshal([]byte(got.stdout), &out); err != nil {
		t.Fatal(err)
	}
	if out.Result.Status == "ok" {
		var inner runtimeValue
		if err := json.Unmarshal(out.Result.Value, &inner); err != nil {
			t.Fatal(err)
		}
		return inner
	}
	return out.Result
}

func TestFileDiscovery(t *testing.T) {
	root := fileFixtureRoot(t)
	fileFixture(t, root, "z.log", "last")
	fileFixture(t, root, ".first", "first")
	path := sourceFile(t, fmt.Sprintf(`flow Names() -> List<String>! { entries = File.list(File.Path.new(%q)); return List.map(entries, Name) }; flow Name(entry: File.Entry) -> String { return entry.name }; return Names()`, root))
	checked := run(t, "check", path, "--format", "json")
	if checked.status != 0 {
		t.Fatalf("File discovery must type check: %+v", checked)
	}
	v := fileOutput(t, run(t, "run", path, "--list-root", root, "--format", "json"))
	valueJSON(t, v, `[{"type":"String","value":".first"},{"type":"String","value":"z.log"}]`)
	denied := run(t, "run", path, "--format", "json")
	if denied.status != 2 || diagnostics(t, denied.stderr).Diagnostics[0].Code != "runtime.capability-denied" {
		t.Fatalf("list grant: %+v", denied)
	}
}

func TestFileRemoval(t *testing.T) {
	root := fileFixtureRoot(t)
	name := fileFixture(t, root, "old.log", "old")
	construct := sourceFile(t, fmt.Sprintf(`flow Make() -> Effect! { entries = File.list(File.Path.new(%q)); return File.remove(entries) }; return Make()`, root))
	checked := run(t, "check", construct, "--format", "json")
	if checked.status != 0 {
		t.Fatalf("File removal must type check: %+v", checked)
	}
	fileOutput(t, run(t, "run", construct, "--list-root", root, "--format", "json"))
	if _, err := os.Stat(name); err != nil {
		t.Fatalf("construction changed file: %v", err)
	}
	commit := sourceFile(t, fmt.Sprintf(`flow Remove() -> Int! { entries = File.list(File.Path.new(%q)); removal = File.remove(entries); commit removal; return List.length(entries) }; return Remove()`, root))
	denied := run(t, "run", commit, "--list-root", root, "--format", "json")
	if denied.status != 2 || diagnostics(t, denied.stderr).Diagnostics[0].Code != "runtime.capability-denied" {
		t.Fatalf("remove grant: %+v", denied)
	}
	if _, err := os.Stat(name); err != nil {
		t.Fatalf("denied commit changed file: %v", err)
	}
	v := fileOutput(t, run(t, "run", commit, "--list-root", root, "--remove-root", root, "--format", "json"))
	valueJSON(t, v, `"1"`)
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("committed file remains: %v", err)
	}
}

func TestGatewayLogCleanup(t *testing.T) {
	installFileProbeFixture(t)
	root := fileFixtureRoot(t)
	const cutoffNS int64 = 2_000_000_000_000_000_000
	cutoff := time.Unix(0, cutoffNS)
	for _, fixture := range []struct {
		name, content string
		age           time.Duration
	}{
		{"deployment-1.log", "old", -time.Second},
		{"deployment-2.log", "exact", 0},
		{"deployment-3.log", "recent", time.Second},
		{"deployment-x.log", "wrong name", -time.Second},
		{"deployment-4.log.bak", "suffix", -time.Second},
		{"deployment-5.log", "linked", -time.Second},
		{"deployment-8.log", "open", -time.Second},
	} {
		path := fileFixture(t, root, fixture.name, fixture.content)
		at := cutoff.Add(fixture.age)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Link(filepath.Join(root, "deployment-5.log"), filepath.Join(root, "linked-copy")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "deployment-5.log"), filepath.Join(root, "deployment-9.log")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LO_TEST_PROBE_STDOUT", "p123\nn./deployment-8.log\n")
	t.Setenv("LO_TEST_PROBE_EXIT", "0")
	if err := os.Symlink(filepath.Join(root, "deployment-1.log"), filepath.Join(root, "deployment-6.log")); err != nil {
		t.Fatal(err)
	}
	subdir, err := os.MkdirTemp(root, "directory-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(subdir, filepath.Join(root, "deployment-7.log")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repository, "docs/examples/gateway-log-cleanup.lo"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(data), `File.Path.new("gateway-logs")`, fmt.Sprintf("File.Path.new(%q)", root), 1)
	path := sourceFile(t, source)
	checked := run(t, "check", path, "--format", "json")
	if checked.status != 0 {
		t.Fatalf("Gateway workflow must type check: %+v", checked)
	}
	preview := sourceFile(t, strings.Replace(source, "return Cleanup(root, cutoff_unix_ns)", "return Preview(root, cutoff_unix_ns)", 1))
	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	previewValue := fileOutput(t, run(t, "run", preview, "--list-root", root, "--open-files-root", root, "--format", "json"))
	var previewFields map[string]runtimeValue
	if err := json.Unmarshal(previewValue.Value, &previewFields); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, previewFields["names"], `[{"type":"String","value":"deployment-1.log"},{"type":"String","value":"deployment-5.log"},{"type":"String","value":"deployment-9.log"}]`)
	v := fileOutput(t, run(t, "run", path, "--list-root", root, "--remove-root", root, "--open-files-root", root, "--format", "json"))
	var fields map[string]runtimeValue
	if err := json.Unmarshal(v.Value, &fields); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, fields["removed_count"], `"3"`)
	valueJSON(t, fields["observed_bytes"], `"15"`)
	valueJSON(t, fields["skipped_count"], `"1"`)
	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var expected, actual []string
	for _, entry := range before {
		if entry.Name() != "deployment-1.log" && entry.Name() != "deployment-5.log" && entry.Name() != "deployment-9.log" {
			expected = append(expected, entry.Name())
		}
	}
	for _, entry := range after {
		actual = append(actual, entry.Name())
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("remaining entries %v, want %v", actual, expected)
	}
}
