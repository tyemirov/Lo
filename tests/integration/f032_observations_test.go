package integration_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestF032ObservationContracts(t *testing.T) {
	for _, source := range []string{
		`flow Project(entry: File.Entry) -> List<Int> { return List.new(entry.device, entry.inode, entry.allocated_bytes) }; return 0`,
		`flow Project(directory: File.Directory) -> List<Int> { return List.new(directory.device, directory.inode, directory.allocated_bytes) }; return 0`,
		`flow Project(usage: File.System.Usage) -> List<Int> { return List.new(usage.total_bytes, usage.used_bytes, usage.free_bytes) }; return 0`,
		`return Clock.now_unix_ns()`,
		`return File.System.usage(File.Path.new("unobserved"))`,
	} {
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 0 || got.stderr != "" {
			t.Fatalf("observation contract: %+v", got)
		}
	}
}

func TestF032ObservationsAreHostReads(t *testing.T) {
	for _, source := range []string{
		`flow Observe(value: Int) -> Int! { return Clock.now_unix_ns() }; return List.map(List.new(0), Observe)`,
		`flow Observe(path: File.Path) -> File.System.Usage! { return File.System.usage(path) }; return List.map(List.new(File.Path.new("unobserved")), Observe)`,
	} {
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 1 || diagnostics(t, got.stderr).Diagnostics[0].Code != "check.effectful-callback" {
			t.Fatalf("host observation accepted as pure callback: %+v", got)
		}
	}
}

func TestF032NativeFileIdentityAndAllocation(t *testing.T) {
	root := fileFixtureRoot(t)
	fileFixture(t, root, "empty", "")
	fileFixture(t, root, "ordinary", "allocated contents")
	sparse := filepath.Join(root, "sparse")
	file, err := os.Create(sparse)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(64 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`flow Fields(entry: File.Entry) -> Record{name: String, device: Int, inode: Int, allocated: Int, logical: Int} { return Record.new(name: entry.name, device: entry.device, inode: entry.inode, allocated: entry.allocated_bytes, logical: entry.size_bytes) }; flow Main() -> Record{device: Int, inode: Int, allocated: Int, entries: List<Record{name: String, device: Int, inode: Int, allocated: Int, logical: Int}>}! { scan = File.scan(File.Path.new(%q)); return Record.new(device: scan.directory.device, inode: scan.directory.inode, allocated: scan.directory.allocated_bytes, entries: List.map(scan.entries, Fields)) }; return Main()`, root)
	got := fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--list-root", root, "--format", "json"))
	check := func(path string, fields map[string]runtimeValue) {
		t.Helper()
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		allocation := new(big.Int).Mul(big.NewInt(stat.Blocks), big.NewInt(512))
		valueJSON(t, fields["device"], fmt.Sprintf(`"%d"`, stat.Dev))
		valueJSON(t, fields["inode"], fmt.Sprintf(`"%d"`, stat.Ino))
		valueJSON(t, fields["allocated"], fmt.Sprintf(`"%s"`, allocation))
	}
	check(root, processFields(t, got))
	var entries []runtimeValue
	if err := json.Unmarshal(recordField(t, got, "entries").Value, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries: %+v", entries)
	}
	for index, name := range []string{"empty", "ordinary", "sparse"} {
		fields := processFields(t, entries[index])
		valueJSON(t, fields["name"], fmt.Sprintf(`%q`, name))
		check(filepath.Join(root, name), fields)
	}
}

func TestF032VolumeExactAuthority(t *testing.T) {
	root := fileFixtureRoot(t)
	child, err := os.MkdirTemp(root, "child-")
	if err != nil {
		t.Fatal(err)
	}
	source := sourceFile(t, fmt.Sprintf(`return File.System.usage(File.Path.new(%q))`, child))
	for _, grants := range [][]string{nil, {"--read-root", root}, {"--list-root", root}} {
		args := append([]string{"--format", "json"}, grants...)
		got := runProgram(t, source, "run", args...)
		if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.capability-denied" {
			t.Fatalf("volume observation widened authority: %+v", got)
		}
	}
	got := fileOutput(t, runProgram(t, source, "run", "--format", "json", "--list-root", child))
	if got.Type != "File.System.Usage" {
		t.Fatalf("volume result: %+v", got)
	}
}

func TestF032VolumeAbsentAndSymlink(t *testing.T) {
	root := fileFixtureRoot(t)
	missing := filepath.Join(root, "missing")
	source := sourceFile(t, fmt.Sprintf(`return File.System.usage(File.Path.new(%q))`, missing))
	got := runProgram(t, source, "run", "--format", "json", "--list-root", missing)
	if got.status != 1 || !strings.Contains(got.stderr, "File.DirectoryAbsent") {
		t.Fatalf("missing volume root: %+v", got)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	source = sourceFile(t, fmt.Sprintf(`return File.System.usage(File.Path.new(%q))`, link))
	got = runProgram(t, source, "run", "--format", "json", "--list-root", link)
	if got.status != 2 || !strings.Contains(got.stderr, "symlink") {
		t.Fatalf("symlink volume root: %+v", got)
	}
}

func TestF032VolumeNominalResult(t *testing.T) {
	for _, source := range []string{
		`flow Read(value: File.System.Usage) -> Int { return value.total_bytes }; return Read(Record.new(total_bytes: 1, used_bytes: 0, free_bytes: 1))`,
		`flow Main() -> File.Remove! { usage = File.System.usage(File.Path.new("unobserved")); return File.Remove.new(List.new(usage)) }; return Main()`,
	} {
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, "check.type-mismatch") {
			t.Fatalf("volume observation acquired authority: %+v", got)
		}
	}
}
