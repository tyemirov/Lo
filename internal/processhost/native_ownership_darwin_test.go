//go:build darwin

package processhost

import (
	"bytes"
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
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"golang.org/x/sys/unix"
)

const ownershipPrivateCause = "private-native-close-payload"

type ownershipCapture struct{ invocation adapter.ProcessInvocation }

func (capture *ownershipCapture) Start(_ context.Context, invocation adapter.ProcessInvocation) (adapter.ProcessChild, error) {
	capture.invocation = invocation
	return nil, errors.New("capture only; no native launch")
}

type ownershipBoundary struct{ execution.Boundary }
type ownershipCreator struct {
	primary Primary
	err     error
}

func (creator ownershipCreator) Create(context.Context, adapter.ProcessInvocation) (Primary, error) {
	return creator.primary, creator.err
}

func ownershipInvocation(t *testing.T) (adapter.ProcessInvocation, string) {
	t.Helper()
	directory := t.TempDir()
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(repository, directory)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatal("native ownership fixture outside Lo")
	}
	path := filepath.Join(directory, "inert-command")
	text, err := source.New(filepath.Join(directory, "ownership.lo"), fmt.Sprintf(`flow Main() -> Process.Result! { executable = Process.Executable.new(File.Path.new(%q)); target = Process.Command.new(executable,List.new("arg"),Map.new(Record.new(key:"NAME",value:"explicit")),File.Path.new(%q),UTF8.Bytes.new("input")); result = commit target; return result }; return Main()`, path, directory))
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
	capture := &ownershipCapture{}
	lifecycle, err := adapter.NewProcessLifecycle(capture, adapter.DefaultProcessLimits())
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := adapter.WithProcessLifecycle(ownershipBoundary{}, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = program.Run(context.Background(), boundary, execution.DefaultBudget(), device.DefaultConfig())
	if capture.invocation == nil {
		t.Fatal("public lifecycle did not produce invocation")
	}
	return capture.invocation, directory
}

func TestNativeOwnershipSetupCloseFailure(t *testing.T) {
	for _, failurePoint := range []string{"pipe", "start"} {
		t.Run(failurePoint, func(t *testing.T) {
			invocation, directory := ownershipInvocation(t)
			var acquired []*os.File
			defer func() {
				for _, file := range acquired {
					_ = file.Close()
				}
			}()
			pipeCalls, closeCalls, startCalls := 0, 0, 0
			creator := &darwinCreator{launcher: filepath.Join(directory, "never-launched"), directory: directory, commands: map[string]struct{}{invocation.Executable(): {}}, roots: []string{directory}}
			creator.setup = &nativeSetup{
				pipe: func() (*os.File, *os.File, error) {
					pipeCalls++
					if failurePoint == "pipe" && pipeCalls == 2 {
						return nil, nil, errors.New("injected pipe acquisition failure")
					}
					read, write, err := os.Pipe()
					if err == nil {
						acquired = append(acquired, read, write)
					}
					return read, write, err
				},
				close: func(file *os.File) error {
					closeCalls++
					if closeCalls == 1 {
						return errors.New(ownershipPrivateCause)
					}
					return file.Close()
				},
				start: func(*exec.Cmd) error { startCalls++; return errors.New("injected start failure; no process") },
			}
			primary, err := creator.Create(context.Background(), invocation)
			if err == nil {
				t.Fatal("setup failure produced successful creation")
			}
			if strings.Contains(err.Error(), ownershipPrivateCause) {
				t.Fatal("raw close cause escaped startup boundary")
			}
			if closeCalls != len(acquired) {
				t.Fatalf("cleanup skipped resources: closes=%d acquired=%d", closeCalls, len(acquired))
			}
			if failurePoint == "pipe" && startCalls != 0 || failurePoint == "start" && startCalls != 1 {
				t.Fatal("unexpected creation callback count")
			}
			if primary == nil {
				t.Fatal("uncertain setup closure was returned as resource-free nil Primary")
			}
			if primary.StartConfirmed() {
				t.Fatal("setup cleanup handle fabricated OS creation")
			}
			supervisor, supervisorErr := NewSupervisor(ownershipCreator{primary: primary, err: err})
			if supervisorErr != nil {
				t.Fatal(supervisorErr)
			}
			child, startErr := supervisor.Start(context.Background(), invocation)
			if startErr != nil {
				t.Fatal(startErr)
			}
			waitContext, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			termination, waitErr := child.Wait(waitContext)
			if errors.Is(waitErr, context.DeadlineExceeded) {
				t.Fatal("resource-only Wait did not settle promptly")
			}
			if termination != nil || waitErr == nil {
				t.Fatal("resource-only owner fabricated target termination")
			}
			if child.StartConfirmed() {
				t.Fatal("supervisor converted cleanup ownership into primary creation")
			}
			if err := primary.Release(context.Background()); err == nil {
				t.Fatal("uncertain setup closure reported successful Release")
			} else if strings.Contains(err.Error(), ownershipPrivateCause) {
				t.Fatal("Release leaked raw close cause")
			}
			if err := primary.Release(context.Background()); err == nil || closeCalls != len(acquired) {
				t.Fatal("setup Release retried a stale descriptor or forgot sticky failure")
			}
		})
	}
}

