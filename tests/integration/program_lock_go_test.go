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
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/processhost"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func goLockPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(fileFixtureRoot(t), "lease")
	registryByteFixture(t, path, []byte("unchanged"))
	return path
}
func goLockConfig(t *testing.T, path string) processhost.Config {
	t.Helper()
	policy, err := adapter.NewProcessPolicy(adapter.ProcessPolicyInput{})
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := processhost.NewConfig(policy, adapter.DefaultProcessLimits())
	if err != nil {
		t.Fatal(err)
	}
	return goLockPolicy(t, configuration, path)
}
func goLockPolicy(t *testing.T, configuration processhost.Config, path string) processhost.Config {
	t.Helper()
	policy, err := adapter.NewLockPolicy([]string{path}, false)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err = processhost.WithLockPolicy(configuration, policy)
	if err != nil {
		t.Fatal(err)
	}
	return configuration
}
func goLockAcquire(t *testing.T, session processhost.Session, path string) {
	t.Helper()
	program := processPolicyProgram(t, fmt.Sprintf(`flow Main() -> Lock.Lease! { target=Lock.File.new(File.Path.new(%q)); lease=commit target; return lease }; return Main()`, path))
	output, err := program.Run(context.Background(), session.Boundary(), execution.DefaultBudget(), device.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if output == nil {
		t.Fatal("missing lease metadata")
	}
}
func goLockContended(t *testing.T, path string, want bool) {
	t.Helper()
	file, err := adapter.NativeLockIO().Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	err = adapter.NativeLockIO().TryExclusive(file)
	if (err != nil) != want {
		t.Fatalf("contention=%v want=%v", err, want)
	}
}

func TestProgramLockGoMissingEntry(t *testing.T) {
	program := processPolicyProgram(t, `return Program.path()`)
	options, err := runner.ParseCompiler([]string{"run", "untrusted-source-metadata.lo"})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	status := runner.RunContext(context.Background(), options, program, device.SoftwareFactory{}, nil, newRunnerConsole(t, &stdout, &stderr))
	if status != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "Program.EntryUnavailable") {
		t.Fatalf("status=%d out=%q err=%q", status, stdout.String(), stderr.String())
	}
}

func TestProgramLockGoNonregularIdentity(t *testing.T) {
	entry, err := execution.OpenProgramEntry(fileFixtureRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer entry.Close()
	ctx, err := execution.WithProgramEntry(context.Background(), entry)
	if err != nil {
		t.Fatal(err)
	}
	program := processPolicyProgram(t, `return Program.path()`)
	options, err := runner.ParseCompiler([]string{"run", "injected-entry.lo"})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	status := runner.RunContext(ctx, options, program, device.SoftwareFactory{}, nil, newRunnerConsole(t, &stdout, &stderr))
	if status != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "Program.EntryUnavailable") {
		t.Fatalf("status=%d out=%q err=%q", status, stdout.String(), stderr.String())
	}
}

func TestProgramLockGoSourceCapture(t *testing.T) {
	path := sourceFile(t, `flow Main() -> Lock.Lease! { target=Lock.File.new(Program.path()); lease=commit target; return lease }; return Main()`)
	entry, err := execution.OpenProgramEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	defer entry.Close()
	first, err := entry.ReadSource()
	if err != nil {
		t.Fatal(err)
	}
	second, err := entry.ReadSource()
	if err != nil || first != second {
		t.Fatalf("source capture changed: %v", err)
	}
	program := processPolicyProgram(t, first)
	if err := os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	registryByteFixture(t, path, []byte("replacement"))
	ctx, err := execution.WithProgramEntry(context.Background(), entry)
	if err != nil {
		t.Fatal(err)
	}
	options, err := runner.ParseCompiler([]string{"run", path, "--lock-entry"})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	status := runner.RunContext(ctx, options, program, device.SoftwareFactory{}, nil, newRunnerConsole(t, &stdout, &stderr))
	if status != 2 || stdout.Len() != 0 {
		t.Fatalf("replacement admitted status=%d out=%q err=%q", status, stdout.String(), stderr.String())
	}
}

