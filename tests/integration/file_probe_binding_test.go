package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type boundFileProbe struct {
	adapter.FileIO
	before func()
}

func (probe *boundFileProbe) ProbeOpenFiles(ctx context.Context, root *os.Root) ([]byte, error) {
	probe.before()
	return probe.FileIO.ProbeOpenFiles(ctx, root)
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
	root, err := os.OpenRoot(fileFixtureRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := adapter.NativeFileIO().ProbeOpenFiles(ctx, root); done <- err }()
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
	root, err := os.OpenRoot(fileFixtureRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	output, err := adapter.NativeFileIO().ProbeOpenFiles(context.Background(), root)
	var status *exec.ExitError
	if len(output) != 0 || !errors.As(err, &status) || status.ExitCode() != 127 {
		t.Fatalf("missing lsof did not fail explicitly: %q %v", output, err)
	}
}
