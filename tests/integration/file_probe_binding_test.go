package integration_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func TestFileOpenPathsProtectsExecutable(t *testing.T) {
	for _, alias := range []string{"direct", "hardlink", "symlink"} {
		for _, operation := range []string{"Write", "Append", "Remove"} {
			t.Run(alias+"/"+operation, func(t *testing.T) {
				record := installFileProbeFixture(t)
				tool := filepath.Join(filepath.Dir(record), "lsof")
				original, err := os.ReadFile(tool)
				if err != nil {
					t.Fatal(err)
				}
				root := filepath.Dir(tool)
				if alias != "direct" {
					root = fileFixtureRoot(t)
					link := filepath.Join(root, "lsof")
					if alias == "hardlink" {
						err = os.Link(tool, link)
					} else {
						err = os.Symlink(tool, link)
					}
					if err != nil {
						t.Fatal(err)
					}
					if alias == "symlink" {
						t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
					} else {
						tool = link
					}
				}
				if alias == "symlink" && operation == "Remove" {
					root = filepath.Dir(record)
				}
				mutation := fmt.Sprintf(`target = File.%s.new(File.Path.new(%q), Bytes.new("replacement")); commit target`, operation, tool)
				if operation == "Remove" {
					mutation = fmt.Sprintf(`entries = File.list(File.Path.new(%q)); target = File.Remove.new(entries); commit target`, root)
				}
				program := sourceFile(t, fmt.Sprintf(`flow Main() -> List<File.Path>! {%s; return File.open_paths(File.Path.new(%q))}; return Main()`, mutation, root))
				args := []string{"run", program, "--write-root", root, "--list-root", root, "--remove-root", root, "--open-files-root", root, "--format", "json"}
				if alias == "symlink" && root != filepath.Dir(record) {
					args = append(args, "--write-root", filepath.Dir(record))
				}
				got := run(t, args...)
				if got.status != 2 || !strings.Contains(got.stderr, "protected") {
					t.Fatalf("probe mutation was not denied: %+v", got)
				}
				remaining, err := os.ReadFile(tool)
				if err != nil || sha256.Sum256(remaining) != sha256.Sum256(original) {
					t.Fatalf("probe bytes changed: %v", err)
				}
				if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("probe ran after mutation: %v", err)
				}
			})
		}
	}
}

func TestFileOpenPathsStandaloneProtection(t *testing.T) {
	record := installFileProbeFixture(t)
	root := filepath.Dir(record)
	tool := filepath.Join(root, "lsof")
	before, err := os.ReadFile(tool)
	if err != nil {
		t.Fatal(err)
	}
	files, err := adapter.OpenFiles(adapter.FileRoots{Write: []string{root}, OpenFiles: []string{root}}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	_, err = evaluate(t, fmt.Sprintf(`target = File.Write.new(File.Path.new(%q), Bytes.new("replacement")); commit target`, tool), files)
	var host execution.HostError
	if !errors.As(err, &host) || !strings.Contains(err.Error(), "protected") {
		t.Fatalf("standalone probe mutation: %v", err)
	}
	after, err := os.ReadFile(tool)
	if err != nil || sha256.Sum256(after) != sha256.Sum256(before) {
		t.Fatalf("standalone probe bytes changed: %v", err)
	}
}

func TestFileOpenPathsFreezesExecutableLookup(t *testing.T) {
	for _, available := range []bool{true, false} {
		t.Run(fmt.Sprint(available), func(t *testing.T) {
			record := installFileProbeFixture(t)
			toolDirectory := filepath.Dir(record)
			root := fileFixtureRoot(t)
			initial, later := toolDirectory, root
			want := "ok"
			if !available {
				initial, later, want = root, toolDirectory, "File.OpenFilesFailed"
			}
			t.Setenv("PATH", initial)
			probe := &boundFileProbe{FileIO: adapter.NativeFileIO(), before: func() { t.Setenv("PATH", later) }}
			source := fmt.Sprintf(`flow Handle(e: Error) -> String { return e.kind }; flow Main() -> String onError Handle { paths = File.open_paths(File.Path.new(%q)); return "ok" }; return Main()`, root)
			output, err, _ := injectedFileProgram(t, context.Background(), root, source, probe)
			if err != nil || output.(value.Text).Text() != want {
				t.Fatalf("probe lookup changed during evaluation: %v %v", output, err)
			}
			_, err = os.Stat(record)
			if available && err != nil || !available && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unexpected probe activation: %v", err)
			}
		})
	}
}

type boundFileProbe struct {
	adapter.FileIO
	before func()
}

func (probe *boundFileProbe) ProbeOpenFiles(ctx context.Context, root *os.Root, executable adapter.FileProbeExecutable) ([]byte, error) {
	probe.before()
	return probe.FileIO.ProbeOpenFiles(ctx, root, executable)
}

