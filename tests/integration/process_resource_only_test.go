package integration_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/processhost"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

const resourceOnlyPrivateCause = "private-resource-only-close-cause"

type resourceOnlyFixture struct {
	events             chan processhost.Event
	input              io.WriteCloser
	output, diagnostic io.ReadCloser
}
type resourceOnlyWriter struct{}

func (resourceOnlyWriter) Write([]byte) (int, error)                  { return 0, io.ErrClosedPipe }
func (resourceOnlyWriter) Close() error                               { return nil }
func (*resourceOnlyFixture) StartConfirmed() bool                     { return false }
func (primary *resourceOnlyFixture) Stdin() io.WriteCloser            { return primary.input }
func (primary *resourceOnlyFixture) Stdout() io.ReadCloser            { return primary.output }
func (primary *resourceOnlyFixture) Stderr() io.ReadCloser            { return primary.diagnostic }
func (primary *resourceOnlyFixture) Events() <-chan processhost.Event { return primary.events }
func (*resourceOnlyFixture) ArmExecution(context.Context) error {
	return errors.New(resourceOnlyPrivateCause)
}
func (*resourceOnlyFixture) Permit(context.Context) (bool, error) {
	return false, errors.New(resourceOnlyPrivateCause)
}
func (*resourceOnlyFixture) Wait(context.Context) (value.ProcessTermination, error) {
	return nil, errors.New(resourceOnlyPrivateCause)
}
func (*resourceOnlyFixture) Stop(context.Context) error { return nil }
func (*resourceOnlyFixture) Release(context.Context) error {
	return errors.New(resourceOnlyPrivateCause)
}

type resourceOnlyCreator struct{ primary processhost.Primary }

func (creator resourceOnlyCreator) Create(context.Context, adapter.ProcessInvocation) (processhost.Primary, error) {
	return creator.primary, errors.New(resourceOnlyPrivateCause)
}

func TestProcessResourceOnlyFailurePublicProgress(t *testing.T) {
	root := fileFixtureRoot(t)
	events := make(chan processhost.Event)
	close(events)
	primary := &resourceOnlyFixture{events: events, input: resourceOnlyWriter{}, output: io.NopCloser(bytes.NewReader(nil)), diagnostic: io.NopCloser(bytes.NewReader(nil))}
	supervisor, err := processhost.NewSupervisor(resourceOnlyCreator{primary: primary})
	if err != nil {
		t.Fatal(err)
	}
	limits, err := adapter.NewProcessLimits(1024, time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := adapter.NewProcessLifecycle(supervisor, limits)
	if err != nil {
		t.Fatal(err)
	}
	files, err := adapter.OpenFiles(adapter.FileRoots{}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	boundary, err := adapter.WithProcessLifecycle(files, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	program := processCompositionProgram(t, root, `flow Main() -> Process.Result! { `+processGateSource(root)+` result = commit target; return result }; return Main()`)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	output, runErr := program.Run(ctx, boundary, execution.DefaultBudget(), device.DefaultConfig())
	if output != nil || runErr == nil {
		t.Fatal("resource-only cleanup failure produced a result")
	}
	if ctx.Err() != nil {
		t.Fatal("resource-only failure did not settle within its lifecycle")
	}
	progress := execution.ProcessProgress(runErr)
	if progress == nil || progress.Field("start_confirmed") != value.Bool(false) || progress.Field("cleanup_complete") != value.Bool(false) {
		t.Fatalf("resource-only ownership fabricated creation or cleanup: %v", progress)
	}
	var diagnostic bytes.Buffer
	if status := runner.ReportRuntime(&diagnostic, runner.JsonFormat, runErr); status != runner.ExitHost {
		t.Fatalf("uncertain cleanup was not a host failure: %s", diagnostic.String())
	}
	if strings.Contains(diagnostic.String(), resourceOnlyPrivateCause) {
		t.Fatal("public host diagnostic leaked private resource cause")
	}
	processNoSecrets(t, diagnostic.String())
}
