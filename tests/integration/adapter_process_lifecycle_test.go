package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type lifecycleDriver struct {
	mu     sync.Mutex
	starts int
	start  func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error)
}

type lifecycleReadPipe struct {
	data     []byte
	err      error
	closeErr error
	closes   atomic.Int32
}

func (p *lifecycleReadPipe) Read(buffer []byte) (int, error) {
	if len(p.data) == 0 {
		return 0, io.EOF
	}
	count := copy(buffer, p.data)
	p.data = p.data[count:]
	return count, p.err
}
func (p *lifecycleReadPipe) Close() error { p.closes.Add(1); return p.closeErr }

type lifecycleInputPipe struct {
	closes  atomic.Int32
	writes  atomic.Int32
	failure error
}

func (p *lifecycleInputPipe) Write(data []byte) (int, error) {
	p.writes.Add(1)
	if p.failure != nil {
		return 2, p.failure
	}
	return len(data), nil
}

func lifecycleCommand(t *testing.T) value.ProcessCommand {
	t.Helper()
	path, err := value.NewText(types.Path(), fileFixtureRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	executable, err := value.NewProcessExecutable(path)
	if err != nil {
		t.Fatal(err)
	}
	argumentType, err := types.NewList(types.String())
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := value.NewList(argumentType, nil)
	if err != nil {
		t.Fatal(err)
	}
	environmentType, err := types.NewMap(types.String(), types.String())
	if err != nil {
		t.Fatal(err)
	}
	environment, err := value.NewMap(environmentType, nil)
	if err != nil {
		t.Fatal(err)
	}
	processContext, err := value.NewProcessContext(environment, path)
	if err != nil {
		t.Fatal(err)
	}
	command, err := value.NewProcessCommand(processContext, executable, arguments, value.NewBytes([]byte("input")))
	if err != nil {
		t.Fatal(err)
	}
	return command
}

type lifecycleEvidence interface {
	error
	Stdout() []byte
	Stderr() []byte
	ProcessProgress() value.Record
}

func lifecycleFailureProgress(t *testing.T, err error) value.Record {
	t.Helper()
	var evidence lifecycleEvidence
	if !errors.As(err, &evidence) {
		t.Fatalf("missing process evidence: %v", err)
	}
	return evidence.ProcessProgress()
}
func lifecycleHostFailure(t *testing.T, output value.Value, err error) {
	t.Helper()
	if output != nil || err == nil {
		t.Fatal("host failure published result")
	}
	var diagnostic bytes.Buffer
	if status := runner.ReportRuntime(&diagnostic, runner.JsonFormat, err); status != 2 {
		t.Fatalf("expected host failure: %s", &diagnostic)
	}
	processNoSecrets(t, diagnostic.String())
}

func TestAdapterProcessLifecycleStartEdges(t *testing.T) {
	for _, name := range []string{"pre-cancel", "nil-child", "partial-start", "missing-pipe", "missing-termination"} {
		t.Run(name, func(t *testing.T) {
			input := &lifecycleInputPipe{}
			stdout := &lifecycleReadPipe{}
			stderr := &lifecycleReadPipe{}
			var waits, releases atomic.Int32
			termination, err := value.NewProcessExited(value.Int64(0))
			if err != nil {
				t.Fatal(err)
			}
			child := &lifecycleChild{input: input, output: stdout, errors: stderr, wait: func(context.Context) (value.ProcessTermination, error) {
				waits.Add(1)
				if name == "missing-termination" {
					return nil, nil
				}
				return termination, nil
			}, stop: func(context.Context) error { return nil }, release: func(context.Context) error { releases.Add(1); return nil }}
			if name == "missing-pipe" {
				child.output = nil
			}
			driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) {
				switch name {
				case "pre-cancel":
					t.Error("canceled execution started")
					return nil, errors.New("unexpected start")
				case "nil-child":
					return nil, errors.New(processStdinSecret)
				case "partial-start":
					return child, errors.New(processStdinSecret)
				}
				return child, nil
			}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "pre-cancel" {
				cancel()
			}
			result, runErr := lifecycleBoundary(t, driver, 1024).Commit(ctx, lifecycleCommand(t))
			if result != nil || runErr == nil {
				t.Fatal("failed start published result")
			}
			progress := lifecycleFailureProgress(t, runErr)
			switch name {
			case "pre-cancel":
				if driver.starts != 0 {
					t.Fatal("pre-cancel reached driver")
				}
				lifecycleHostFailure(t, result, runErr)
			case "nil-child":
				if progress.Field("start_confirmed").(value.Boolean).Bool() || !progress.Field("cleanup_complete").(value.Boolean).Bool() || waits.Load() != 0 || releases.Load() != 0 {
					t.Fatal("nil-child claimed resource ownership")
				}
				processNoSecrets(t, runErr.Error())
			case "partial-start":
				if input.writes.Load() != 0 || input.closes.Load() != 1 || waits.Load() != 1 || releases.Load() != 1 {
					t.Fatal("partial start wrote input or lost ownership")
				}
			default:
				lifecycleHostFailure(t, result, runErr)
				if waits.Load() != 1 || releases.Load() != 1 {
					t.Fatal("invalid driver child not joined/released")
				}
			}
		})
	}
}