func TestFileOpenPathsDescriptorBinding(t *testing.T) {
	installFileProbeFixture(t)
	t.Setenv("LO_TEST_PROBE_LIST", "1")
	parent := fileFixtureRoot(t)
	root, err := os.MkdirTemp(parent, "root-")
	if err != nil {
		t.Fatal(err)
	}
	fileFixture(t, root, "retained.log", "original")
	moved := filepath.Join(parent, "retained-directory")
	probe := &boundFileProbe{FileIO: adapter.NativeFileIO(), before: func() {
		if err := os.Rename(root, moved); err != nil {
			t.Fatal(err)
		}
		replacement, err := os.MkdirTemp(parent, "replacement-")
		if err != nil {
			t.Fatal(err)
		}
		fileFixture(t, replacement, "replacement.log", "replacement")
		if err := os.Rename(replacement, root); err != nil {
			t.Fatal(err)
		}
	}}
	source := fmt.Sprintf(`flow Main() -> List<File.Path>! {return File.open_paths(File.Path.new(%q))};return Main()`, root)
	output, err, _ := injectedFileProgram(t, context.Background(), root, source, probe)
	if err != nil {
		t.Fatal(err)
	}

	if fallible, ok := output.(value.Fallible); ok {
		var cause value.Error
		output, cause = fallible.Outcome()
		if cause != nil {
			t.Fatal(cause.Message())
		}
	}
	items := output.(value.List).Items()
	if len(items) != 1 || items[0].(value.Text).Text() != filepath.Join(root, "retained.log") {
		t.Fatalf("probe reopened replacement directory: %v", output)
	}

	if _, err := os.Stat(filepath.Join(root, "replacement.log")); err != nil {
		t.Fatalf("replacement fixture changed: %v", err)
	}
}

func TestFileOpenPathsProbeCancellation(t *testing.T) {
	installFileProbeFixture(t)
	t.Setenv("LO_TEST_PROBE_WAIT", "1")
	ready := filepath.Join(fileFixtureRoot(t), "ready")
	t.Setenv("LO_TEST_PROBE_READY", ready)
	root := fileFixtureRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err, _ := injectedFileProgram(t, ctx, root, fmt.Sprintf(`return File.open_paths(File.Path.new(%q))`, root), adapter.NativeFileIO())
		done <- err
	}()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
waiting:
	for {
		select {
		case err := <-done:
			t.Fatalf("probe exited before cancellation: %v", err)
		case <-deadline.C:
			t.Fatal("probe did not reach wait fixture")
		case <-ticker.C:
			if _, err := os.Stat(ready); err == nil {
				break waiting
			}
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("probe cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("probe did not terminate after cancellation")
	}
}

func TestFileAbsentGrantStaysInert(t *testing.T) {
	for _, operation := range []string{"File.list", "File.open_paths"} {
		t.Run(operation, func(t *testing.T) {
			root := filepath.Join(fileFixtureRoot(t), "created-later")
			filesystem := &scheduledFileIO{FileIO: adapter.NativeFileIO(), openPaths: func(context.Context, *os.Root) ([]byte, error) {
				t.Error("inert grant reached native probe")
				return []byte("n./present.log\n"), nil
			}}
			files, err := adapter.OpenFiles(adapter.FileRoots{List: []string{root}, OpenFiles: []string{root}}, filesystem)
			if err != nil {
				t.Fatal(err)
			}
			defer files.Close()
			if err := os.Mkdir(root, os.ModePerm); err != nil {
				t.Fatal(err)
			}
			fileFixture(t, root, "present.log", "present")
			_, err = evaluate(t, fmt.Sprintf(`return %s(File.Path.new(%q))`, operation, root), files)
			var host execution.HostError
			if !errors.As(err, &host) || host.Code() != diagnostic.CapabilityDenied {
				t.Fatalf("absent grant acquired authority after directory creation: %v", err)
			}
		})
	}
}

func TestFileOpenPathsParsingBudget(t *testing.T) {
	record := installFileProbeFixture(t)
	root := fileFixtureRoot(t)
	t.Setenv("LO_TEST_PROBE_STDOUT", strings.Repeat("p123\n", 100))
	t.Setenv("LO_TEST_PROBE_EXIT", "0")
	path := sourceFile(t, fmt.Sprintf(`flow Main()->List<File.Path>!{return File.open_paths(File.Path.new(%q))};return Main()`, root))
	good := fileOutput(t, run(t, "run", path, "--open-files-root", root, "--max-steps", "1000", "--format", "json"))
	valueJSON(t, good, `[]`)
	if err := os.Remove(record); err != nil {
		t.Fatal(err)
	}
	got := run(t, "run", path, "--open-files-root", root, "--max-steps", "60", "--format", "json")
	if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" {
		t.Fatalf("raw probe parsing bypassed logical steps: %+v", got)
	}
	if _, err := os.Stat(record); err != nil {
		t.Fatalf("limit occurred before raw probe completed: %v", err)
	}
}

func TestFileOpenPathsMissingExecutable(t *testing.T) {
	t.Setenv("PATH", fileFixtureRoot(t))
	root := fileFixtureRoot(t)
	output, err, _ := injectedFileProgram(t, context.Background(), root, fmt.Sprintf(`flow Handle(e: Error) -> String { return e.kind }; flow Main() -> String onError Handle { paths = File.open_paths(File.Path.new(%q)); return "unexpected" }; return Main()`, root), adapter.NativeFileIO())
	if err != nil || output.(value.Text).Text() != "File.OpenFilesFailed" {
		t.Fatalf("missing lsof did not fail explicitly: %v %v", output, err)
	}
}