type goFailLockClose struct {
	adapter.LockIO
	calls atomic.Int32
}

func (io *goFailLockClose) Close(file *os.File) error {
	io.calls.Add(1)
	return errors.Join(file.Close(), errors.New("private-close-cause"))
}

func TestProgramLockGoCloseFailure(t *testing.T) {
	path := goLockPath(t)
	filesystem := &goFailLockClose{LockIO: adapter.NativeLockIO()}
	session, err := processhost.OpenTrusted(context.Background(), goLockConfig(t, path), adapter.FileRoots{}, sessionCreator(func(context.Context, adapter.ProcessInvocation) (processhost.Primary, error) {
		return nil, errors.New("unexpected child")
	}), filesystem)
	if err != nil {
		t.Fatal(err)
	}
	goLockAcquire(t, session, path)
	goLockContended(t, path, true)
	closeErr := session.Close(context.Background())
	retainedErr := session.WaitRetained()
	if closeErr == nil || retainedErr == nil || filesystem.calls.Load() != 2 || strings.Contains(closeErr.Error(), "private-close-cause") {
		t.Fatalf("close=%v retained=%v calls=%d", closeErr, retainedErr, filesystem.calls.Load())
	}
	goLockContended(t, path, false)
}

func TestProgramLockGoDelayedChild(t *testing.T) {
	configuration, root, command := sessionFixture(t)
	path := goLockPath(t)
	configuration = goLockPolicy(t, configuration, path)
	primary := newSessionPrimary()
	primary.events <- processhost.Event{Kind: processhost.Ready}
	session, err := processhost.OpenTrusted(context.Background(), configuration, adapter.FileRoots{}, sessionCreator(func(context.Context, adapter.ProcessInvocation) (processhost.Primary, error) { return primary, nil }), adapter.NativeLockIO())
	if err != nil {
		t.Fatal(err)
	}
	goLockAcquire(t, session, path)
	run := sessionRun(t, root, command, session)
	deadline := time.After(time.Second)
	for primary.permits.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("child did not execute")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := session.Close(cleanup); err == nil {
		t.Fatal("close falsely completed retained child")
	}
	sessionAwait(t, primary.releaseEntered, "child release")
	goLockContended(t, path, true)
	joined := sessionWaitRetained(session)
	sessionStillOwned(t, joined)
	close(primary.allowRelease)
	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("retained cleanup did not finish")
	}
	select {
	case <-run:
	case <-time.After(time.Second):
		t.Fatal("child activation did not finish")
	}
	goLockContended(t, path, false)
}

