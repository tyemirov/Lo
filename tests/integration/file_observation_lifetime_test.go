package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func TestFileObservationScopeLifetime(t *testing.T) {
	base := fileFixtureRoot(t)
	paths := make([]string, 160)
	arguments := []string{}
	for index := range paths {
		root, err := os.MkdirTemp(base, "directory-")
		if err != nil {
			t.Fatal(err)
		}
		paths[index] = fmt.Sprintf("File.Path.new(%q)", root)
		arguments = append(arguments, "--list-root", root)
	}
	text := fmt.Sprintf(`flow Observe(count: Int, path: File.Path) -> Int! { directory = File.directory(path); return Math.add(count, 1) }; return Flow.fold(List.new(%s), 0, Observe)`, strings.Join(paths, ","))
	arguments = append([]string{"run", sourceFile(t, text)}, arguments...)
	arguments = append(arguments, "--format", "json")
	valueJSON(t, fileOutput(t, run(t, arguments...)), `"160"`)
}

func TestFileObservationEscapesThroughRemovalAndGuard(t *testing.T) {
	selected := fileFixtureRoot(t)
	contextRoot := fileFixtureRoot(t)
	fileFixture(t, selected, "selected", "content")
	paths := make([]string, 160)
	arguments := []string{"--list-root", selected, "--remove-root", selected, "--list-root", contextRoot}
	for index := range paths {
		root, err := os.MkdirTemp(fileFixtureRoot(t), "directory-")
		if err != nil {
			t.Fatal(err)
		}
		paths[index] = fmt.Sprintf("File.Path.new(%q)", root)
		arguments = append(arguments, "--list-root", root)
	}
	text := fmt.Sprintf(`flow Guard(directory: File.Directory!, original: File.Entry, current: File.Entry) -> Bool { return Math.eq(List.length(List.new(directory)), 1) }; flow Build() -> File.Remove! { scan = File.scan(File.Path.new(%q)); directory = File.directory(File.Path.new(%q)); return File.Remove.checked(scan.entries, directory, Guard) }; flow Observe(count: Int, path: File.Path) -> Int! { directory = File.directory(path); return Math.add(count, 1) }; flow Main() -> Int! { target = Build(); visited = Flow.fold(List.new(%s), 0, Observe); receipt = commit target; return receipt.completed_count }; return Main()`, selected, contextRoot, strings.Join(paths, ","))
	arguments = append([]string{"run", sourceFile(t, text)}, arguments...)
	arguments = append(arguments, "--format", "json")
	valueJSON(t, fileOutput(t, run(t, arguments...)), `"1"`)
}

func TestFileObservationClosesBeforeNextIterationAndOnCancel(t *testing.T) {
	root := fileFixtureRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed, closed := 0, 0
	native := adapter.NativeFileIO()
	filesystem := &scheduledFileIO{FileIO: native}
	filesystem.stat = func(directory *os.Root, name string, _ int) (os.FileInfo, error) {
		observed++
		if closed != observed-1 {
			t.Errorf("observation %d began with %d released handles", observed, closed)
		}
		if observed == 5 {
			cancel()
		}
		return native.Lstat(directory, name)
	}
	filesystem.close = func(directory *os.Root) error { closed++; return native.CloseDirectory(directory) }
	text := fmt.Sprintf(`flow Observe(count: Int, path: File.Path) -> Int! { directory = File.directory(path); return Math.add(count, 1) }; return Flow.fold(List.new(%s), 0, Observe)`, strings.TrimSuffix(strings.Repeat(fmt.Sprintf("File.Path.new(%q),", root), 10), ","))
	_, err, got := injectedFileProgram(t, ctx, root, text, filesystem)
	if err == nil || got.status != 2 || !strings.Contains(got.stderr, "runtime.cancelled") {
		t.Fatalf("cancelled run: %v, %+v", err, got)
	}
	if observed != 5 || closed != observed+3 {
		t.Fatalf("observations=%d, closes=%d; expected observed handles and three grant roots closed", observed, closed)
	}
}