func TestAdapterProcessLifecycleInputCountAndCleanupFailure(t *testing.T) {
	for _, releaseFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("release-fails-%t", releaseFails), func(t *testing.T) {
			input := &lifecycleInputPipe{failure: errors.New(processStdinSecret)}
			termination, err := value.NewProcessExited(value.Int64(1))
			if err != nil {
				t.Fatal(err)
			}
			child := &lifecycleChild{input: input, output: &lifecycleReadPipe{}, errors: &lifecycleReadPipe{}, wait: func(context.Context) (value.ProcessTermination, error) { return termination, nil }, stop: func(context.Context) error { return nil }, release: func(context.Context) error {
				if releaseFails {
					return errors.New(processEnvironmentSecret)
				}
				return nil
			}}
			driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) { return child, nil }}
			result, runErr := lifecycleBoundary(t, driver, 1024).Commit(context.Background(), lifecycleCommand(t))
			if result != nil || runErr == nil {
				t.Fatal("failed write returned result")
			}
			progress := lifecycleFailureProgress(t, runErr)
			if progress.Field("stdin_written").(value.Integer).Big().Int64() != 2 || progress.Field("stage").(value.Text).Text() != "stdin" || progress.Field("primary_class").(value.Text).Text() != "execution" {
				t.Fatal("partial write lost count or original classification")
			}
			if releaseFails {
				lifecycleHostFailure(t, result, runErr)
				if progress.Field("cleanup_complete").(value.Boolean).Bool() {
					t.Fatal("failed release reported complete cleanup")
				}
			}
			processNoSecrets(t, runErr.Error())
		})
	}
}

func TestAdapterProcessLifecycleExactOutputLimit(t *testing.T) {
	termination, err := value.NewProcessExited(value.Int64(0))
	if err != nil {
		t.Fatal(err)
	}
	child := &lifecycleChild{input: &lifecycleInputPipe{}, output: &lifecycleReadPipe{data: []byte("four")}, errors: &lifecycleReadPipe{data: []byte("four")}, wait: func(context.Context) (value.ProcessTermination, error) { return termination, nil }, stop: func(context.Context) error { t.Error("exact output limit stopped process"); return nil }, release: func(context.Context) error { return nil }}
	driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) { return child, nil }}
	output, err := lifecycleProgram(t, fileFixtureRoot(t)).Run(context.Background(), lifecycleBoundary(t, driver, 4), execution.DefaultBudget(), device.DefaultConfig())
	result := lifecycleOutcome(t, output, err)
	if string(result.Stdout().Bytes()) != "four" || string(result.Stderr().Bytes()) != "four" {
		t.Fatal("exact per-stream limit lost bytes")
	}
}

