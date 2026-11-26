package testsession_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/testsession"
)

func sessionFixture(t *testing.T, slots int) (*testsession.Server, string) {
	return sessionFixtureContext(t, slots, context.Background())
}

func sessionFixtureContext(t *testing.T, slots int, ctx context.Context) (*testsession.Server, string) {
	t.Helper()
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	compiler := filepath.Join(directory, "lo")
	command := exec.Command("go", "build", "-o", compiler, "./cmd/lo")
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build compiler: %v\n%s", err, output)
	}
	manifest := os.Getenv("LO_LLVM_TOOLCHAIN")
	if manifest == "" {
		manifest = filepath.Join(repository, "_scratch/native-runtime/distribution/toolchain.json")
	}
	server, err := testsession.Start(ctx, testsession.Config{
		Directory:  filepath.Join(directory, "session"),
		Compiler:   compiler,
		Toolchain:  manifest,
		BuildSlots: slots,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	})
	return server, directory
}

func TestFixtureSessionNativeExecutionAndDeduplication(t *testing.T) {
	server, _ := sessionFixture(t, 2)
	source, err := testsession.Source(context.Background(), server.Address(), "return Math.add(20, 22)")
	if err != nil {
		t.Fatal(err)
	}
	const count = 8
	results := make([]testsession.CompileResult, count)
	errors := make([]error, count)
	var workers sync.WaitGroup
	for index := range count {
		workers.Go(func() {
			results[index], errors[index] = testsession.Compile(context.Background(), server.Address(), testsession.CompileRequest{Source: source})
		})
	}
	workers.Wait()
	for index, result := range results {
		if errors[index] != nil {
			t.Fatal(errors[index])
		}
		if result.Artifact != results[0].Artifact || len(result.Runtime) != 64 {
			t.Fatalf("shared build identity: %+v", results)
		}
	}
	command := exec.Command(results[0].Artifact, "run")
	if output, err := command.CombinedOutput(); err != nil || string(output) != "42\n" {
		t.Fatalf("native public CLI: %v %s", err, output)
	}
	metadata, err := exec.Command(results[0].Artifact, "metadata").Output()
	var identity map[string]json.RawMessage
	if err != nil || json.Unmarshal(metadata, &identity) != nil {
		t.Fatalf("native artifact metadata: %v %s", err, metadata)
	}
	var digest, original string
	if json.Unmarshal(identity["runtime_sha256"], &digest) != nil || digest != results[0].Runtime || json.Unmarshal(identity["source"], &original) != nil || original != source {
		t.Fatalf("native runtime/source identity changed: %s", metadata)
	}
	if stats := server.Stats(); stats.Builds != 1 || stats.Reused != count-1 {
		t.Fatalf("build accounting: %+v", stats)
	}
}

func TestFixtureSessionPreservesSourceIdentityAndFailures(t *testing.T) {
	server, directory := sessionFixture(t, 2)
	for _, name := range []string{"first.lo", "second.lo"} {
		path := filepath.Join(directory, name)
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.WriteString("return Math.div(1, 0)")
		if err := file.Close(); writeErr != nil || err != nil {
			t.Fatalf("source: %v %v", writeErr, err)
		}
		compiled, err := testsession.Compile(context.Background(), server.Address(), testsession.CompileRequest{Source: path})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(compiled.Artifact, "run", "--format", "json").CombinedOutput()
		if err == nil || !strings.Contains(string(output), path) || !strings.Contains(string(output), "Math.DivideByZero") {
			t.Fatalf("source diagnostic after removal: %v %s", err, output)
		}
	}
	bad, err := testsession.Source(context.Background(), server.Address(), "return Unknown.operation()")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := testsession.Compile(context.Background(), server.Address(), testsession.CompileRequest{Source: bad}); err == nil || !strings.Contains(err.Error(), "resolve.unknown-name") {
			t.Fatalf("compiler failure lost: %v", err)
		}
	}
	if stats := server.Stats(); stats.Builds != 4 {
		t.Fatalf("failed builds must not remain cached: %+v", stats)
	}
}

