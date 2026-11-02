package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	wirebinary "encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type consoleTestSink struct {
	write func(context.Context, []byte) (int, error)
	flush func(context.Context) error
}

func TestConsoleWriterProtocol(t *testing.T) {
	for _, test := range []struct {
		name   string
		closed bool
	}{{"complete", false}, {"closed pipe", true}} {
		t.Run(test.name, func(t *testing.T) {
			ackRead, ackWrite, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer ackRead.Close()
			defer ackWrite.Close()
			liveRead, liveWrite, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer liveRead.Close()
			defer liveWrite.Close()
			outRead, outWrite, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer outRead.Close()
			defer outWrite.Close()
			if test.closed {
				outRead.Close()
			}
			body := []byte("protocol\n")
			request := make([]byte, 9)
			copy(request, "LCW1")
			request[4] = 1
			wirebinary.BigEndian.PutUint32(request[5:], uint32(len(body)))
			request = append(request, body...)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "--lo-console-writer")
			command.Stdin = bytes.NewReader(request)
			command.Stdout = outWrite
			command.Stderr = outWrite
			command.ExtraFiles = []*os.File{ackWrite, liveRead}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if command.ProcessState == nil {
					_ = command.Process.Kill()
					_ = command.Wait()
				}
			}()
			ackWrite.Close()
			liveRead.Close()
			var final [10]byte
			for {
				if _, err := io.ReadFull(ackRead, final[:]); err != nil {
					t.Fatal(err)
				}
				if final[0] != 1 {
					break
				}
			}
			if err := command.Wait(); err != nil {
				t.Fatal(err)
			}
			if test.closed {
				if final[0] != 3 || final[9] != 1 || wirebinary.BigEndian.Uint64(final[1:9]) != 0 {
					t.Fatalf("failure frame=%v", final)
				}
				return
			}
			if final[0] != 2 || final[9] != 0 || wirebinary.BigEndian.Uint64(final[1:9]) != uint64(len(body)) {
				t.Fatalf("completion=%v", final)
			}
			got := make([]byte, len(body))
			if _, err := io.ReadFull(outRead, got); err != nil || !bytes.Equal(got, body) {
				t.Fatalf("output=%q error=%v", got, err)
			}
		})
	}
}

func TestConsoleWriterParentLiveness(t *testing.T) {
	ackRead, ackWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer ackRead.Close()
	defer ackWrite.Close()
	liveRead, liveWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer liveRead.Close()
	defer liveWrite.Close()
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer outRead.Close()
	defer outWrite.Close()
	body := bytes.Repeat([]byte("x"), 1<<20)
	request := make([]byte, 9)
	copy(request, "LCW1")
	request[4] = 1
	wirebinary.BigEndian.PutUint32(request[5:], uint32(len(body)))
	request = append(request, body...)
	command := exec.Command(binary, "--lo-console-writer")
	command.Stdin = bytes.NewReader(request)
	command.Stdout = outWrite
	command.Stderr = outWrite
	command.ExtraFiles = []*os.File{ackWrite, liveRead}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	ackWrite.Close()
	liveRead.Close()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		t.Fatalf("worker exited before parent liveness ended: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	liveWrite.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("parent loss reported success")
		}
	case <-time.After(2 * time.Second):
		command.Process.Kill()
		<-done
		t.Fatal("blocked worker survived parent liveness EOF")
	}
}

func newRunnerConsole(t *testing.T, stdout, stderr *bytes.Buffer) *adapter.Console {
	t.Helper()
	out, err := adapter.NewBufferConsoleSink(stdout)
	if err != nil {
		t.Fatal(err)
	}
	errout, err := adapter.NewBufferConsoleSink(stderr)
	if err != nil {
		t.Fatal(err)
	}
	console, err := adapter.OpenConsole(adapter.ConsoleOptions{Stdout: out, Stderr: errout, MaxBytes: adapter.DefaultConsoleBytes, Format: adapter.ConsoleText})
	if err != nil {
		t.Fatal(err)
	}
	return console
}

func (s consoleTestSink) Write(ctx context.Context, p []byte) (int, error) { return s.write(ctx, p) }
func (s consoleTestSink) Flush(ctx context.Context) error {
	if s.flush != nil {
		return s.flush(ctx)
	}
	return nil
}