func ownershipTerminalPrimary(t *testing.T) *darwinPrimary {
	t.Helper()
	port := func() *nativePort {
		read, write, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := write.Close(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = read.Close() })
		return &nativePort{File: read}
	}
	queue, err := unix.Kqueue()
	if err != nil {
		t.Fatal(err)
	}
	termination, err := value.NewProcessExited(value.Int64(0))
	if err != nil {
		t.Fatal(err)
	}
	primary := &darwinPrimary{input: port(), output: port(), diagnostic: port(), frame: port(), configuration: port(), status: port(), wakeRead: port(), wakeWrite: port(), permit: port(), termination: termination, kqueue: queue, reaped: make(chan struct{}), waitReady: make(chan struct{}), releaseGate: make(chan struct{}, 1)}
	primary.exited.Store(true)
	close(primary.reaped)
	close(primary.waitReady)
	t.Cleanup(func() {
		if !primary.kqueueClosed {
			_ = unix.Close(queue)
		}
	})
	return primary
}

func TestNativeOwnershipTransferredCloseFailure(t *testing.T) {
	for failed := 0; failed < 7; failed++ {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			primary := ownershipTerminalPrimary(t)
			var transferred []*os.File
			for index := 0; index < 7; index++ {
				read, write, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := write.Close(); err != nil {
					t.Fatal(err)
				}
				transferred = append(transferred, read)
				t.Cleanup(func() { _ = read.Close() })
			}
			closes := 0
			creator := &darwinCreator{setup: &nativeSetup{close: func(file *os.File) error {
				index := closes
				closes++
				if index == failed {
					return errors.New(ownershipPrivateCause)
				}
				return file.Close()
			}}}
			creator.closeTransferred(primary, transferred)
			if closes != 7 {
				t.Fatal("transfer cleanup stopped before all closes")
			}
			if err := primary.Release(context.Background()); err == nil {
				t.Fatal("forgotten child-side close failure produced successful Release")
			} else {
				var diagnostic bytes.Buffer
				diagnostic.WriteString(err.Error())
				if strings.Contains(diagnostic.String(), ownershipPrivateCause) {
					t.Fatal("Release exposed raw close payload")
				}
			}
			if err := primary.Release(context.Background()); err == nil || closes != 7 {
				t.Fatal("Release retried a stale transferred descriptor or lost sticky failure")
			}
		})
	}
}

