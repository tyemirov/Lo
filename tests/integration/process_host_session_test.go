package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/processhost"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// OpenTrusted is the explicit Go-embedding seam for the same session assembly
// used by Open. Config contains no qualification bypass or Creator field.
type sessionCreator func(context.Context, adapter.ProcessInvocation) (processhost.Primary, error)

func (create sessionCreator) Create(ctx context.Context, invocation adapter.ProcessInvocation) (processhost.Primary, error) {
	return create(ctx, invocation)
}

type sessionPrimary struct {
	events                   chan processhost.Event
	stopped                  chan struct{}
	releaseEntered           chan struct{}
	allowRelease             chan struct{}
	stopOnce, releaseOnce    sync.Once
	waits, releases, permits atomic.Int32
}

func newSessionPrimary() *sessionPrimary {
	return &sessionPrimary{events: make(chan processhost.Event, 4), stopped: make(chan struct{}), releaseEntered: make(chan struct{}), allowRelease: make(chan struct{})}
}
func (*sessionPrimary) StartConfirmed() bool                     { return true }
func (*sessionPrimary) Stdin() io.WriteCloser                    { return sessionDiscard{} }
func (*sessionPrimary) Stdout() io.ReadCloser                    { return io.NopCloser(bytes.NewReader(nil)) }
func (*sessionPrimary) Stderr() io.ReadCloser                    { return io.NopCloser(bytes.NewReader(nil)) }
func (primary *sessionPrimary) Events() <-chan processhost.Event { return primary.events }
func (*sessionPrimary) ArmExecution(context.Context) error       { return nil }
func (primary *sessionPrimary) Permit(context.Context) (bool, error) {
	primary.permits.Add(1)
	primary.events <- processhost.Event{Kind: processhost.Executed}
	return true, nil
}
func (primary *sessionPrimary) Wait(ctx context.Context) (value.ProcessTermination, error) {
	primary.waits.Add(1)
	select {
	case <-primary.stopped:
		return value.NewProcessExited(value.Int64(0))
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (primary *sessionPrimary) Stop(context.Context) error {
	primary.stopOnce.Do(func() { close(primary.stopped) })
	return nil
}
func (primary *sessionPrimary) Release(ctx context.Context) error {
	primary.releases.Add(1)
	primary.releaseOnce.Do(func() { close(primary.releaseEntered) })
	select {
	case <-primary.allowRelease:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type sessionDiscard struct{}

func (sessionDiscard) Write(data []byte) (int, error) { return len(data), nil }
func (sessionDiscard) Close() error                   { return nil }

func sessionFixture(t *testing.T) (processhost.Config, string, string) {
	t.Helper()
	root := fileFixtureRoot(t)
	path := filepath.Join(root, "registered-image")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("inert registered session fixture")
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	policy, err := adapter.NewProcessPolicy([]string{fmt.Sprintf("%x,%s", digest, path)}, filepath.Join(root, "absent-launcher"), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	limits, err := adapter.NewProcessLimits(1024, time.Second, 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	config, err := processhost.NewConfig(policy, limits)
	if err != nil {
		t.Fatal(err)
	}
	return config, root, path
}

func sessionRun(t *testing.T, root, path string, session processhost.Session) <-chan error {
	t.Helper()
	program := processCompositionProgram(t, root, fmt.Sprintf(`flow Main() -> Process.Result! { executable = Process.Executable.new(File.Path.new(%q)); arguments = List.new("arg"); environment = Map.new(Record.new(key: "NAME", value: "explicit")); command = Process.Command.new(executable, arguments, environment, File.Path.new(%q), UTF8.Bytes.new("input")); result = commit command; return result }; return Main()`, path, root))
	done := make(chan error, 1)
	go func() {
		_, err := program.Run(context.Background(), session.Boundary(), execution.DefaultBudget(), device.DefaultConfig())
		done <- err
	}()
	return done
}

func sessionAwait(t *testing.T, event <-chan struct{}, subject string) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", subject)
	}
}
func sessionWaitRetained(session processhost.Session) <-chan error {
	done := make(chan error, 1)
	go func() { done <- session.WaitRetained() }()
	return done
}
func sessionStillOwned(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("retained ownership returned before release: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestProcessHostSessionLateCreationRetainsSingleCleanupOwner(t *testing.T) {
	config, root, path := sessionFixture(t)
	entered, canceled, publish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	primary := newSessionPrimary()
	var creates atomic.Int32
	session, err := processhost.OpenTrusted(context.Background(), config, adapter.FileRoots{Read: []string{root}, Write: []string{root}}, sessionCreator(func(ctx context.Context, _ adapter.ProcessInvocation) (processhost.Primary, error) {
		creates.Add(1)
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-publish
		return primary, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	run := sessionRun(t, root, path, session)
	sessionAwait(t, entered, "creation admission")
	closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := session.Close(closeCtx); err == nil {
		t.Fatal("Close claimed settled late creation")
	}
	sessionAwait(t, canceled, "session cancellation")
	retained := sessionWaitRetained(session)
	sessionStillOwned(t, retained)
	readPath, err := value.NewText(types.Path(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Boundary().Read(context.Background(), readPath); err == nil {
		t.Fatal("closed session admitted File read")
	}
	second := sessionRun(t, root, path, session)
	select {
	case err := <-second:
		if err == nil {
			t.Fatal("closed session admitted Process activation")
		}
	case <-time.After(time.Second):
		t.Fatal("closed admission blocked")
	}
	if creates.Load() != 1 {
		t.Fatal("shutdown admitted another creation")
	}
	close(publish)
	sessionAwait(t, primary.stopped, "automatic late Stop without another Close")
	sessionAwait(t, primary.releaseEntered, "retained primary release")
	sessionStillOwned(t, retained)
	close(primary.allowRelease)
	select {
	case <-retained:
	case <-time.After(2 * time.Second):
		t.Fatal("retained cleanup did not settle")
	}
	select {
	case err := <-run:
		if err == nil {
			t.Fatal("shutdown activation falsely succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("active operation not canceled")
	}
	if primary.waits.Load() != 1 || primary.releases.Load() != 1 || primary.permits.Load() != 0 {
		t.Fatalf("ownership counts: wait=%d release=%d permit=%d", primary.waits.Load(), primary.releases.Load(), primary.permits.Load())
	}
	// Historical timeout must not keep WaitRetained blocked once ownership settled.
	select {
	case <-sessionWaitRetained(session):
	case <-time.After(time.Second):
		t.Fatal("historical cleanup error treated as live ownership")
	}
}

func TestProcessHostSessionSettledCreationFailureDoesNotRetainResources(t *testing.T) {
	config, root, path := sessionFixture(t)
	session, err := processhost.OpenTrusted(context.Background(), config, adapter.FileRoots{}, sessionCreator(func(context.Context, adapter.ProcessInvocation) (processhost.Primary, error) {
		return nil, errors.New("private creator payload")
	}))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-sessionRun(t, root, path, session):
		if err == nil {
			t.Fatal("creation failure produced result")
		}
	case <-time.After(time.Second):
		t.Fatal("settled failure blocked")
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-sessionWaitRetained(session):
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("resource-free creation retained owner")
	}
}

func TestProcessHostSessionEmptyPolicyKeepsDefaultGateClosed(t *testing.T) {
	policy, err := adapter.NewProcessPolicy(nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	config, err := processhost.NewConfig(policy, adapter.DefaultProcessLimits())
	if err != nil {
		t.Fatal(err)
	}
	root := fileFixtureRoot(t)
	session, err := processhost.Open(context.Background(), config, adapter.FileRoots{Read: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background())
	program := processPolicyProgram(t, `flow Main() -> Process.Result! { `+processGateSource(root)+` result = commit target; return result }; return Main()`)
	_, runErr := program.Run(context.Background(), session.Boundary(), execution.DefaultBudget(), device.DefaultConfig())
	var host execution.HostError
	if !errors.As(runErr, &host) || host.Code() != execution.ProcessUnavailable().Code() {
		t.Fatalf("default gate did not return unavailable: %v", runErr)
	}
}

func TestProcessHostSessionParentCancellationStartsOwnedShutdown(t *testing.T) {
	configuration, root, path := sessionFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	session, err := processhost.OpenTrusted(ctx, configuration, adapter.FileRoots{}, sessionCreator(func(ctx context.Context, _ adapter.ProcessInvocation) (processhost.Primary, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	if err != nil {
		t.Fatal(err)
	}
	run := sessionRun(t, root, path, session)
	sessionAwait(t, entered, "parent cancellation admission")
	cancel()
	select {
	case <-sessionWaitRetained(session):
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not finish owned shutdown")
	}
	select {
	case err := <-run:
		if err == nil {
			t.Fatal("cancelled activation succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled activation did not return")
	}
}

func TestProcessHostSessionConcurrentCloseSharesCleanup(t *testing.T) {
	configuration, root, path := sessionFixture(t)
	entered := make(chan struct{})
	primary := newSessionPrimary()
	session, err := processhost.OpenTrusted(context.Background(), configuration, adapter.FileRoots{}, sessionCreator(func(context.Context, adapter.ProcessInvocation) (processhost.Primary, error) {
		close(entered)
		return primary, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	run := sessionRun(t, root, path, session)
	sessionAwait(t, entered, "concurrent shutdown admission")
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- session.Close(context.Background()) }()
	go func() { second <- session.Close(context.Background()) }()
	sessionAwait(t, primary.releaseEntered, "single shared release")
	sessionStillOwned(t, first)
	sessionStillOwned(t, second)
	close(primary.allowRelease)
	for _, done := range []<-chan error{first, second, sessionWaitRetained(session)} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("shared shutdown did not settle")
		}
	}
	select {
	case <-run:
	case <-time.After(time.Second):
		t.Fatal("active caller did not return")
	}
	if primary.releases.Load() != 1 || primary.waits.Load() != 1 {
		t.Fatal("concurrent Close duplicated ownership jobs")
	}
}

func TestProcessHostSessionProtectsRegisteredFileAliases(t *testing.T) {
	configuration, root, path := sessionFixture(t)
	alias := filepath.Join(root, "registered-alias")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	var creates atomic.Int32
	session, err := processhost.OpenTrusted(context.Background(), configuration, adapter.FileRoots{Write: []string{root}}, sessionCreator(func(context.Context, adapter.ProcessInvocation) (processhost.Primary, error) {
		creates.Add(1)
		return nil, errors.New("unexpected creation")
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background())
	program := processCompositionProgram(t, root, fmt.Sprintf(`flow Main() -> File.WriteReceipt! { target = File.Write.new(File.Path.new(%q),UTF8.Bytes.new("changed")); receipt = commit target; return receipt }; return Main()`, alias))
	_, runErr := program.Run(context.Background(), session.Boundary(), execution.DefaultBudget(), device.DefaultConfig())
	if runErr == nil {
		t.Fatal("participating File adapter changed a registered executable alias")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "inert registered session fixture" || creates.Load() != 0 {
		t.Fatal("protected identity changed or Process unexpectedly ran")
	}
}
