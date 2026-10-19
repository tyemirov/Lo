package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type scheduledFileIO struct {
	adapter.FileIO
	stat      func(*os.Root, string, int) (os.FileInfo, error)
	remove    func(*os.Root, string, int) error
	close     func(*os.Root) error
	readDir   func(*os.File, int) ([]os.DirEntry, error)
	openPaths func(context.Context, *os.Root) ([]byte, error)
	stats     map[string]int
	removals  []string
}

func (f *scheduledFileIO) ProbeOpenFiles(ctx context.Context, root *os.Root) ([]byte, error) {
	if f.openPaths != nil {
		return f.openPaths(ctx, root)
	}
	return f.FileIO.ProbeOpenFiles(ctx, root)
}

func (f *scheduledFileIO) ReadDir(file *os.File, count int) ([]os.DirEntry, error) {
	if f.readDir != nil {
		return f.readDir(file, count)
	}
	return f.FileIO.ReadDir(file, count)
}

type invalidNameEntry struct{ os.DirEntry }

func (invalidNameEntry) Name() string { return string([]byte{0xff}) }

func (f *scheduledFileIO) Lstat(root *os.Root, name string) (os.FileInfo, error) {
	if f.stats == nil {
		f.stats = map[string]int{}
	}
	f.stats[name]++
	if f.stat != nil {
		return f.stat(root, name, f.stats[name])
	}
	return f.FileIO.Lstat(root, name)
}
func (f *scheduledFileIO) Remove(root *os.Root, name string) error {
	f.removals = append(f.removals, name)
	if f.remove != nil {
		return f.remove(root, name, len(f.removals))
	}
	return f.FileIO.Remove(root, name)
}
func (f *scheduledFileIO) CloseDirectory(root *os.Root) error {
	if f.close != nil {
		return f.close(root)
	}
	return f.FileIO.CloseDirectory(root)
}

func injectedFileProgram(t *testing.T, ctx context.Context, root, text string, filesystem adapter.FileIO) (value.Value, error, result) {
	t.Helper()
	files, err := adapter.OpenFiles(adapter.FileRoots{List: []string{root}, Remove: []string{root}, OpenFiles: []string{root}}, filesystem)
	if err != nil {
		t.Fatal(err)
	}
	src, err := source.New("file-failure.lo", text)
	if err != nil {
		t.Fatal(err)
	}
	checked, diagnostic := compiler.Check(src)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	output, err := eval.Run(ctx, checked, files, execution.DefaultBudget(), device.DefaultConfig())
	err = execution.PreserveCleanup(err, files.Close())
	var stderr bytes.Buffer
	status := 0
	if err != nil {
		status = runner.ReportRuntime(&stderr, runner.JsonFormat, err)
	}
	return output, err, result{stderr: stderr.String(), status: status}
}

func progressFields(t *testing.T, got result, completed, total int, stage, kind string) {
	t.Helper()
	var output struct {
		Diagnostics []struct {
			Code     string
			Error    *runtimeValue
			Progress *runtimeValue `json:"removal_progress"`
			Entries  *runtimeValue `json:"removal_entries"`
		}
	}
	if err := json.Unmarshal([]byte(got.stderr), &output); err != nil {
		t.Fatalf("%v: %+v", err, got)
	}
	if len(output.Diagnostics) != 1 {
		t.Fatalf("missing diagnostic: %+v", got)
	}
	d := output.Diagnostics[0]
	var progress runtimeValue
	if kind != "" {
		if d.Error == nil || d.Error.Kind != kind {
			t.Fatalf("error kind: %+v", got)
		}
		var input map[string]runtimeValue
		if err := json.Unmarshal(d.Error.Input.Value, &input); err != nil {
			t.Fatal(err)
		}
		progress = input["progress"]
		if _, ok := input["entries"]; !ok {
			t.Fatalf("missing ordered entries: %+v", got)
		}
	} else {
		if d.Progress == nil || d.Entries == nil {
			t.Fatalf("missing host removal receipt: %+v", got)
		}
		progress = *d.Progress
	}
	var fields map[string]runtimeValue
	if err := json.Unmarshal(progress.Value, &fields); err != nil {
		t.Fatalf("progress: %v: %+v", err, got)
	}
	valueJSON(t, fields["completed_count"], fmt.Sprintf("%q", fmt.Sprint(completed)))
	valueJSON(t, fields["total_count"], fmt.Sprintf("%q", fmt.Sprint(total)))
	valueJSON(t, fields["stage"], fmt.Sprintf("%q", stage))
	if strings.Contains(got.stderr, "ChangedUnixNS") || strings.Contains(got.stderr, "DirectoryIdentity") || strings.Contains(got.stderr, "FileSession") {
		t.Fatalf("opaque provenance in output: %+v", got)
	}
}