func TestNativeOwnershipStopJoinsStatusWithUncertainTransferredWriter(t *testing.T) {
	for _, alreadyReaped := range []bool{false, true} {
		t.Run(fmt.Sprintf("already-reaped-%t", alreadyReaped), func(t *testing.T) {
			primary := ownershipTerminalPrimary(t)
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
			primary.status = &nativePort{File: reader}
			primary.statusJoined = make(chan struct{})
			primary.waitReady = make(chan struct{})
			primary.events = make(chan Event, 16)
			primary.stopRequest = make(chan struct{})
			// This fixture has no OS child or observer. A simulated reaper owns
			// only its completion channel; no signal or wake descriptor is used.
			primary.wakeClosed = true
			if !alreadyReaped {
				primary.reaped = make(chan struct{})
				go func() { <-primary.stopRequest; close(primary.reaped) }()
			}
			closeCalls := 0
			creator := &darwinCreator{setup: &nativeSetup{close: func(*os.File) error {
				closeCalls++
				return errors.New(ownershipPrivateCause)
			}}}
			if err := creator.closeTransferred(primary, []*os.File{writer}); err == nil {
				t.Fatal("injected writer close unexpectedly succeeded")
			}
			go primary.readStatus()
			go func() {
				<-primary.reaped
				<-primary.statusJoined
				close(primary.events)
				close(primary.waitReady)
			}()
			select {
			case <-primary.statusJoined:
				t.Fatal("status reader did not retain the open writer dependency")
			case <-time.After(20 * time.Millisecond):
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := primary.Stop(ctx); err != nil {
				t.Fatalf("owned termination did not settle: %v", err)
			}
			select {
			case <-primary.statusJoined:
			case <-ctx.Done():
				t.Fatal("Stop left status reader blocked on the uncertain parent-owned writer")
			}
			err = primary.Release(ctx)
			if err == nil {
				t.Fatal("joined status reader erased uncertain transferred close evidence")
			}
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				t.Fatalf("Release deadlocked behind its own status reader: %v", err)
			}
			if strings.Contains(err.Error(), ownershipPrivateCause) || closeCalls != 1 {
				t.Fatal("cleanup leaked payload or retried an uncertain descriptor")
			}
		})
	}
}

func ownershipFillPipe(t *testing.T, writer *os.File) {
	t.Helper()
	connection, err := writer.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var fillErr error
	blocked := false
	if err := connection.Control(func(fd uintptr) {
		if err := unix.SetNonblock(int(fd), true); err != nil {
			fillErr = err
			return
		}
		var chunk [4096]byte
		for written := 0; written < 1<<20; {
			count, err := unix.Write(int(fd), chunk[:])
			if err == unix.EAGAIN {
				blocked = true
				return
			}
			if err == unix.EINTR {
				continue
			}
			if err != nil {
				fillErr = err
				return
			}
			if count == 0 {
				fillErr = errors.New("pipe fill made no progress")
				return
			}
			written += count
		}
	}); err != nil {
		t.Fatal(err)
	}
	if fillErr != nil || !blocked {
		t.Fatalf("could not establish blocked protocol pipe: %v", fillErr)
	}
}

func TestNativeOwnershipReapedStopJoinsBlockedProtocolWriter(t *testing.T) {
	for _, name := range []string{"frame", "configuration"} {
		t.Run(name, func(t *testing.T) {
			primary := ownershipTerminalPrimary(t)
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = writer.Close(); _ = reader.Close() })
			ownershipFillPipe(t, writer)
			port := &nativePort{File: writer}
			if name == "frame" {
				primary.frame = port
			} else {
				primary.configuration = port
			}
			primary.frameJoined = make(chan struct{})
			primary.waitReady = make(chan struct{})
			primary.wakeClosed = true
			closes := 0
			creator := &darwinCreator{setup: &nativeSetup{close: func(*os.File) error { closes++; return errors.New(ownershipPrivateCause) }}}
			if err := creator.closeTransferred(primary, []*os.File{reader}); err == nil {
				t.Fatal("uncertain held reader unexpectedly closed")
			}
			writeEntered := make(chan struct{})
			go func() {
				close(writeEntered)
				_, _ = port.Write([]byte("x"))
				_ = port.Close()
				close(primary.frameJoined)
			}()
			go func() { <-primary.reaped; <-primary.frameJoined; close(primary.waitReady) }()
			<-writeEntered
			select {
			case <-primary.frameJoined:
				t.Fatal("protocol writer was not blocked by held reader")
			case <-time.After(20 * time.Millisecond):
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := primary.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case <-primary.frameJoined:
			case <-ctx.Done():
				t.Fatal("already-reaped Stop left its owned protocol writer blocked")
			}
			err = primary.Release(ctx)
			if err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				t.Fatalf("Release lost close evidence or deadlocked on writer join: %v", err)
			}
			if strings.Contains(err.Error(), ownershipPrivateCause) || closes != 1 {
				t.Fatal("cleanup leaked payload or retried held descriptor")
			}
		})
	}
}