func TestFileObservationReleasedLeaseCannotReattach(t *testing.T) {
	root := fileFixtureRoot(t)
	selected := fileFixture(t, root, "selected", "content")
	closed := 0
	native := adapter.NativeFileIO()
	filesystem := &scheduledFileIO{FileIO: native, close: func(root *os.Root) error { closed++; return native.CloseDirectory(root) }}
	files, err := adapter.OpenFiles(adapter.FileRoots{List: []string{root}, Remove: []string{root}}, filesystem)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := files.Close(); err != nil {
			t.Error(err)
		}
	}()
	session, err := execution.NewSession(context.Background(), files, execution.DefaultBudget(), device.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			t.Error(err)
		}
	}()
	path, err := value.NewText(types.Path(), root)
	if err != nil {
		t.Fatal(err)
	}
	firstScope := session.Scope()
	first, err := files.List(session.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	firstScope.Keep(value.Snapshot(first))
	if closed != 1 {
		t.Fatalf("public snapshot retained an observation lease: %d closed", closed)
	}
	firstScope.Close(nil)
	secondScope := session.Scope()
	defer secondScope.Close(nil)
	second, err := files.List(session.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if first.Items()[0].(value.FileEntry).Observation().Lease == second.Items()[0].(value.FileEntry).Observation().Lease {
		t.Fatal("reopened directory reused a released lease")
	}
	target, err := value.NewRemovalEffect(session.FileSession(), first)
	if err != nil {
		t.Fatal(err)
	}
	_, err = files.Commit(session.Context(), target)
	var failure execution.FileError
	if !errors.As(err, &failure) || failure.Kind() != "File.InvalidSelection" {
		t.Fatalf("released observation commit: %v", err)
	}
	if _, err := os.Stat(selected); err != nil {
		t.Fatalf("released observation removed current file: %v", err)
	}
}

func TestFileObservationDynamicDirectoryTraversal(t *testing.T) {
	root := fileFixtureRoot(t)
	arguments := []string{"--list-root", root}
	parent := root
	for index := 1; index < 160; index++ {
		created, err := os.MkdirTemp(parent, "")
		if err != nil {
			t.Fatal(err)
		}
		child := filepath.Join(parent, "d")
		if err := os.Rename(created, child); err != nil {
			t.Fatal(err)
		}
		arguments = append(arguments, "--list-root", child)
		parent = child
	}
	text := fmt.Sprintf(`flow Path(entry: File.Entry) -> File.Path { return entry.path }; flow Step(state: Int, path: File.Path) -> Record{state: Int, items: List<File.Path>, stop: Bool}! { scan = File.scan(path); return Record.new(state: Math.add(state, 1), items: List.map(scan.entries, Path), stop: false) }; return Flow.worklist(List.new(File.Path.new(%q)), 0, Step)`, root)
	arguments = append([]string{"run", sourceFile(t, text)}, arguments...)
	arguments = append(arguments, "--format", "json")
	valueJSON(t, fileOutput(t, run(t, arguments...)), `"160"`)
}

func TestFileObservationCallbackCleanupProgress(t *testing.T) {
	for _, operation := range []string{"fold", "worklist"} {
		t.Run(operation, func(t *testing.T) {
			roots := []string{fileFixtureRoot(t), fileFixtureRoot(t), fileFixtureRoot(t)}
			native := adapter.NativeFileIO()
			closes := 0
			filesystem := &scheduledFileIO{FileIO: native, close: func(directory *os.Root) error {
				closes++
				err := native.CloseDirectory(directory)
				if closes == 1 {
					return errors.Join(err, errors.New("injected observation close failure"))
				}
				return err
			}}
			paths := fmt.Sprintf("List.new(File.Path.new(%q), File.Path.new(%q), File.Path.new(%q))", roots[0], roots[1], roots[2])
			const stateType = `Record{count: Int, directories: List<File.Directory>}`
			const initial = `Record.new(count: 0, directories: List.new())`
			text := fmt.Sprintf(`flow Step(state: %s, path: File.Path) -> %s! { directory = File.directory(path); return Record.new(count: Math.add(state.count, 1), directories: List.new(directory)) }; return Flow.fold(%s, %s, Step)`, stateType, stateType, paths, initial)
			if operation == "worklist" {
				text = fmt.Sprintf(`flow Step(state: %s, path: File.Path) -> Record{state: %s, items: List<File.Path>, stop: Bool}! { directory = File.directory(path); return Record.new(state: Record.new(count: Math.add(state.count, 1), directories: List.new(directory)), items: List.new(), stop: false) }; return Flow.worklist(%s, %s, Step)`, stateType, stateType, paths, initial)
			}
			_, err, got := injectedFileProgramWithRoots(t, context.Background(), adapter.FileRoots{List: roots}, text, filesystem, execution.DefaultBudget())
			if err == nil || got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.host-failure" {
				t.Fatalf("callback cleanup: %v, %+v", err, got)
			}
			if filesystem.stats["."] != 2 {
				t.Fatalf("cleanup failure allowed %d callbacks", filesystem.stats["."])
			}
			var document struct {
				Diagnostics []struct {
					Progress []runtimeValue `json:"flow_progress"`
				} `json:"diagnostics"`
			}
			if err := json.Unmarshal([]byte(got.stderr), &document); err != nil {
				t.Fatal(err)
			}
			if len(document.Diagnostics) != 1 || len(document.Diagnostics[0].Progress) != 1 {
				t.Fatalf("missing callback completion progress: %+v", got)
			}
			fields := foldProgressFields(t, document.Diagnostics[0].Progress[0])
			valueJSON(t, fields["completed_count"], `"2"`)
			state := foldProgressFields(t, fields["state"])
			valueJSON(t, state["count"], `"2"`)
			if operation == "worklist" {
				valueJSON(t, fields["pending_count"], `"1"`)
			} else {
				valueJSON(t, fields["next_index"], `"2"`)
				valueJSON(t, fields["total_count"], `"3"`)
			}
		})
	}
}
