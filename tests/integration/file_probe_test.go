package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func installFileProbeFixture(t *testing.T) string {
	t.Helper()
	directory := fileFixtureRoot(t)
	helper := filepath.Join(directory, "lsof")
	command := exec.Command("go", "build", "-o", helper, "./testdata/fileprobe")
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build probe fixture: %v: %s", err, output)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	record := filepath.Join(directory, "arguments.json")
	t.Setenv("LO_TEST_PROBE_RECORD", record)
	t.Setenv("LO_TEST_PROBE_STDOUT", "")
	t.Setenv("LO_TEST_PROBE_STDERR", "")
	t.Setenv("LO_TEST_PROBE_EXIT", "1")
	t.Setenv("LO_TEST_PROBE_BYTES", "")
	t.Setenv("LO_TEST_PROBE_CHANGE", "")
	t.Setenv("LO_TEST_PROBE_ACTION", "")
	return record
}

func TestFileOpenPaths(t *testing.T) {
	record := installFileProbeFixture(t)
	root := fileFixtureRoot(t)
	open := fileFixture(t, root, "open.log", "keep")
	t.Setenv("LO_TEST_PROBE_STDOUT", "p123\nf3\nn./open.log\nfcwd\nn.\n")
	t.Setenv("LO_TEST_PROBE_EXIT", "0")
	path := sourceFile(t, fmt.Sprintf(`flow Main() -> List<File.Path>! {return File.open_paths(File.Path.new(%q))}; return Main()`, root))
	got := fileOutput(t, run(t, "run", path, "--open-files-root", root, "--format", "json"))
	valueJSON(t, got, fmt.Sprintf(`[{"type":"File.Path","value":%q}]`, open))
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	if err := json.Unmarshal(data, &args); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, []string{"-Fn", "+D", "."}) {
		t.Fatalf("probe args: %v", args)
	}
	denied := run(t, "run", path, "--list-root", root, "--format", "json")
	if denied.status != 2 || diagnostics(t, denied.stderr).Diagnostics[0].Code != "runtime.capability-denied" {
		t.Fatalf("probe grant: %+v", denied)
	}
}

func TestFileOpenPathsResults(t *testing.T) {
	installFileProbeFixture(t)
	root := fileFixtureRoot(t)
	path := sourceFile(t, fmt.Sprintf(`flow Main() -> List<File.Path>! {return File.open_paths(File.Path.new(%q))}; return Main()`, root))
	for _, tc := range []struct{ name, output, stderr, status, kind string }{
		{"empty", "", "", "1", ""},
		{"whitespace-stderr", "", " \n\t", "1", ""},
		{"stderr", "", "probe failed", "1", "File.OpenFilesFailed"},
		{"status", "", "", "2", "File.OpenFilesFailed"},
		{"invalid-utf8", string([]byte{0xff}), "", "0", "File.OpenFilesFailed"},
		{"malformed", "unrecognized probe output\n", "", "0", "File.OpenFilesFailed"},
		{"invalid-process", "pabc\n", "", "0", "File.OpenFilesFailed"},
		{"escaping-path", "n../outside\n", "", "0", "File.OpenFilesFailed"},
		{"display-escape", "n./escaped\\n.log\n", "", "0", "File.OpenFilesFailed"},
		{"literal-backslash", "n./back\\slash.log\n", "", "0", "File.OpenFilesFailed"},
		{"control-character", "n./tab\tname.log\n", "", "0", "File.OpenFilesFailed"},
		{"empty-descriptor", "f\n", "", "0", "File.OpenFilesFailed"},
		{"malformed-descriptor", "f3?\n", "", "0", "File.OpenFilesFailed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LO_TEST_PROBE_STDOUT", tc.output)
			t.Setenv("LO_TEST_PROBE_STDERR", tc.stderr)
			t.Setenv("LO_TEST_PROBE_EXIT", tc.status)
			got := run(t, "run", path, "--open-files-root", root, "--format", "json")
			if tc.kind == "" {
				valueJSON(t, fileOutput(t, got), `[]`)
				return
			}
			var output struct {
				Diagnostics []struct{ Error runtimeValue }
			}
			if err := json.Unmarshal([]byte(got.stderr), &output); err != nil {
				t.Fatal(err)
			}
			if got.status != 1 || len(output.Diagnostics) != 1 || output.Diagnostics[0].Error.Kind != tc.kind {
				t.Fatalf("probe failure: %+v", got)
			}
		})
	}
}

func TestFileOpenPathsRecordedProtocol(t *testing.T) {
	installFileProbeFixture(t)
	root := fileFixtureRoot(t)
	// Captured from the host probe against a retained Lo fixture directory.
	t.Setenv("LO_TEST_PROBE_STDOUT", "p33265\nf3\nn./held.log\nf4\nn.\np33271\nfcwd\nn.\np33276\nfcwd\nn.\n")
	t.Setenv("LO_TEST_PROBE_EXIT", "0")
	path := sourceFile(t, fmt.Sprintf(`return File.open_paths(File.Path.new(%q))`, root))
	got := fileOutput(t, run(t, "run", path, "--open-files-root", root, "--format", "json"))
	valueJSON(t, got, fmt.Sprintf(`[{"type":"File.Path","value":%q}]`, filepath.Join(root, "held.log")))
}

func TestFileOpenPathsHostProbe(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("host lsof is unavailable; fixed protocol fixtures cover the boundary")
	}
	root := fileFixtureRoot(t)
	path := fileFixture(t, root, "held.log", "held")
	held, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	program := sourceFile(t, fmt.Sprintf(`return File.open_paths(File.Path.new(%q))`, root))
	got := fileOutput(t, run(t, "run", program, "--open-files-root", root, "--format", "json"))
	valueJSON(t, got, fmt.Sprintf(`[{"type":"File.Path","value":%q}]`, path))
}

func TestFileDirectoryAbsent(t *testing.T) {
	root := filepath.Join(fileFixtureRoot(t), "absent")
	path := sourceFile(t, fmt.Sprintf(`flow Handle(e: Error) -> String {return e.kind}; flow Main() -> String onError Handle {entries = File.list(File.Path.new(%q)); return "present"}; return Main()`, root))
	got := fileOutput(t, run(t, "run", path, "--list-root", root, "--remove-root", root, "--format", "json"))
	valueJSON(t, got, `"File.DirectoryAbsent"`)
	denied := run(t, "run", path, "--format", "json")
	if denied.status != 2 || diagnostics(t, denied.stderr).Diagnostics[0].Code != "runtime.capability-denied" {
		t.Fatalf("missing is not denied: %+v", denied)
	}
}

func TestFileRemovalHardlinks(t *testing.T) {
	root := fileFixtureRoot(t)
	first := fileFixture(t, root, "a", "linked")
	if err := os.Link(first, filepath.Join(root, "b")); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(fileFixtureRoot(t), "retained")
	if err := os.Link(first, sibling); err != nil {
		t.Fatal(err)
	}
	path := sourceFile(t, fmt.Sprintf(`flow Main() -> Int! {entries = File.list(File.Path.new(%q)); removal = File.remove(entries); commit removal; return List.length(entries)}; return Main()`, root))
	valueJSON(t, fileOutput(t, run(t, "run", path, "--list-root", root, "--remove-root", root, "--format", "json")), `"2"`)
	remaining, err := os.ReadDir(root)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("selected hardlinks remain: %v, %v", remaining, err)
	}
	if content, err := os.ReadFile(sibling); err != nil || string(content) != "linked" {
		t.Fatalf("sibling hardlink: %q, %v", content, err)
	}
}