func TestAdapterProcessLifecycleDeadlinesJoinWorkers(t *testing.T) {
	for _, cleanupExpires := range []bool{false, true} {
		t.Run(fmt.Sprintf("cleanup-expires-%t", cleanupExpires), func(t *testing.T) {
			stdinReader, stdinWriter := io.Pipe()
			stdoutReader, stdoutWriter := io.Pipe()
			stderrReader, stderrWriter := io.Pipe()
			t.Cleanup(func() { _ = stdinReader.Close(); _ = stdoutWriter.Close(); _ = stderrWriter.Close() })
			stopped := make(chan struct{})
			waitDone := make(chan struct{})
			var releases atomic.Int32
			termination, err := value.NewProcessSignaled(value.Int64(15))
			if err != nil {
				t.Fatal(err)
			}
			child := &lifecycleChild{input: stdinWriter, output: stdoutReader, errors: stderrReader, wait: func(ctx context.Context) (value.ProcessTermination, error) {
				defer close(waitDone)
				if cleanupExpires {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				<-stopped
				return termination, nil
			}, stop: func(ctx context.Context) error {
				if cleanupExpires {
					<-ctx.Done()
					return ctx.Err()
				}
				close(stopped)
				return nil
			}, release: func(ctx context.Context) error {
				releases.Add(1)
				if cleanupExpires {
					return ctx.Err()
				}
				return nil
			}}
			driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) { return child, nil }}
			limits, err := adapter.NewProcessLimits(1024, 10*time.Millisecond, 20*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			lifecycle, err := adapter.NewProcessLifecycle(driver, limits)
			if err != nil {
				t.Fatal(err)
			}
			output, runErr := lifecycle.Execute(context.Background(), lifecycleCommand(t))
			lifecycleHostFailure(t, output, runErr)
			select {
			case <-waitDone:
			default:
				t.Fatal("deadline returned before Wait joined")
			}
			if releases.Load() != 1 {
				t.Fatal("deadline lost resource release")
			}
			progress := lifecycleFailureProgress(t, runErr)
			if progress.Field("primary_class").(value.Text).Text() != "cancelled" {
				t.Fatal("execution deadline lost cancellation classification")
			}
			if progress.Field("cleanup_complete").(value.Boolean).Bool() == cleanupExpires {
				t.Fatal("incorrect cleanup deadline status")
			}
		})
	}
}

func TestAdapterProcessLifecycleCleanupEvidence(t *testing.T) {
	for _, recoveredRelease := range []bool{false, true} {
		t.Run(fmt.Sprintf("recovered-release-%t", recoveredRelease), func(t *testing.T) {
			termination, err := value.NewProcessExited(value.Int64(0))
			if err != nil {
				t.Fatal(err)
			}
			stdout := &lifecycleReadPipe{}
			if !recoveredRelease {
				stdout.closeErr = errors.New(processStdinSecret)
			}
			var releases, stops atomic.Int32
			child := &lifecycleChild{input: &lifecycleInputPipe{}, output: stdout, errors: &lifecycleReadPipe{}, wait: func(context.Context) (value.ProcessTermination, error) { return termination, nil }, stop: func(context.Context) error { stops.Add(1); return nil }, release: func(context.Context) error {
				attempt := releases.Add(1)
				if recoveredRelease && attempt == 1 {
					return errors.New(processEnvironmentSecret)
				}
				return nil
			}}
			driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) { return child, nil }}
			output, runErr := lifecycleBoundary(t, driver, 1024).Commit(context.Background(), lifecycleCommand(t))
			if output != nil || runErr == nil {
				t.Fatal("cleanup failure published result")
			}
			progress := lifecycleFailureProgress(t, runErr)
			if progress.Field("cleanup_complete").(value.Boolean).Bool() != recoveredRelease {
				t.Fatalf("incorrect final cleanup evidence: %v", progress)
			}
			expectedStage := "stdout"
			if recoveredRelease {
				expectedStage = "release"
				lifecycleHostFailure(t, output, runErr)
				var host execution.HostError
				if !errors.As(runErr, &host) {
					t.Fatal("recovered release lost host failure")
				}
				if strings.Contains(host.Error(), "not verified") || strings.Contains(host.Error(), "unverified") {
					t.Fatalf("verified retry cleanup described as unverified: %s", host.Error())
				}
				if releases.Load() != 2 || stops.Load() != 1 {
					t.Fatal("failed initial release did not Stop/release once more")
				}
			}
			if progress.Field("stage").(value.Text).Text() != expectedStage || progress.Field("primary_class").(value.Text).Text() != "execution" {
				t.Fatal("cleanup changed original stage/class")
			}
			processNoSecrets(t, runErr.Error())
		})
	}
}
func (p *lifecycleInputPipe) Close() error { p.closes.Add(1); return nil }