func TestConsoleBoundaryOwnership(t *testing.T) {
	t.Run("flush failure metadata", func(t *testing.T) {
		var output bytes.Buffer
		sink := consoleTestSink{write: func(_ context.Context, p []byte) (int, error) { return output.Write(p) }, flush: func(context.Context) error { return errors.New("private sink failure") }}
		console, err := adapter.OpenConsole(adapter.ConsoleOptions{Stdout: sink, Format: adapter.ConsoleText})
		if err != nil {
			t.Fatal(err)
		}
		defer console.Close(context.Background())
		n, err := console.Write(context.Background(), adapter.ConsoleStdout, []byte("data"), true)
		var progress execution.ConsoleWriteError
		if n != 4 || !errors.As(err, &progress) || progress.Stage() != "flush" || progress.BytesWritten() != 4 || progress.Stream() != adapter.ConsoleStdout || err.Error() == "private sink failure" {
			t.Fatalf("progress=%d error=%v", n, err)
		}
	})
	t.Run("partial progress", func(t *testing.T) {
		sink := consoleTestSink{write: func(context.Context, []byte) (int, error) { return 2, io.ErrClosedPipe }}
		console, err := adapter.OpenConsole(adapter.ConsoleOptions{Stdout: sink, MaxBytes: 100, Format: adapter.ConsoleText})
		if err != nil {
			t.Fatal(err)
		}
		defer console.Close(context.Background())
		n, err := console.Write(context.Background(), adapter.ConsoleStdout, []byte("hello"), false)
		if n != 2 || err == nil {
			t.Fatalf("progress=%d error=%v", n, err)
		}
	})
	t.Run("cancelled queued record", func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		sink := consoleTestSink{write: func(ctx context.Context, p []byte) (int, error) {
			once.Do(func() { close(entered) })
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-release:
				return len(p), nil
			}
		}}
		console, err := adapter.OpenConsole(adapter.ConsoleOptions{Stdout: sink, Format: adapter.ConsoleText})
		if err != nil {
			t.Fatal(err)
		}
		defer console.Close(context.Background())
		first := make(chan error, 1)
		go func() {
			_, err := console.Write(context.Background(), adapter.ConsoleStdout, []byte("first"), false)
			first <- err
		}()
		<-entered
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err = console.Write(ctx, adapter.ConsoleStdout, []byte("queued"), false)
		close(release)
		if firstErr := <-first; firstErr != nil {
			t.Fatal(firstErr)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("quota reserves before write", func(t *testing.T) {
		calls := 0
		sink := consoleTestSink{write: func(_ context.Context, p []byte) (int, error) { calls++; return len(p), nil }}
		console, err := adapter.OpenConsole(adapter.ConsoleOptions{Stdout: sink, MaxBytes: 4, Format: adapter.ConsoleText})
		if err != nil {
			t.Fatal(err)
		}
		defer console.Close(context.Background())
		if _, err := console.Write(context.Background(), adapter.ConsoleStdout, []byte("four"), false); err != nil {
			t.Fatal(err)
		}
		if _, err := console.Write(context.Background(), adapter.ConsoleStdout, []byte("x"), false); err == nil {
			t.Fatal("missing quota error")
		}
		if calls != 1 {
			t.Fatalf("sink calls=%d", calls)
		}
	})
}

func TestConsoleTerminalCompletion(t *testing.T) {
	for _, status := range []int64{0, 1, 2} {
		for _, cleanupFailure := range []bool{false, true} {
			t.Run(fmt.Sprintf("status%d-cleanup%v", status, cleanupFailure), func(t *testing.T) {
				terminal, err := value.NewProgramExit(value.Int64(status), value.NewBytes([]byte("out")), value.NewBytes([]byte("err")))
				if err != nil {
					t.Fatal(err)
				}
				program, err := execution.NewProgram(func(session execution.Session) (value.Value, error) {
					if cleanupFailure {
						if err := session.OnClose(func() error { return errors.New("selected cleanup failure") }); err != nil {
							return nil, err
						}
					}
					return terminal, nil
				}, nil, types.ProgramExit())
				if err != nil {
					t.Fatal(err)
				}
				options, err := runner.ParseCompiler([]string{"run", "terminal.lo"})
				if err != nil {
					t.Fatal(err)
				}
				var stdout, stderr bytes.Buffer
				got := runner.RunContext(context.Background(), options, program, device.SoftwareFactory{}, nil, newRunnerConsole(t, &stdout, &stderr))
				if cleanupFailure {
					if got != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
						t.Fatalf("cleanup result status=%d out=%q err=%q", got, stdout.String(), stderr.String())
					}
					return
				}
				if got != int(status) || stdout.String() != "out" || stderr.String() != "err" {
					t.Fatalf("status=%d out=%q err=%q", got, stdout.String(), stderr.String())
				}
			})
		}
	}
}

