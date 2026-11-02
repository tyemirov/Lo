package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type lockCleanupProgram struct {
	execution.Program
	entered chan<- struct{}
	release <-chan struct{}
}

func (p lockCleanupProgram) NewInvocation(ctx context.Context, boundary execution.Boundary, budget execution.Budget, devices device.Config) (execution.Invocation, error) {
	invocation, err := p.Program.NewInvocation(ctx, boundary, budget, devices)
	if err != nil {
		return nil, err
	}
	return &lockCleanupInvocation{Invocation: invocation, entered: p.entered, release: p.release}, nil
}

type lockCleanupInvocation struct {
	execution.Invocation
	entered chan<- struct{}
	release <-chan struct{}
	once    sync.Once
	err     error
}

func (i *lockCleanupInvocation) Close() error {
	i.once.Do(func() {
		close(i.entered)
		<-i.release
		i.err = i.Invocation.Close()
	})
	return i.err
}

func TestProgramLockGoHTTPShutdown(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprintf("timeout=%v", timeout), func(t *testing.T) {
			path := goLockPath(t)
			source := fmt.Sprintf(`flow Handle(request: HTTP.Request) -> HTTP.Response! { line = Console.Line.new("stderr", "active", true); commit line; return HTTP.Response.Text.new(200,"ok") }; flow OnError(error: Error) -> HTTP.Response { return HTTP.Response.Text.new(500,error.kind) }; flow Main() -> Int! { target = Lock.File.new(File.Path.new(%q)); lease = commit target; server = HTTP.Server.new(0,Handle,OnError); commit server; return 0 }; return Main()`, path)
			program := processPolicyProgram(t, source)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered, cleanupEntered, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			if timeout {
				program = lockCleanupProgram{Program: program, entered: cleanupEntered, release: release}
			}
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			ready := make(chan string, 1)
			var stdout, stderr bytes.Buffer
			out := consoleTestSink{write: func(_ context.Context, data []byte) (int, error) {
				var event struct {
					Status  string
					Servers []adapter.ServerInfo
				}
				if err := json.Unmarshal(data, &event); err == nil && event.Status == "ready" && len(event.Servers) == 1 {
					ready <- event.Servers[0].Address
				}
				return stdout.Write(data)
			}}
			errout := consoleTestSink{write: func(output context.Context, data []byte) (int, error) {
				if strings.Contains(string(data), `"event":"console"`) {
					close(entered)
					select {
					case <-output.Done():
						return 0, output.Err()
					case <-release:
					}
				}
				return stderr.Write(data)
			}}
			console, err := adapter.OpenConsole(adapter.ConsoleOptions{Stdout: out, Stderr: errout, Format: adapter.ConsoleJSON})
			if err != nil {
				t.Fatal(err)
			}
			options, err := runner.ParseCompiler([]string{"serve", "lock-shutdown.lo", "--listen", "0", "--lock-file", path, "--format", "json"})
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan int, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				done <- runner.RunContext(ctx, options, program, device.SoftwareFactory{}, nil, console)
			}()
			t.Cleanup(func() {
				cancel()
				unblock()
				select {
				case <-finished:
				case <-time.After(2 * adapter.ShutdownTimeout):
					t.Error("runner cleanup did not finish")
				}
			})
			var address string
			select {
			case address = <-ready:
			case status := <-done:
				t.Fatalf("serve failed: status=%d stderr=%s", status, stderr.String())
			case <-time.After(2 * time.Second):
				t.Fatal("serve did not become ready")
			}
			requestDone := make(chan error, 1)
			go func() {
				client := &http.Client{Timeout: 10 * time.Second}
				response, err := client.Get("http://" + address)
				if err == nil {
					_, readErr := io.Copy(io.Discard, response.Body)
					err = response.Body.Close()
					if readErr != nil {
						err = readErr
					}
				}
				client.CloseIdleConnections()
				requestDone <- err
			}()
			sessionAwait(t, entered, "handler Console write")
			cancel()
			if timeout {
				select {
				case <-cleanupEntered:
				case <-time.After(2 * adapter.ShutdownTimeout):
					t.Fatal("HTTP handler did not enter evaluator cleanup")
				}
				select {
				case err := <-requestDone:
					if err == nil {
						t.Fatal("HTTP request succeeded before forced shutdown")
					}
				case <-time.After(2 * time.Second):
					t.Fatal("HTTP shutdown did not close the active connection")
				}
			}
			select {
			case status := <-done:
				t.Fatalf("serve returned during active handler: %d", status)
			case <-time.After(100 * time.Millisecond):
			}
			goLockContended(t, path, true)
			unblock()
			select {
			case status := <-done:
				want := 0
				if timeout {
					want = 2
				}
				if status != want {
					t.Fatalf("status=%d want=%d stderr=%s", status, want, stderr.String())
				}
			case <-time.After(2 * time.Second):
				t.Fatal("serve did not join handler")
			}
			if !timeout {
				select {
				case err := <-requestDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("HTTP client did not finish")
				}
			}
			goLockContended(t, path, false)
		})
	}
}

func TestProgramLockGoEvaluatorCleanup(t *testing.T) {
	path := goLockPath(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	lockPath, err := value.NewText(types.Path(), path)
	if err != nil {
		t.Fatal(err)
	}
	target, err := value.NewLockFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	program, err := execution.NewProgram(func(session execution.Session) (value.Value, error) {
		if _, err := session.Boundary().Commit(session.Context(), target); err != nil {
			return nil, err
		}
		if err := session.OnClose(func() error {
			close(entered)
			<-release
			return nil
		}); err != nil {
			return nil, err
		}
		cancel()
		return nil, ctx.Err()
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	options, err := runner.ParseCompiler([]string{"run", "lock-cleanup.lo", "--lock-file", path})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	console := newRunnerConsole(t, &stdout, &stderr)
	done := make(chan int, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		done <- runner.RunContext(ctx, options, program, device.SoftwareFactory{}, nil, console)
	}()
	t.Cleanup(func() {
		unblock()
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Error("runner cleanup did not finish")
		}
	})
	sessionAwait(t, entered, "evaluator cleanup")
	select {
	case status := <-done:
		t.Fatalf("runner returned during evaluator cleanup: %d", status)
	case <-time.After(100 * time.Millisecond):
	}
	goLockContended(t, path, true)
	unblock()
	select {
	case status := <-done:
		if status == 0 {
			t.Fatal("canceled evaluation succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not join evaluator cleanup")
	}
	goLockContended(t, path, false)
}