func TestAdapterProcessLifecycleFailures(t *testing.T) {
	for _, test := range []struct {
		name     string
		ordinary bool
	}{
		{"capture-partial", true}, {"stdin-early-close", true}, {"partial-start", true}, {"release", false}, {"output-limit", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fileFixtureRoot(t)
			input := &lifecycleInputPipe{}
			output := &lifecycleReadPipe{data: []byte(processArgumentSecret)}
			stderr := &lifecycleReadPipe{data: []byte(processEnvironmentSecret)}
			var stops, waits, releases atomic.Int32
			termination, err := value.NewProcessExited(value.Int64(0))
			if err != nil {
				t.Fatal(err)
			}
			if test.name == "capture-partial" {
				output.err = errors.New(processStdinSecret)
			}
			if test.name == "stdin-early-close" {
				input.failure = io.ErrClosedPipe
			}
			child := &lifecycleChild{input: input, output: output, errors: stderr, wait: func(context.Context) (value.ProcessTermination, error) { waits.Add(1); return termination, nil }, stop: func(ctx context.Context) error {
				stops.Add(1)
				if ctx.Err() != nil {
					t.Error("Stop received expired context")
				}
				return nil
			}, release: func(ctx context.Context) error {
				releases.Add(1)
				if ctx.Err() != nil {
					t.Error("Release received expired context")
				}
				if test.name == "release" {
					return errors.New(processStdinSecret)
				}
				return nil
			}}
			driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) {
				if test.name == "partial-start" {
					return child, errors.New(processStdinSecret)
				}
				return child, nil
			}}
			limit := 1024
			if test.name == "output-limit" {
				limit = 4
			}
			got, runErr := lifecycleProgram(t, root).Run(context.Background(), lifecycleBoundary(t, driver, limit), execution.DefaultBudget(), device.DefaultConfig())
			if test.ordinary {
				var failure execution.Failure
				if got != nil || !errors.As(runErr, &failure) || failure.Value().Kind() != "Process.ExecutionFailed" {
					t.Fatalf("ordinary failure: output=%v error=%v", got, runErr)
				}
				data, err := json.Marshal(failure.Value())
				if err != nil {
					t.Fatal(err)
				}
				processNoSecrets(t, string(data))
			} else {
				if runErr == nil || got != nil {
					t.Fatal("host failure published a result")
				}
				var diagnostic bytes.Buffer
				if status := runner.ReportRuntime(&diagnostic, runner.JsonFormat, runErr); status != 2 {
					t.Fatalf("host failure status %d", status)
				}
				processNoSecrets(t, diagnostic.String())
			}
			if input.closes.Load() != 1 || output.closes.Load() != 1 || stderr.closes.Load() != 1 || waits.Load() != 1 || releases.Load() < 1 || (test.name != "release" && releases.Load() != 1) {
				t.Fatalf("ownership counts stdin=%d stdout=%d stderr=%d wait=%d release=%d", input.closes.Load(), output.closes.Load(), stderr.closes.Load(), waits.Load(), releases.Load())
			}
			if test.name != "release" && stops.Load() != 1 {
				t.Fatalf("failure did not Stop exactly once: %d", stops.Load())
			}
		})
	}
}