func TestConsoleProtectedImagesHaveNoExecutionGrant(t *testing.T) {
	root := fileFixtureRoot(t)
	path := filepath.Join(root, "writer-image")
	original := []byte("bound writer image fixture")
	registryByteFixture(t, path, original)
	grant, err := adapter.NewProcessExecutableGrant(path, sha256.Sum256(original))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := adapter.OpenProtectedProcessRegistry(context.Background(), nil, []adapter.ProcessExecutableGrant{grant}, adapter.DefaultProcessRegistryLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	files, err := registry.OpenFiles(adapter.FileRoots{Write: []string{root}}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	hardlink, symlink := filepath.Join(root, "hardlink"), filepath.Join(root, "symlink")
	if err := os.Link(path, hardlink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{path, hardlink, symlink} {
		code := fmt.Sprintf(`flow Main() -> File.WriteReceipt! { target = File.Write.new(File.Path.new(%q), UTF8.Bytes.new("replace")); result = commit target; return result }; return Main()`, target)
		if err := registryFileCommit(t, root, files, code); err == nil {
			t.Fatalf("protected mutation accepted: %s", target)
		}
	}
	driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) {
		t.Error("protected image granted execution")
		return nil, errors.New("unexpected start")
	}}
	wrapped, err := registry.Driver(driver)
	if err != nil {
		t.Fatal(err)
	}
	child, err := wrapped.Start(context.Background(), registryInvocation(t, root, path))
	if child != nil || err == nil || registryStartCount(driver) != 0 {
		t.Fatalf("child=%v error=%v", child, err)
	}
	content, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(content, original) {
		t.Fatalf("protected bytes=%q error=%v", content, err)
	}
}

func TestConsoleRunnerCancellation(t *testing.T) {
	var active atomic.Int32
	sink := consoleTestSink{write: func(ctx context.Context, _ []byte) (int, error) {
		active.Add(1)
		defer active.Add(-1)
		<-ctx.Done()
		return 0, ctx.Err()
	}}
	console, err := adapter.OpenConsole(adapter.ConsoleOptions{Stderr: sink, Format: adapter.ConsoleText})
	if err != nil {
		t.Fatal(err)
	}
	line, err := value.NewConsoleLine(adapter.ConsoleStderr, "blocked", true)
	if err != nil {
		t.Fatal(err)
	}
	program, err := execution.NewProgram(func(session execution.Session) (value.Value, error) {
		return session.Boundary().Commit(session.Context(), line)
	}, nil, types.ConsoleReceipt())
	if err != nil {
		t.Fatal(err)
	}
	options, err := runner.ParseCompiler([]string{"run", "blocked.lo"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	status := runner.RunContext(ctx, options, program, device.SoftwareFactory{}, nil, console)
	if status != runner.ExitHost || active.Load() != 0 || time.Since(start) > 2*time.Second {
		t.Fatalf("status=%d active=%d duration=%s", status, active.Load(), time.Since(start))
	}
}

func TestConsoleReceiptInvariant(t *testing.T) {
	for _, count := range []uint64{0, 1, 1 << 20, (1 << 20) + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			receipt, err := value.NewConsoleReceipt(adapter.ConsoleStdout, count, true)
			valid := count > 0 && count <= 1<<20
			if valid != (err == nil) || valid != (receipt != nil) {
				t.Fatalf("count=%d receipt=%v error=%v", count, receipt, err)
			}
		})
	}
}

func TestConsoleFinalRenderingFlush(t *testing.T) {
	for _, test := range []struct {
		name     string
		terminal bool
		code     int64
		fail     bool
	}{{"ordinary", false, 0, false}, {"terminal0", true, 0, false}, {"terminal1", true, 1, false}, {"terminal2", true, 2, false}, {"terminal0 failure", true, 0, true}, {"terminal1 failure", true, 1, true}, {"terminal2 failure", true, 2, true}} {
		t.Run(test.name, func(t *testing.T) {
			var pending, delivered, stderr bytes.Buffer
			flushes := 0
			sink := consoleTestSink{write: func(_ context.Context, p []byte) (int, error) { return pending.Write(p) }, flush: func(context.Context) error {
				flushes++
				if test.fail && flushes == 2 {
					return errors.New("final flush failure")
				}
				_, err := delivered.Write(pending.Bytes())
				pending.Reset()
				return err
			}}
			errout, err := adapter.NewBufferConsoleSink(&stderr)
			if err != nil {
				t.Fatal(err)
			}
			console, err := adapter.OpenConsole(adapter.ConsoleOptions{Stdout: sink, Stderr: errout, Format: adapter.ConsoleJSON})
			if err != nil {
				t.Fatal(err)
			}
			var output value.Value = value.Int64(7)
			resultType := types.Int()
			if test.terminal {
				terminal, err := value.NewProgramExit(value.Int64(test.code), value.NewBytes([]byte("out")), value.NewBytes(nil))
				if err != nil {
					t.Fatal(err)
				}
				output = terminal
				resultType = types.ProgramExit()
			}
			program, err := execution.NewProgram(func(execution.Session) (value.Value, error) { return output, nil }, nil, resultType)
			if err != nil {
				t.Fatal(err)
			}
			options, err := runner.ParseCompiler([]string{"run", "flush.lo", "--format", "json"})
			if err != nil {
				t.Fatal(err)
			}
			status := runner.RunContext(context.Background(), options, program, device.SoftwareFactory{}, nil, console)
			if test.fail {
				if status != 2 || stderr.Len() == 0 || flushes != 2 {
					t.Fatalf("status=%d flushes=%d stderr=%q", status, flushes, stderr.String())
				}
				return
			}
			if status != int(test.code) || pending.Len() != 0 || delivered.Len() == 0 || flushes == 0 {
				t.Fatalf("status=%d flushes=%d pending=%q delivered=%q", status, flushes, pending.String(), delivered.String())
			}
		})
	}
}

func TestConsoleServeReadinessStopDuringFlush(t *testing.T) {
	program := processPolicyProgram(t, `flow Handle(request: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(status: 200, body: "ok") }; flow Failure(error: Error) -> HTTP.Response { return HTTP.Response.Text.new(status: 500, body: "error") }; server = HTTP.Server.new(port: 0, handler: Handle, onError: Failure); commit server`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	flushes := 0
	sink := consoleTestSink{
		write: func(_ context.Context, p []byte) (int, error) { return stdout.Write(p) },
		flush: func(output context.Context) error {
			flushes++
			if flushes == 1 {
				cancel()
			}
			return output.Err()
		},
	}
	errout, err := adapter.NewBufferConsoleSink(&stderr)
	if err != nil {
		t.Fatal(err)
	}
	console, err := adapter.OpenConsole(adapter.ConsoleOptions{Stdout: sink, Stderr: errout, Format: adapter.ConsoleJSON})
	if err != nil {
		t.Fatal(err)
	}
	options, err := runner.ParseCompiler([]string{"serve", "readiness.lo", "--listen", "0", "--format", "json"})
	if err != nil {
		t.Fatal(err)
	}
	status := runner.RunContext(ctx, options, program, device.SoftwareFactory{}, nil, console)
	if status != 0 || stderr.Len() != 0 || flushes != 2 {
		t.Fatalf("ready-to-stop ownership: status=%d flushes=%d stdout=%s stderr=%s", status, flushes, stdout.String(), stderr.String())
	}
	events := cliStackJSON(t, stdout.String())
	if len(events) != 2 || events[0]["status"] != "ready" || events[1]["status"] != "stopped" {
		t.Fatalf("server protocol: %#v", events)
	}
}

func TestConsoleServeReadinessBlockedStop(t *testing.T) {
	program := processPolicyProgram(t, `flow Handle(request: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(status: 200, body: "ok") }; flow Failure(error: Error) -> HTTP.Response { return HTTP.Response.Text.new(status: 500, body: "error") }; server = HTTP.Server.new(port: 0, handler: Handle, onError: Failure); commit server`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stderr bytes.Buffer
	var active atomic.Int32
	sink := consoleTestSink{write: func(output context.Context, _ []byte) (int, error) {
		active.Add(1)
		defer active.Add(-1)
		cancel()
		<-output.Done()
		return 0, output.Err()
	}}
	errout, err := adapter.NewBufferConsoleSink(&stderr)
	if err != nil {
		t.Fatal(err)
	}
	console, err := adapter.OpenConsole(adapter.ConsoleOptions{Stdout: sink, Stderr: errout, Format: adapter.ConsoleJSON})
	if err != nil {
		t.Fatal(err)
	}
	options, err := runner.ParseCompiler([]string{"serve", "readiness.lo", "--listen", "0", "--format", "json"})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	status := runner.RunContext(ctx, options, program, device.SoftwareFactory{}, nil, console)
	if status != runner.ExitHost || active.Load() != 0 || stderr.Len() == 0 || time.Since(started) > 3*time.Second {
		t.Fatalf("blocked readiness ownership: status=%d active=%d stderr=%s duration=%s", status, active.Load(), stderr.String(), time.Since(started))
	}
}