func TestFixtureSessionLeaseCancellationAndCleanup(t *testing.T) {
	server, directory := sessionFixture(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	lease, err := testsession.Acquire(ctx, server.Address())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := testsession.Acquire(context.Background(), server.Address())
	if err != nil {
		t.Fatal(err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
	inputs, err := testsession.Inputs(context.Background(), server.Address())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(inputs.Manifest, filepath.Join(directory, "session")) {
		t.Fatalf("session input snapshot: %+v", inputs)
	}
	limited := filepath.Join(directory, "limited")
	if _, err := testsession.Start(t.Context(), testsession.Config{
		Directory: limited, Toolchain: inputs.Manifest, Compiler: inputs.Compiler, BuildSlots: 1, MaxBytes: 1,
	}); err == nil || !strings.Contains(err.Error(), "storage budget") {
		t.Fatalf("storage reservation failure was not reported: %v", err)
	}
	if _, err := os.Stat(limited); !os.IsNotExist(err) {
		t.Fatalf("failed snapshot left session state: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "session")); !os.IsNotExist(err) {
		t.Fatal(fmt.Errorf("session workspace remains: %w", err))
	}
}

func TestFixtureSessionCanceledQueuedBuildCanRetry(t *testing.T) {
	server, _ := sessionFixture(t, 1)
	lease, err := testsession.Acquire(t.Context(), server.Address())
	if err != nil {
		t.Fatal(err)
	}
	source, err := testsession.Source(t.Context(), server.Address(), "return 17")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	completed := make(chan error, 1)
	go func() {
		_, err := testsession.Compile(ctx, server.Address(), testsession.CompileRequest{Source: source})
		completed <- err
	}()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for server.Stats().Builds == 0 {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("native fixture did not queue behind the occupied global slot")
		}
	}
	cancel()
	if err := <-completed; err == nil {
		t.Fatal("cancelled queued native build succeeded")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	compiled, err := testsession.Compile(t.Context(), server.Address(), testsession.CompileRequest{Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(compiled.Artifact, "run").CombinedOutput(); err != nil || string(output) != "17\n" {
		t.Fatalf("native retry after cancellation: %v %s", err, output)
	}
}

func TestFixtureSessionEvictsOnlyReleasedArtifacts(t *testing.T) {
	baseline, directory := sessionFixture(t, 2)
	seed, err := testsession.Source(t.Context(), baseline.Address(), "return 11")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := testsession.Compile(t.Context(), baseline.Address(), testsession.CompileRequest{Source: seed})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(prepared.Artifact)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := testsession.Inputs(t.Context(), baseline.Address())
	if err != nil {
		t.Fatal(err)
	}
	budget := baseline.Stats().Bytes + info.Size() + 65536
	server, err := testsession.Start(t.Context(), testsession.Config{
		Directory: filepath.Join(directory, "bounded"), Toolchain: inputs.Manifest,
		Compiler: inputs.Compiler, BuildSlots: 2, MaxBytes: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	})
	compile := func(text string) (testsession.CompileResult, interface{ Close() error }) {
		t.Helper()
		source, err := testsession.Source(t.Context(), server.Address(), text)
		if err != nil {
			t.Fatal(err)
		}
		result, lease, err := testsession.CompileRetained(t.Context(), server.Address(), testsession.CompileRequest{Source: source})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = lease.Close() })
		return result, lease
	}
	first, firstLease := compile("return 11")
	second, secondLease := compile("return 22")
	blockedSource, err := testsession.Source(t.Context(), server.Address(), "return 33")
	if err != nil {
		t.Fatal(err)
	}
	if _, lease, err := testsession.CompileRetained(t.Context(), server.Address(), testsession.CompileRequest{Source: blockedSource}); err == nil || lease != nil || !strings.Contains(err.Error(), "storage budget") {
		t.Fatalf("retained fixtures did not enforce the storage budget: lease=%v error=%v", lease, err)
	}
	if err := secondLease.Close(); err != nil {
		t.Fatal(err)
	}
	awaitRetained(t, server, 1)
	third, thirdLease := compile("return 33")
	if _, err := os.Stat(second.Artifact); !os.IsNotExist(err) {
		t.Fatalf("released fixture was not evicted: %v", err)
	}
	for _, artifact := range []testsession.CompileResult{first, third} {
		if _, err := os.Stat(artifact.Artifact); err != nil {
			t.Fatalf("active fixture was evicted: %v", err)
		}
	}
	if output, err := exec.Command(first.Artifact, "run").CombinedOutput(); err != nil || string(output) != "11\n" {
		t.Fatalf("active artifact stopped executing: %v %s", err, output)
	}
	if err := firstLease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := thirdLease.Close(); err != nil {
		t.Fatal(err)
	}
	awaitRetained(t, server, 0)
	_, rebuiltLease := compile("return 22")
	if err := rebuiltLease.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := server.Stats(); stats.Builds != 5 || stats.Evicted < 2 || stats.Bytes > budget {
		t.Fatalf("bounded fixture registry: %+v budget=%d", stats, budget)
	}
}

func awaitRetained(t *testing.T, server *testsession.Server, expected uint64) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for server.Stats().Retained != expected {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("artifact retention did not settle at %d: %+v", expected, server.Stats())
		}
	}
}

func TestFixtureSessionRetainedWorkerDisconnect(t *testing.T) {
	server, _ := sessionFixture(t, 2)
	source, err := testsession.Source(t.Context(), server.Address(), "return 41")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFixtureSessionRetentionWorker$")
	command.Env = append(os.Environ(), "LO_FIXTURE_RETENTION_WORKER="+server.Address(), "LO_FIXTURE_RETENTION_SOURCE="+source)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	ready, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || ready != "retained\n" {
		t.Fatalf("retaining worker did not start: %q %v", ready, err)
	}
	awaitRetained(t, server, 1)
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("worker unexpectedly survived forced exit")
	}
	awaitRetained(t, server, 0)
	compiled, lease, err := testsession.CompileRetained(t.Context(), server.Address(), testsession.CompileRequest{Source: source})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if output, err := exec.Command(compiled.Artifact, "run").CombinedOutput(); err != nil || string(output) != "41\n" {
		t.Fatalf("fixture after worker death: %v %s", err, output)
	}
}

func TestFixtureSessionRetentionWorker(t *testing.T) {
	address := os.Getenv("LO_FIXTURE_RETENTION_WORKER")
	if address == "" {
		return
	}
	_, lease, err := testsession.CompileRetained(t.Context(), address, testsession.CompileRequest{Source: os.Getenv("LO_FIXTURE_RETENTION_SOURCE")})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	fmt.Fprintln(os.Stdout, "retained")
	<-t.Context().Done()
}

func TestFixtureSessionRejectsQueuedSourceMutation(t *testing.T) {
	server, directory := sessionFixture(t, 1)
	source := filepath.Join(directory, "mutable.lo")
	writeSource := func(text string) {
		t.Helper()
		file, err := os.Create(source)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.WriteString(text)
		if err := errors.Join(writeErr, file.Close()); err != nil {
			t.Fatal(err)
		}
	}
	writeSource("return 1")
	slot, err := testsession.Acquire(t.Context(), server.Address())
	if err != nil {
		t.Fatal(err)
	}
	defer slot.Close()
	finished := make(chan error, 1)
	go func() {
		_, err := testsession.Compile(t.Context(), server.Address(), testsession.CompileRequest{Source: source})
		finished <- err
	}()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for server.Stats().Builds == 0 {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("fixture did not enter the occupied backend queue")
		}
	}
	writeSource("return 2")
	if err := slot.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err == nil || !strings.Contains(err.Error(), "source identity") {
		t.Fatalf("queued source mutation was not rejected: %v", err)
	}
	writeSource("return 1")
	compiled, retained, err := testsession.CompileRetained(t.Context(), server.Address(), testsession.CompileRequest{Source: source})
	if err != nil {
		t.Fatal(err)
	}
	defer retained.Close()
	if output, err := exec.Command(compiled.Artifact, "run").CombinedOutput(); err != nil || string(output) != "1\n" {
		t.Fatalf("original source retry used corrupted cache identity: %v %s", err, output)
	}
}

func TestFixtureSessionCancelClosesIdleWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server, _ := sessionFixtureContext(t, 1, ctx)
	const count = 32
	connections := make([]net.Conn, count)
	for index := range connections {
		connection, err := (&net.Dialer{}).DialContext(t.Context(), "unix", server.Address())
		if err != nil {
			t.Fatal(err)
		}
		connections[index] = connection
		t.Cleanup(func() { _ = connection.Close() })
	}
	cancel()
	for _, connection := range connections {
		if err := connection.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		var data [1]byte
		if _, err := connection.Read(data[:]); err == nil {
			t.Fatal("idle worker stayed open after session cancellation")
		} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
			t.Fatalf("idle worker was not closed after session cancellation: %v", err)
		}
	}
	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("session shutdown did not join all accepted workers")
	}
}

func TestFixtureSessionCommandFailurePreservesCause(t *testing.T) {
	server, _ := sessionFixture(t, 1)
	inputs, err := testsession.Inputs(t.Context(), server.Address())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(inputs.Compiler); err != nil {
		t.Fatal(err)
	}
	source, err := testsession.Source(t.Context(), server.Address(), "return 1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = testsession.Compile(t.Context(), server.Address(), testsession.CompileRequest{Source: source})
	var failure *testsession.CommandError
	if !errors.As(err, &failure) || failure.Status != -1 || failure.Cause == "" || !strings.Contains(failure.Cause, "no such file") {
		t.Fatalf("frontend execution cause was lost: %#v %v", failure, err)
	}
}