func TestAdapterProcessLifecycleCancellation(t *testing.T) {
	root := fileFixtureRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	t.Cleanup(func() { _ = stdinReader.Close(); _ = stdoutWriter.Close(); _ = stderrWriter.Close() })
	stopped := make(chan struct{})
	var waits, releases atomic.Int32
	termination, err := value.NewProcessSignaled(value.Int64(15))
	if err != nil {
		t.Fatal(err)
	}
	child := &lifecycleChild{input: stdinWriter, output: stdoutReader, errors: stderrReader, wait: func(waitCtx context.Context) (value.ProcessTermination, error) {
		waits.Add(1)
		select {
		case <-stopped:
			return termination, nil
		case <-waitCtx.Done():
			return nil, waitCtx.Err()
		}
	}, stop: func(cleanup context.Context) error {
		if cleanup.Err() != nil {
			t.Error("cleanup inherited canceled context")
		}
		close(stopped)
		return nil
	}, release: func(cleanup context.Context) error {
		if cleanup.Err() != nil {
			t.Error("release inherited canceled context")
		}
		releases.Add(1)
		return nil
	}}
	driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) {
		go func() { _, _ = stdoutWriter.Write([]byte("partial")); cancel() }()
		return child, nil
	}}
	output, runErr := lifecycleProgram(t, root).Run(ctx, lifecycleBoundary(t, driver, 1024), execution.DefaultBudget(), device.DefaultConfig())
	if runErr == nil || output != nil {
		t.Fatal("canceled process published a result")
	}
	var diagnostic bytes.Buffer
	status := runner.ReportRuntime(&diagnostic, runner.JsonFormat, runErr)
	if status != 2 || !strings.Contains(diagnostic.String(), "runtime.cancelled") {
		t.Fatalf("cancellation: %s", &diagnostic)
	}
	if waits.Load() != 1 || releases.Load() != 1 {
		t.Fatalf("cancellation ownership: wait=%d release=%d", waits.Load(), releases.Load())
	}
}

func TestAdapterProcessLifecycleFailureEvidence(t *testing.T) {
	root := fileFixtureRoot(t)
	input := &lifecycleInputPipe{}
	output := &lifecycleReadPipe{data: []byte(processArgumentSecret), err: errors.New(processStdinSecret)}
	stderr := &lifecycleReadPipe{data: []byte(processEnvironmentSecret)}
	termination, err := value.NewProcessExited(value.Int64(9))
	if err != nil {
		t.Fatal(err)
	}
	child := &lifecycleChild{input: input, output: output, errors: stderr, wait: func(context.Context) (value.ProcessTermination, error) { return termination, nil }, stop: func(context.Context) error { return nil }, release: func(context.Context) error { return nil }}
	driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) { return child, nil }}
	path, err := value.NewText(types.Path(), root)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := value.NewProcessExecutable(path)
	if err != nil {
		t.Fatal(err)
	}
	argumentType, err := types.NewList(types.String())
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := value.NewList(argumentType, nil)
	if err != nil {
		t.Fatal(err)
	}
	environmentType, err := types.NewMap(types.String(), types.String())
	if err != nil {
		t.Fatal(err)
	}
	environment, err := value.NewMap(environmentType, nil)
	if err != nil {
		t.Fatal(err)
	}
	processContext, err := value.NewProcessContext(environment, path)
	if err != nil {
		t.Fatal(err)
	}
	command, err := value.NewProcessCommand(processContext, executable, arguments, value.NewBytes([]byte("input")))
	if err != nil {
		t.Fatal(err)
	}
	result, runErr := lifecycleBoundary(t, driver, 1024).Commit(context.Background(), command)
	if result != nil || runErr == nil {
		t.Fatal("capture failure returned result")
	}
	var evidence interface {
		error
		Stdout() []byte
		Stderr() []byte
		ProcessProgress() value.Record
	}
	if !errors.As(runErr, &evidence) {
		t.Fatalf("missing Process failure evidence: %T", runErr)
	}
	if string(evidence.Stdout()) != processArgumentSecret {
		t.Fatal("n>0 + error lost captured bytes")
	}
	copyOfOutput := evidence.Stdout()
	copyOfOutput[0] = 'X'
	if string(evidence.Stdout()) != processArgumentSecret {
		t.Fatal("failure evidence exposed mutable bytes")
	}
	progress := evidence.ProcessProgress()
	for name, kind := range map[string]types.Kind{"stage": types.StringKind, "start_confirmed": types.BoolKind, "stdin_written": types.IntKind, "stdout_bytes": types.IntKind, "stderr_bytes": types.IntKind, "cleanup_complete": types.BoolKind, "primary_class": types.StringKind} {
		field := progress.Field(name)
		if field == nil || field.Type().Kind() != kind {
			t.Fatalf("missing typed process progress %s", name)
		}
	}
	if !progress.Field("start_confirmed").(value.Boolean).Bool() || !progress.Field("cleanup_complete").(value.Boolean).Bool() || progress.Field("primary_class").(value.Text).Text() != "execution" {
		t.Fatal("incorrect process progress classification")
	}
	encoded, err := json.Marshal(progress)
	if err != nil {
		t.Fatal(err)
	}
	processNoSecrets(t, string(encoded))
	processNoSecrets(t, runErr.Error())
}

