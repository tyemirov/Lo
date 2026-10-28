package integration_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/runtimeops"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
			got := fileOutput(t, run(t, args...))
			valueJSON(t, got, `"File.InvalidSelection"`)
			if _, err := os.Stat(filepath.Join(root, "a")); err != nil {
				t.Fatalf("construction mutated file: %v", err)
			}
		})
	}
	empty := sourceFile(t, `flow Main() -> String! {pending = File.Remove.new(List.new()); commit pending; return "empty"}; return Main()`)
	got := fileOutput(t, run(t, "run", empty, "--format", "json"))
	valueJSON(t, got, `"empty"`)
}

func TestFileLanguageSessionIsolation(t *testing.T) {
	root := fileFixtureRoot(t)
	fileFixture(t, root, "a", "content")
	files, err := adapter.OpenFiles(adapter.FileRoots{List: []string{root}, Remove: []string{root}}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	path, err := value.NewText(types.Path(), root)
	if err != nil {
		t.Fatal(err)
	}
	var observed value.List
	var empty value.RemovalEffect
	first, err := execution.NewProgram(func(session execution.Session) (value.Value, error) {
		var err error
		observed, err = files.List(session.Context(), path)
		if err != nil {
			return nil, err
		}
		emptyList, err := value.NewList(fileLanguageListType(t), nil)
		if err != nil {
			return nil, err
		}
		empty, err = value.NewRemovalEffect(session.FileSession(), emptyList)
		return value.Int64(0), err
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Run(context.Background(), files, execution.DefaultBudget(), device.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	second, err := execution.NewProgram(func(session execution.Session) (value.Value, error) {
		if _, err := value.NewRemovalEffect(session.FileSession(), observed); err == nil {
			t.Error("foreign entry accepted by construction")
		}
		_, err := files.Commit(session.Context(), empty)
		var file execution.FileError
		if !errors.As(err, &file) || file.Kind() != "File.InvalidSelection" {
			t.Errorf("foreign empty removal: %v", err)
		}
		return value.Int64(0), nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Run(context.Background(), files, execution.DefaultBudget(), device.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
}
func fileLanguageListType(t *testing.T) types.List {
	t.Helper()
	typ, err := types.NewList(types.FileEntry())
	if err != nil {
		t.Fatal(err)
	}
	return typ
}

type fileLanguageCloseObserver struct {
	adapter.FileIO
	closed int
}

func (observer *fileLanguageCloseObserver) CloseDirectory(root *os.Root) error {
	observer.closed++
	return observer.FileIO.CloseDirectory(root)
}
func TestFileLanguageDirectoryBudget(t *testing.T) {
	base := fileFixtureRoot(t)
	roots := make([]string, 129)
	for index := range roots {
		root, err := os.MkdirTemp(base, fmt.Sprintf("root-%03d-", index))
		if err != nil {
			t.Fatal(err)
		}
		roots[index] = root
	}
	observer := &fileLanguageCloseObserver{FileIO: adapter.NativeFileIO()}
	files, err := adapter.OpenFiles(adapter.FileRoots{List: roots}, observer)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	program, err := execution.NewProgram(func(session execution.Session) (value.Value, error) {
		for _, root := range roots[:128] {
			path, err := value.NewText(types.Path(), root)
			if err != nil {
				return nil, err
			}
			if _, err := files.List(session.Context(), path); err != nil {
				return nil, err
			}
		}
		repeat, err := value.NewText(types.Path(), roots[0])
		if err != nil {
			return nil, err
		}
		if _, err := files.List(session.Context(), repeat); err != nil {
			return nil, err
		}
		next, err := value.NewText(types.Path(), roots[128])
		if err != nil {
			return nil, err
		}
		_, err = files.List(session.Context(), next)
		return nil, err
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = program.Run(context.Background(), files, execution.DefaultBudget(), device.DefaultConfig())
	var host execution.HostError
	if !errors.As(err, &host) || host.Code() != diagnostic.RuntimeLimit {
		t.Fatalf("129th observed directory: %v", err)
	}
	if observer.closed != 128 {
		t.Fatalf("closed %d retained directories, want 128", observer.closed)
	}
}

func TestFileLanguageLateFailureProgress(t *testing.T) {
	for _, kind := range []string{"steps", "value"} {
		t.Run(kind, func(t *testing.T) {
			root := fileFixtureRoot(t)
			fileFixture(t, root, "a", "content")
			files, err := adapter.OpenFiles(adapter.FileRoots{List: []string{root}, Remove: []string{root}}, adapter.NativeFileIO())
			if err != nil {
				t.Fatal(err)
			}
			defer files.Close()
			program, err := execution.NewProgram(func(session execution.Session) (value.Value, error) {
				path, err := value.NewText(types.Path(), root)
				if err != nil {
					return nil, err
				}
				entries, err := files.List(session.Context(), path)
				if err != nil {
					return nil, err
				}
				effect, err := value.NewRemovalEffect(session.FileSession(), entries)
				if err != nil {
					return nil, err
				}
				if _, err := files.Commit(session.Context(), effect); err != nil {
					return nil, err
				}
				if kind == "value" {
					return nil, session.ChargeValue(value.NewBytes(make([]byte, execution.MaxValueWeight)))
				}
				return nil, session.ReserveFile(execution.DefaultSteps+1, 0)
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = program.Run(context.Background(), files, execution.DefaultBudget(), device.DefaultConfig())
			entries, progress, ok := execution.RemovalDetails(err)
			if !ok || entries.Len() != 1 || progress.Field("completed_count").(value.Integer).Big().Int64() != 1 || progress.Field("stage").(value.Text).Text() != "finalize" {
				t.Fatalf("late failure lost completion receipt: %v (%v)", err, progress)
			}
		})
	}
}

func TestFileLanguageHTTPDisconnectProgress(t *testing.T) {
	root := fileFixtureRoot(t)
	fileFixture(t, root, "a", "content")
	files, err := adapter.OpenFiles(adapter.FileRoots{List: []string{root}, Remove: []string{root}}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	fixture, err := source.New("file-request.lo", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	span := fixture.Span(0, 7)
	removed := make(chan struct{})
	handler := execution.Handler{ID: "H", Name: "H", Parameters: []types.Parameter{{Name: "request", Type: types.Request()}}, Span: span, Call: func(session execution.Session, _ []value.Value, _ source.Span) (value.Value, error) {
		path, err := value.NewText(types.Path(), root)
		if err != nil {
			return nil, err
		}
		entries, err := files.List(session.Context(), path)
		if err != nil {
			return nil, err
		}
		effect, err := value.NewRemovalEffect(session.FileSession(), entries)
		if err != nil {
			return nil, err
		}
		if _, err := files.Commit(session.Context(), effect); err != nil {
			return nil, err
		}
		close(removed)
		<-session.Context().Done()
		return nil, session.Step()
	}}
	program, err := execution.NewProgram(func(session execution.Session) (value.Value, error) {
		target, err := value.NewServer(value.Int64(0), "H", "E")
		if err != nil {
			return nil, err
		}
		return session.Boundary().Commit(session.Context(), target)
	}, []execution.Handler{handler})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := adapter.NewListenPolicy([]uint16{0})
	if err != nil {
		t.Fatal(err)
	}
	servers, err := adapter.NewServers(program, files, policy, execution.DefaultBudget(), device.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := servers.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	if _, err := servers.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+servers.Snapshot()[0].Address+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() {
		response, _ := http.DefaultClient.Do(request)
		if response != nil {
			response.Body.Close()
		}
		close(finished)
	}()
	select {
	case <-removed:
	case <-time.After(time.Second):
		t.Fatal("request did not remove fixture")
	}
	cancel()
	select {
	case failure := <-servers.Failures():
		_, progress, ok := execution.RemovalDetails(failure)
		if !ok || progress.Field("completed_count").(value.Integer).Big().Int64() != 1 {
			t.Fatalf("disconnected request lost receipt: %v", failure)
		}
	case <-time.After(time.Second):
		t.Fatal("disconnected request suppressed removal completion diagnostic")
	}
	<-finished
}

func TestFileLanguageConstructionBudget(t *testing.T) {
	for _, missing := range []uint64{1, 0} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			files, err := adapter.OpenFiles(adapter.FileRoots{}, adapter.NativeFileIO())
			if err != nil {
				t.Fatal(err)
			}
			defer files.Close()
			program, err := execution.NewProgram(func(session execution.Session) (value.Value, error) {
				entries, err := value.NewList(fileLanguageListType(t), nil)
				if err != nil {
					return nil, err
				}
				expectedWeight := entries.Weight() + 32 + uint64(len(catalog.FileRemove))
				session.AddWeight(execution.MaxEvaluationWeight - expectedWeight + missing)
				fixture, err := source.New("budget.lo", "fixture")
				if err != nil {
					return nil, err
				}
				op, _ := catalog.Lookup(catalog.FileRemove)
				call, err := runtimeops.NewCall(op.Output(), fixture.Span(0, 7), op, nil)
				if err != nil {
					return nil, err
				}
				result, err := runtimeops.FileRemove(session, call, []value.Value{entries}, nil)
				if missing == 1 {
					if err == nil || result != nil {
						t.Errorf("budget should reject before effect allocation: %v, %v", result, err)
					}
					return nil, err
				}
				if err != nil {
					return nil, err
				}
				if err := session.ChargeValue(result); err != nil {
					return nil, err
				}
				return value.Int64(0), nil
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = program.Run(context.Background(), files, execution.DefaultBudget(), device.DefaultConfig())
			if missing == 0 && err != nil {
				t.Fatal(err)
			}
			if missing == 1 {
				var host execution.HostError
				if !errors.As(err, &host) || host.Code() != diagnostic.RuntimeLimit {
					t.Fatalf("construction budget: %v", err)
				}
			}
		})
	}
}