func TestProgramLockGoSnapshotAuthority(t *testing.T) {
	path := goLockPath(t)
	session, err := processhost.Open(context.Background(), goLockConfig(t, path), adapter.FileRoots{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background())
	text, err := value.NewText(types.Path(), path)
	if err != nil {
		t.Fatal(err)
	}
	target, err := value.NewLockFile(text)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := value.Snapshot(target).(value.LockFile)
	if snapshot.Path() != nil || snapshot.Weight() != 16 {
		t.Fatal("snapshot retained lock path")
	}
	if _, err := session.Boundary().Commit(context.Background(), snapshot); err == nil {
		t.Fatal("snapshot granted lock activation")
	}
}

func TestProgramLockGoInspection(t *testing.T) {
	path := sourceFile(t, `flow Main() -> Lock.Lease! { target=Lock.File.new(Program.path()); lease=commit target; return lease }; return Main()`)
	got := run(t, "inspect", path, "--format", "json")
	var doc struct {
		Report struct {
			Reads []struct {
				Operation string
				Target    struct{ Type string }
			}
			Targets     []struct{ Operation string }
			Activations []json.RawMessage
		}
	}
	if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if got.status != 0 || len(doc.Report.Reads) != 1 || doc.Report.Reads[0].Operation != "Program.path" || doc.Report.Reads[0].Target.Type != "File.Path" || len(doc.Report.Targets) != 1 || doc.Report.Targets[0].Operation != "Lock.File.new" || len(doc.Report.Activations) != 1 || !strings.Contains(got.stdout, "Program.EntryUnavailable") || !strings.Contains(got.stdout, "Lock.acquire") || !strings.Contains(got.stdout, "Lock.Busy") || !strings.Contains(got.stdout, "Lock.Failed") {
		t.Fatalf("inspection metadata: %+v", got)
	}
}

type goChangedLockOpen struct {
	adapter.LockIO
	alternate string
	opens     int
}

func (io *goChangedLockOpen) Open(path string) (*os.File, error) {
	io.opens++
	if io.opens > 1 {
		path = io.alternate
	}
	return io.LockIO.Open(path)
}
func TestProgramLockGoFreshIdentity(t *testing.T) {
	path, alternate := goLockPath(t), goLockPath(t)
	filesystem := &goChangedLockOpen{LockIO: adapter.NativeLockIO(), alternate: alternate}
	session, err := processhost.OpenTrusted(context.Background(), goLockConfig(t, path), adapter.FileRoots{}, sessionCreator(func(context.Context, adapter.ProcessInvocation) (processhost.Primary, error) {
		return nil, errors.New("unexpected child")
	}), filesystem)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background())
	program := processPolicyProgram(t, fmt.Sprintf(`flow Recover(error: Error) -> Int { return Match.new(error.kind, "Lock.Failed": 41, _: 42) }; flow Main() -> Int onError Recover { target=Lock.File.new(File.Path.new(%q)); lease=commit target; return 0 }; return Main()`, path))
	output, err := program.Run(context.Background(), session.Boundary(), execution.DefaultBudget(), device.DefaultConfig())
	if err != nil || value.TextResult(output) != "41" || filesystem.opens != 2 {
		t.Fatalf("output=%v err=%v opens=%d", output, err, filesystem.opens)
	}
	goLockContended(t, path, false)
	goLockContended(t, alternate, false)
}

func TestProgramLockGoEntryInjection(t *testing.T) {
	path := sourceFile(t, `return 0`)
	entry, err := execution.OpenProgramEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	defer entry.Close()
	ctx, err := execution.WithProgramEntry(context.Background(), entry)
	if err != nil {
		t.Fatal(err)
	}
	program := processPolicyProgram(t, `return Program.path()`)
	options, err := runner.ParseCompiler([]string{"run", "forged-source-path.lo"})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	status := runner.RunContext(ctx, options, program, device.SoftwareFactory{}, nil, newRunnerConsole(t, &stdout, &stderr))
	if status != 0 || stdout.String() != path+"\n" || stderr.Len() != 0 {
		t.Fatalf("status=%d out=%q err=%q", status, stdout.String(), stderr.String())
	}
	if _, err := execution.WithProgramEntry(context.Background(), entry); err == nil {
		t.Fatal("closed entry accepted for another invocation")
	}
}

func TestProgramLockGoGrantLimits(t *testing.T) {
	for _, path := range []string{"relative", "/unclean/../path", "/" + strings.Repeat("x", 4096), "/" + strings.Repeat("x/", 256) + "x", "/invalid-\xff"} {
		if _, err := adapter.NewLockPolicy([]string{path}, false); err == nil {
			t.Fatalf("invalid grant admitted %q", path)
		}
	}
	paths := make([]string, 129)
	for index := range paths {
		paths[index] = fmt.Sprintf("/path%d", index)
	}
	if _, err := adapter.NewLockPolicy(paths, false); err == nil {
		t.Fatal("grant quota ignored")
	}
}

func TestProgramLockGoSourceLimit(t *testing.T) {
	path := sourceFile(t, strings.Repeat(" ", (16<<20)+1))
	got := runExecutable(t, binary, "run", path, "--format", "json")
	if got.status != 1 || got.stdout != "" || diagnostics(t, got.stderr).Diagnostics[0].Code != "source.limit" {
		t.Fatalf("source limit changed: %+v", got)
	}
}