func (d *lifecycleDriver) Start(ctx context.Context, invocation adapter.ProcessInvocation) (adapter.ProcessChild, error) {
	d.mu.Lock()
	d.starts++
	d.mu.Unlock()
	return d.start(ctx, invocation)
}

type lifecycleChild struct {
	input          io.WriteCloser
	output, errors io.ReadCloser
	wait           func(context.Context) (value.ProcessTermination, error)
	stop, release  func(context.Context) error
}

func (c *lifecycleChild) StartConfirmed() bool  { return true }
func (c *lifecycleChild) Stdin() io.WriteCloser { return c.input }
func (c *lifecycleChild) Stdout() io.ReadCloser { return c.output }
func (c *lifecycleChild) Stderr() io.ReadCloser { return c.errors }
func (c *lifecycleChild) Wait(ctx context.Context) (value.ProcessTermination, error) {
	return c.wait(ctx)
}
func (c *lifecycleChild) Stop(ctx context.Context) error    { return c.stop(ctx) }
func (c *lifecycleChild) Release(ctx context.Context) error { return c.release(ctx) }

type lifecycleWriteCloser struct{ bytes.Buffer }

func (w *lifecycleWriteCloser) Close() error { return nil }

func lifecycleProgram(t *testing.T, root string) execution.Program {
	t.Helper()
	text, err := source.New(filepath.Join(root, "lifecycle.lo"), fmt.Sprintf(`flow Main() -> Process.Result! { executable = Process.Executable.new(File.Path.new(%q)); target = Process.Command.new(executable: executable, arguments: List.new("one two","$VALUE"), context: Process.Context.new(Map.new(Record.new(key:"ONLY",value:"explicit")), File.Path.new(%q)), stdin: UTF8.Bytes.new("input")); outcome = commit target; return outcome }; return Main()`, filepath.Join(root, "absent-executable"), filepath.Join(root, "absent-directory")))
	if err != nil {
		t.Fatal(err)
	}
	checked, diagnostic := compiler.Check(text)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	program, err := eval.Program(checked)
	if err != nil {
		t.Fatal(err)
	}
	return program
}
func lifecycleBoundary(t *testing.T, driver adapter.ProcessDriver, outputBytes int) execution.Boundary {
	t.Helper()
	limits, err := adapter.NewProcessLimits(outputBytes, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := adapter.NewProcessLifecycle(driver, limits)
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := adapter.WithProcessLifecycle(invalidReceiptBoundary{}, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	return boundary
}
func lifecycleOutcome(t *testing.T, output value.Value, err error) value.ProcessResult {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	result, failure := output.(value.Fallible).Outcome()
	if failure != nil {
		t.Fatalf("unexpected execution error: %s", failure.Kind())
	}
	return result.(value.ProcessResult)
}

func TestAdapterProcessLifecycleCompleted(t *testing.T) {
	root := fileFixtureRoot(t)
	for _, signaled := range []bool{false, true} {
		t.Run(fmt.Sprintf("signaled-%t", signaled), func(t *testing.T) {
			stdinReader, stdinWriter := io.Pipe()
			stdoutReader, stdoutWriter := io.Pipe()
			stderrReader, stderrWriter := io.Pipe()
			inputDone := make(chan []byte, 1)
			fixtureDone := make(chan error, 1)
			termination, err := value.NewProcessExited(value.Int64(73))
			if signaled {
				termination, err = value.NewProcessSignaled(value.Int64(15))
			}
			if err != nil {
				t.Fatal(err)
			}
			var released bool
			child := &lifecycleChild{input: stdinWriter, output: stdoutReader, errors: stderrReader, wait: func(ctx context.Context) (value.ProcessTermination, error) {
				select {
				case err := <-fixtureDone:
					return termination, err
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}, stop: func(context.Context) error { return nil }, release: func(context.Context) error { released = true; return nil }}
			driver := &lifecycleDriver{start: func(_ context.Context, inv adapter.ProcessInvocation) (adapter.ProcessChild, error) {
				if inv.Executable() != filepath.Join(root, "absent-executable") || !reflect.DeepEqual(inv.Argv(), []string{filepath.Join(root, "absent-executable"), "one two", "$VALUE"}) || !reflect.DeepEqual(inv.Environment(), []string{"ONLY=explicit"}) || inv.Directory() != filepath.Join(root, "absent-directory") || string(inv.Stdin()) != "input" {
					t.Errorf("invocation did not preserve explicit inputs")
				}
				argv := inv.Argv()
				argv[0] = "changed"
				environment := inv.Environment()
				environment[0] = "changed"
				input := inv.Stdin()
				input[0] = 'X'
				if inv.Argv()[0] == "changed" || inv.Environment()[0] == "changed" || string(inv.Stdin()) != "input" {
					t.Error("invocation exposed mutable storage")
				}
				go func() {
					_, first := stdoutWriter.Write([]byte{0, 255, 'o'})
					closeOut := stdoutWriter.Close()
					_, second := stderrWriter.Write([]byte("error-output"))
					closeErr := stderrWriter.Close()
					data, readErr := io.ReadAll(stdinReader)
					inputDone <- data
					_ = stdinReader.Close()
					if first != nil {
						fixtureDone <- first
					} else if second != nil {
						fixtureDone <- second
					} else if closeOut != nil {
						fixtureDone <- closeOut
					} else if closeErr != nil {
						fixtureDone <- closeErr
					} else {
						fixtureDone <- readErr
					}
				}()
				return child, nil
			}}
			output, runErr := lifecycleProgram(t, root).Run(context.Background(), lifecycleBoundary(t, driver, 1024), execution.DefaultBudget(), device.DefaultConfig())
			result := lifecycleOutcome(t, output, runErr)
			if !released || driver.starts != 1 || !bytes.Equal(result.Stdout().Bytes(), []byte{0, 255, 'o'}) || string(result.Stderr().Bytes()) != "error-output" {
				t.Fatal("incomplete process result")
			}
			if string(<-inputDone) != "input" {
				t.Fatal("stdin changed")
			}
			if result.Termination().Kind().Text() != termination.Kind().Text() {
				t.Fatal("termination changed")
			}
		})
	}
}

func TestAdapterProcessLifecycleWaitsForRelease(t *testing.T) {
	root := fileFixtureRoot(t)
	entered := make(chan struct{})
	allow := make(chan struct{})
	termination, err := value.NewProcessExited(value.Int64(0))
	if err != nil {
		t.Fatal(err)
	}
	child := &lifecycleChild{input: &lifecycleWriteCloser{}, output: io.NopCloser(bytes.NewReader(nil)), errors: io.NopCloser(bytes.NewReader(nil)), wait: func(context.Context) (value.ProcessTermination, error) { return termination, nil }, stop: func(context.Context) error { return nil }, release: func(context.Context) error { close(entered); <-allow; return nil }}
	driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) { return child, nil }}
	boundary := lifecycleBoundary(t, driver, 1024)
	program := lifecycleProgram(t, root)
	done := make(chan error, 1)
	go func() {
		_, err := program.Run(context.Background(), boundary, execution.DefaultBudget(), device.DefaultConfig())
		done <- err
	}()
	<-entered
	select {
	case err := <-done:
		t.Fatalf("published before release: %v", err)
	default:
	}
	close(allow)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAdapterProcessLifecycleStandardGate(t *testing.T) {
	root := fileFixtureRoot(t)
	driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) {
		return nil, fmt.Errorf("unexpected start")
	}}
	boundary, err := adapter.WithProcess(lifecycleBoundary(t, driver, 1024))
	if err != nil {
		t.Fatal(err)
	}
	_, runErr := lifecycleProgram(t, root).Run(context.Background(), boundary, execution.DefaultBudget(), device.DefaultConfig())
	if runErr == nil || driver.starts != 0 {
		t.Fatal("standard gate delegated process start")
	}
}