func TestFileRemovalFailures(t *testing.T) {
	for _, scenario := range []string{"preflight-change", "per-entry-change", "remove-failure", "cancel-after-remove", "cleanup-failure", "failure-and-cleanup"} {
		t.Run(scenario, func(t *testing.T) {
			root := fileFixtureRoot(t)
			for _, name := range []string{"a", "b", "c", "d"} {
				fileFixture(t, root, name, name)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			filesystem := &scheduledFileIO{FileIO: adapter.NativeFileIO()}
			completed, attempts, stage, kind := 0, 0, "preflight", "File.EntryChanged"
			switch scenario {
			case "preflight-change", "per-entry-change":
				wanted, occurrence := "a", 2
				if scenario == "per-entry-change" {
					wanted, occurrence, completed, attempts, stage = "b", 3, 1, 1, "remove"
				}
				filesystem.stat = func(boundary *os.Root, name string, count int) (os.FileInfo, error) {
					if name == wanted && count == occurrence {
						file, err := boundary.Create(name)
						if err != nil {
							return nil, err
						}
						_, writeErr := file.WriteString("changed")
						if err := errors.Join(writeErr, file.Close()); err != nil {
							return nil, err
						}
					}
					return boundary.Lstat(name)
				}
			case "remove-failure", "failure-and-cleanup":
				completed, attempts, stage, kind = 2, 3, "remove", "File.RemoveFailed"
				filesystem.remove = func(boundary *os.Root, name string, count int) error {
					if count == 3 {
						return errors.New("injected removal failure")
					}
					return boundary.Remove(name)
				}
			case "cancel-after-remove":
				completed, attempts, stage, kind = 1, 1, "remove", ""
				filesystem.remove = func(boundary *os.Root, name string, count int) error {
					if err := boundary.Remove(name); err != nil {
						return err
					}
					cancel()
					return nil
				}
			case "cleanup-failure":
				completed, attempts, stage, kind = 4, 4, "finalize", ""
			}
			if strings.Contains(scenario, "cleanup") {
				filesystem.close = func(boundary *os.Root) error {
					return errors.Join(boundary.Close(), errors.New("injected directory close failure"))
				}
			}
			program := fmt.Sprintf(`flow Clean() -> Int! { entries = File.list(File.Path.new(%q)); removal = File.Remove.new(entries); commit removal; return 7 }; return Clean()`, root)
			_, err, got := injectedFileProgram(t, ctx, root, program, filesystem)
			if err == nil {
				t.Fatal("expected failure")
			}
			progressFields(t, got, completed, 4, stage, kind)
			if len(filesystem.removals) != attempts {
				t.Fatalf("removal attempts %v", filesystem.removals)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 4-completed {
				t.Fatalf("remaining entries %d: %v", len(entries), err)
			}
			if scenario == "cancel-after-remove" && diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.cancelled" {
				t.Fatalf("cancellation classification: %+v", got)
			}
			if strings.Contains(scenario, "cleanup") && !strings.Contains(got.stderr, "injected directory close failure") {
				t.Fatalf("cleanup error lost: %+v", got)
			}
		})
	}
}

func TestFileRemovalRevalidationBoundary(t *testing.T) {
	root := fileFixtureRoot(t)
	fileFixture(t, root, "a", "old")
	sentinel := fileFixture(t, fileFixtureRoot(t), "untouched", "sentinel")
	filesystem := &scheduledFileIO{FileIO: adapter.NativeFileIO()}
	filesystem.stat = func(boundary *os.Root, name string, count int) (os.FileInfo, error) {
		observed, err := boundary.Lstat(name)
		if err != nil {
			return nil, err
		}
		if count == 3 {
			if err := boundary.Remove(name); err != nil {
				return nil, err
			}
			if err := boundary.Symlink(sentinel, name); err != nil {
				return nil, err
			}
		}
		return observed, nil
	}
	program := fmt.Sprintf(`flow Clean() -> String! { entries = File.list(File.Path.new(%q)); removal = File.Remove.new(entries); commit removal; return "done" }; return Clean()`, root)
	_, err, _ := injectedFileProgram(t, context.Background(), root, program, filesystem)
	if err != nil {
		t.Fatal(err)
	}
	// This controlled uncoordinated replacement is outside identity protection.
	if _, err := os.Lstat(filepath.Join(root, "a")); !os.IsNotExist(err) {
		t.Fatalf("replacement still exists: %v", err)
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "sentinel" {
		t.Fatalf("symlink target changed: %q %v", data, err)
	}
}

func TestFileRemovalChangedAndRepeated(t *testing.T) {
	for _, repeat := range []bool{false, true} {
		t.Run(fmt.Sprint(repeat), func(t *testing.T) {
			root := fileFixtureRoot(t)
			name := fileFixture(t, root, "a", "old")
			middle := fmt.Sprintf(`change = File.Append.new(File.Path.new(%q), UTF8.Bytes.new("changed")); commit change;`, name)
			if repeat {
				middle = "commit removal;"
			}
			program := fmt.Sprintf(`flow Clean() -> Int! { entries = File.list(File.Path.new(%q)); removal = File.Remove.new(entries); %s commit removal; return 0 }; return Clean()`, root, middle)
			got := run(t, "run", sourceFile(t, program), "--list-root", root, "--remove-root", root, "--write-root", root, "--format", "json")
			if got.status != 1 {
				t.Fatalf("expected stale observation: %+v", got)
			}
			progressFields(t, got, 0, 1, "preflight", "File.EntryChanged")
		})
	}
}

func TestFileDiscoveryFailures(t *testing.T) {
	for _, scenario := range []string{"invalid-name", "entry-limit", "missing-child"} {
		t.Run(scenario, func(t *testing.T) {
			root := fileFixtureRoot(t)
			filesystem := &scheduledFileIO{FileIO: adapter.NativeFileIO()}
			want := "File.ListFailed"
			switch scenario {
			case "invalid-name":
				fileFixture(t, root, "a", "bad")
				filesystem.readDir = func(file *os.File, count int) ([]os.DirEntry, error) {
					entries, err := file.ReadDir(count)
					if len(entries) != 0 {
						entries[0] = invalidNameEntry{entries[0]}
					}
					return entries, err
				}
				want = "File.InvalidName"
			case "entry-limit":
				want = "runtime.limit"
				for index := 0; index < 10001; index++ {
					fileFixture(t, root, fmt.Sprintf("entry-%05d", index), "")
				}
			case "missing-child":
				fileFixture(t, root, "a", "old")
				filesystem.stat = func(boundary *os.Root, name string, count int) (os.FileInfo, error) {
					if err := boundary.Remove(name); err != nil {
						return nil, err
					}
					return boundary.Lstat(name)
				}
			}
			program := fmt.Sprintf(`return File.list(File.Path.new(%q))`, root)
			_, err, got := injectedFileProgram(t, context.Background(), root, program, filesystem)
			if err == nil || !strings.Contains(got.stderr, want) || got.stdout != "" {
				t.Fatalf("discovery failure %q: %+v", want, got)
			}
		})
	}
}