func TestProgramLockGoEntrySingleOwner(t *testing.T) {
	path := sourceFile(t, `return 19`)
	entry, err := execution.OpenProgramEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	defer entry.Close()
	ctx, err := execution.WithProgramEntry(context.Background(), entry)
	if err != nil {
		t.Fatal(err)
	}
	configuration := goLockConfig(t, goLockPath(t))
	type outcome struct {
		session processhost.Session
		err     error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	for index := 0; index < 2; index++ {
		go func() {
			<-start
			session, err := processhost.Open(ctx, configuration, adapter.FileRoots{})
			results <- outcome{session, err}
		}()
	}
	close(start)
	var owner processhost.Session
	failures := 0
	for index := 0; index < 2; index++ {
		result := <-results
		if result.err != nil {
			failures++
		} else if owner != nil {
			result.session.Close(context.Background())
			t.Fatal("source adopted by two host lifetimes")
		} else {
			owner = result.session
		}
	}
	if owner == nil || failures != 1 {
		t.Fatalf("owner=%v failures=%d", owner, failures)
	}
	defer owner.Close(context.Background())
	program := processPolicyProgram(t, `return Program.path()`)
	options, err := runner.ParseCompiler([]string{"run", "untrusted-metadata.lo"})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	status := runner.RunContext(ctx, options, program, device.SoftwareFactory{}, nil, newRunnerConsole(t, &stdout, &stderr))
	if status != 2 || stdout.Len() != 0 {
		t.Fatalf("reuse status=%d out=%q err=%q", status, stdout.String(), stderr.String())
	}
	text, err := entry.ReadSource()
	if err != nil || text != "return 19" {
		t.Fatalf("failed adoption closed original owner: text=%q err=%v", text, err)
	}
}

type goLockFaultIO struct {
	adapter.LockIO
	mode      string
	opens     int
	closes    int
	failClose bool
	cause     error
	partial   *os.File
}

func (io *goLockFaultIO) Open(path string) (*os.File, error) {
	io.opens++
	if io.mode == "nil" && io.opens == 2 {
		return nil, nil
	}
	file, err := io.LockIO.Open(path)
	if err != nil {
		return file, err
	}
	if io.mode == "partial" && io.opens == 2 || io.mode == "partial-grant" && io.opens == 1 {
		io.partial = file
		return file, io.cause
	}
	return file, nil
}
func (io *goLockFaultIO) Close(file *os.File) error {
	io.closes++
	err := io.LockIO.Close(file)
	if io.failClose {
		return errors.Join(err, io.cause)
	}
	return err
}

func TestProgramLockGoAcquisitionFaults(t *testing.T) {
	for _, mode := range []string{"partial", "nil", "busy-close"} {
		t.Run(mode, func(t *testing.T) {
			path := goLockPath(t)
			cause := errors.New("private fault cause")
			filesystem := &goLockFaultIO{LockIO: adapter.NativeLockIO(), mode: mode, cause: cause}
			session, err := processhost.OpenTrusted(context.Background(), goLockConfig(t, path), adapter.FileRoots{}, sessionCreator(func(context.Context, adapter.ProcessInvocation) (processhost.Primary, error) {
				return nil, errors.New("unexpected child")
			}), filesystem)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				filesystem.failClose = false
				if err := session.Close(context.Background()); err != nil {
					t.Error(err)
				}
			}()
			if mode == "busy-close" {
				goLockAcquire(t, session, path)
				filesystem.failClose = true
			}
			program := processPolicyProgram(t, fmt.Sprintf(`flow Recover(error: Error) -> Int { return Match.new(error.kind, "Lock.Failed": 41, _: 42) }; flow Main() -> Int onError Recover { target=Lock.File.new(File.Path.new(%q)); lease=commit target; return 0 }; return Main()`, path))
			output, err := program.Run(context.Background(), session.Boundary(), execution.DefaultBudget(), device.DefaultConfig())
			if mode == "partial" {
				if err != nil || value.TextResult(output) != "41" || filesystem.closes != 1 {
					t.Fatalf("partial output=%v err=%v closes=%d", output, err, filesystem.closes)
				}
				if _, err := filesystem.partial.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Fatalf("partial descriptor remains open: %v", err)
				}
				return
			}
			var host execution.HostError
			if output != nil || !errors.As(err, &host) || host.Code() != diagnostic.RuntimeHost || strings.Contains(err.Error(), "private fault cause") {
				t.Fatalf("malformed/cleanup fault recovered: output=%v err=%v", output, err)
			}
			if mode == "busy-close" && (!errors.Is(err, cause) || filesystem.closes != 1) {
				t.Fatalf("close fault evidence lost: %v closes=%d", err, filesystem.closes)
			}
		})
	}
	path := goLockPath(t)
	filesystem := &goLockFaultIO{LockIO: adapter.NativeLockIO(), mode: "partial-grant", cause: errors.New("grant open failure")}
	policy, err := adapter.NewLockPolicy([]string{path}, false)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := adapter.OpenLocks(context.Background(), policy, nil, filesystem)
	if owner != nil || err == nil || filesystem.closes != 1 {
		t.Fatalf("partial grant owner=%v err=%v closes=%d", owner, err, filesystem.closes)
	}
	if _, err := filesystem.partial.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("partial grant descriptor remains open: %v", err)
	}
}

