package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type changingProcessContextBoundary struct {
	execution.Boundary
	change func()
}

func (b changingProcessContextBoundary) Commit(ctx context.Context, target value.Target) (value.Value, error) {
	result, err := b.Boundary.Commit(ctx, target)
	if err == nil {
		b.change()
	}
	return result, err
}

func TestProcessContextInitialInputs(t *testing.T) {
	root := fileFixtureRoot(t)
	laterDirectory := fileFixtureRoot(t)
	t.Chdir(root)
	t.Setenv("LO_F024_INITIAL", "initial")
	files, err := adapter.OpenFiles(adapter.FileRoots{Write: []string{root}}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := files.Close(); err != nil {
			t.Error(err)
		}
	})
	boundary := changingProcessContextBoundary{files, func() { t.Setenv("LO_F024_INITIAL", "changed"); t.Chdir(laterDirectory) }}
	program := processCompositionProgram(t, root, processContextProgram("Record{first: Process.Context, first_value: String, second: Process.Context, second_value: String}", fmt.Sprintf(`initial = Process.Context.current(); target = File.Write.new(File.Path.new(%q), Bytes.new("change")); commit target; later = Process.Context.current(); return Record.new(first: initial, first_value: Process.Context.environment_value(initial, "LO_F024_INITIAL"), second: later, second_value: Process.Context.environment_value(later, "LO_F024_INITIAL"))`, filepath.Join(root, "observation-marker"))))
	output, err := program.Run(context.Background(), boundary, execution.DefaultBudget(), device.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	result, failure := output.(value.Fallible).Outcome()
	if failure != nil {
		t.Fatal(failure)
	}
	for _, field := range []string{"first_value", "second_value"} {
		if actual := result.(value.Record).Field(field).(value.Text).Text(); actual != "initial" {
			t.Fatalf("%s observed %q", field, actual)
		}
	}
	for _, field := range []string{"first", "second"} {
		if actual := result.(value.Record).Field(field).(value.ProcessContext).Directory().Text(); actual != root {
			t.Fatalf("%s directory changed to %q", field, actual)
		}
	}
}

func TestProcessContextInjectedInputs(t *testing.T) {
	root := fileFixtureRoot(t)
	t.Setenv("LO_F024_INITIAL", "ambient")
	key, err := value.NewText(types.String(), "LO_F024_INITIAL")
	if err != nil {
		t.Fatal(err)
	}
	text, err := value.NewText(types.String(), "injected")
	if err != nil {
		t.Fatal(err)
	}
	mapType, err := types.NewMap(types.String(), types.String())
	if err != nil {
		t.Fatal(err)
	}
	environment, err := value.NewMap(mapType, []value.Entry{{Key: key, Value: text}})
	if err != nil {
		t.Fatal(err)
	}
	directory, err := value.NewText(types.Path(), root)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := value.NewProcessContext(environment, directory)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := execution.WithProcessContext(context.Background(), initial)
	if err != nil {
		t.Fatal(err)
	}
	files, err := adapter.OpenFiles(adapter.FileRoots{}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := files.Close(); err != nil {
			t.Error(err)
		}
	})
	program := processCompositionProgram(t, root, processContextProgram("String", `initial = Process.Context.current(); return Process.Context.environment_value(initial, "LO_F024_INITIAL")`))
	output, err := program.Run(ctx, files, execution.DefaultBudget(), device.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	result, failure := output.(value.Fallible).Outcome()
	if failure != nil || result.(value.Text).Text() != "injected" {
		t.Fatalf("injected inputs: %v %v", result, failure)
	}
}

func TestProcessContextDerivedInspection(t *testing.T) {
	root := fileFixtureRoot(t)
	source := processContextProgram("Process.Command", fmt.Sprintf(`initial = Process.Context.current(); patched = Process.Context.override_environment(initial, Map.new(Record.new(key: "CI", value: "1"))); derived = Process.Context.with_directory(patched, File.Path.new(%q)); executable = Process.Executable.new(File.Path.new(%q)); return Process.Command.new(derived, executable, List.new(), Bytes.new(""))`, root, filepath.Join(root, "missing")))
	_, _, output := inspectSource(t, source)
	var document struct {
		Report struct {
			Targets []struct{ Process *processInspection }
		}
	}
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Report.Targets) != 1 || document.Report.Targets[0].Process == nil {
		t.Fatalf("missing Process inputs: %s", output)
	}
	inputs := document.Report.Targets[0].Process.Context
	if inputs.Origin != "derived" || inputs.Directory == nil || inputs.Directory.Value != root || inputs.Base == nil || inputs.Base.Updates == nil || inputs.Base.Base == nil || inputs.Base.Base.Origin != "inherited" {
		t.Fatalf("missing context origin or overrides: %s", output)
	}
}

func TestProcessContextServerSnapshot(t *testing.T) {
	root := fileFixtureRoot(t)
	t.Setenv("LO_F024_INITIAL", "initial")
	ctx := execution.CaptureProcessContext(context.Background())
	files, err := adapter.OpenFiles(adapter.FileRoots{}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := files.Close(); err != nil {
			t.Error(err)
		}
	})
	policy, err := adapter.NewListenPolicy([]uint16{0})
	if err != nil {
		t.Fatal(err)
	}
	program := processCompositionProgram(t, root, `flow Handler(request: HTTP.Request) -> HTTP.Response! { context = Process.Context.current(); message = Process.Context.environment_value(context, "LO_F024_INITIAL"); return HTTP.Response.Text.new(200, message) }; flow Failed(error: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, "failed") }; target = HTTP.Server.new(0, Handler, Failed); commit target`)
	servers, err := adapter.NewServers(program, files, policy, execution.DefaultBudget(), device.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := servers.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if _, err := servers.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LO_F024_INITIAL", "changed")
	response, err := http.Get("http://" + servers.Snapshot()[0].Address)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || string(body) != "initial" {
		t.Fatalf("server context: status=%d body=%q error=%v", response.StatusCode, body, err)
	}
}