func TestProgramLockGoGrantCloseProvider(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprint(rollback), func(t *testing.T) {
			path := goLockPath(t)
			paths := []string{path}
			if rollback {
				paths = append(paths, path+"-missing")
			}
			cause := errors.New("private-grant-close")
			filesystem := &goLockFaultIO{LockIO: adapter.NativeLockIO(), failClose: true, cause: cause}
			policy, err := adapter.NewLockPolicy(paths, false)
			if err != nil {
				t.Fatal(err)
			}
			owner, err := adapter.OpenLocks(context.Background(), policy, nil, filesystem)
			if rollback {
				if owner != nil || err == nil || !errors.Is(err, cause) || filesystem.closes != 1 {
					t.Fatalf("rollback owner=%v err=%v closes=%d", owner, err, filesystem.closes)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			first, second := owner.Close(), owner.Close()
			if first == nil || second == nil || !errors.Is(first, cause) || filesystem.closes != 1 || strings.Contains(first.Error(), "private-grant-close") {
				t.Fatalf("first=%v second=%v closes=%d", first, second, filesystem.closes)
			}
		})
	}
}

func TestProgramLockGoUnregisteredEmbedding(t *testing.T) {
	for _, serve := range []bool{false, true} {
		t.Run(fmt.Sprint(serve), func(t *testing.T) {
			files, err := adapter.OpenFiles(adapter.FileRoots{}, adapter.NativeFileIO())
			if err != nil {
				t.Fatal(err)
			}
			defer files.Close()
			program := processPolicyProgram(t, `flow Main() -> Lock.Lease! { target=Lock.File.new(File.Path.new("/not-authorized")); lease=commit target; return lease }; return Main()`)
			var runErr error
			if serve {
				policy, err := adapter.NewListenPolicy([]uint16{0})
				if err != nil {
					t.Fatal(err)
				}
				servers, err := adapter.NewServers(program, files, policy, execution.DefaultBudget(), device.DefaultConfig())
				if err != nil {
					t.Fatal(err)
				}
				_, runErr = servers.Start(context.Background())
				defer servers.Shutdown(context.Background())
			} else {
				_, runErr = program.Run(context.Background(), files, execution.DefaultBudget(), device.DefaultConfig())
			}
			var host execution.HostError
			if !errors.As(runErr, &host) || host.Code() != diagnostic.CapabilityDenied {
				t.Fatalf("unregistered lock result=%v", runErr)
			}
		})
	}
}
