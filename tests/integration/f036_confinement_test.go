package integration_test

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func f036ConfinementCommand(t *testing.T, fixture f036Fixture, path string) string {
	t.Helper()
	compiler, flags := llvmDarwinSDKHarness(t, fixture.work)
	flags = append(flags, fmt.Sprintf("-DLO_TEST_PROCESS_KERNEL_LIMIT=%du", fixture.kernelLimit))
	command := exec.Command(compiler, append(flags, filepath.Join(repository, "testdata/native-runtime/process-confinement-command.c"), "-o", path)...)
	command.Dir, command.Env = fixture.work, fixture.environment()
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build confined command: %v\n%s", err, output)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x,%s", sha256.Sum256(contents), path)
}

func f036ConfinementSource(fixture f036Fixture, initial, operation string, payload ...string) string {
	arguments := []string{fixture.socket, operation, fixture.ready, fixture.release, "initial", ""}
	arguments = append(arguments, payload...)
	encoded := make([]string, len(arguments))
	for index, argument := range arguments {
		encoded[index] = strconv.Quote(argument)
	}
	return fmt.Sprintf(`flow Main()->Int!{lock=Lock.File.new(File.Path.new(%q));lease=commit lock;context=Process.Context.new(Map.new(),File.Path.new(%q));executable=Process.Executable.new(File.Path.new(%q));command=Process.Command.new(context,executable,List.new(%s),Bytes.new(""));result=commit command;return Process.Termination.exit_code(result.termination)};return Main()`,
		fixture.lock, fixture.work, initial, strings.Join(encoded, ","))
}

func f036ConfinementComplete(t *testing.T, fixture f036Fixture, actual result) {
	t.Helper()
	nativeValueCheck("Int", `"0"`)(t, actual)
	fixture.wait(t, "confined activation owner release", func() bool { return fixture.logged("C\n") && !fixture.lockBusy(t) })
	fixture.wait(t, "confined child exits", func() bool {
		for _, token := range fixture.tokenRecords(t) {
			if fixture.alive(token[1]) {
				return false
			}
		}
		return true
	})
}

func f036ConfinementDiagnostics(t *testing.T, fixture f036Fixture) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		for _, path := range []string{fixture.log, fixture.tokens, fixture.ready} {
			file, err := os.Open(path)
			if err != nil {
				t.Logf("confinement evidence %s: %v", filepath.Base(path), err)
				continue
			}
			contents, readErr := io.ReadAll(io.LimitReader(file, 32768))
			closeErr := file.Close()
			t.Logf("confinement evidence %s (at most 32768 bytes): %q; read=%v close=%v", filepath.Base(path), contents, readErr, closeErr)
		}
	})
}

func TestF036NativeConfinement(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires the Darwin descendant profile and audit-token fixture")
	}
	for _, operation := range []string{"syscalls-fork", "spawn-uid", "spawn-persona", "spawn-resetids-descriptor-bound"} {
		t.Run(operation, func(t *testing.T) {
			fixture := newF036Fixture(t)
			f036ConfinementDiagnostics(t, fixture)
			writeLLVMFixture(t, fixture.ready, nil)
			fixture.grant = f036ConfinementCommand(t, fixture, fixture.command)
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "public-cli", source: f036ConfinementSource(fixture, fixture.command, operation),
				harness: "testdata/native-runtime/process-ownership-provider.c", harnessTransform: fixture.transform,
				hostBootstrap: "lo_test_host_init", sdkHarness: true,
				run: func(t *testing.T, artifact, work string) {
					actual := f036RunPolicyFixture(t, fixture, artifact, "--process-kernel-limit", fixture.kernelArgument())
					f036ConfinementComplete(t, fixture, actual)
				}}})
		})
	}
}

func f036ConfinementEndpoint(t *testing.T, path string) *atomic.Int32 {
	t.Helper()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	var received atomic.Int32
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		for {
			connection, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			_ = connection.SetDeadline(time.Now().Add(time.Second))
			var request [1]byte
			if _, err := io.ReadFull(connection, request[:]); err == nil && request[0] == 'x' {
				received.Add(1)
				_, _ = connection.Write([]byte{'y'})
			}
			_ = connection.Close()
		}
	}()
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
		<-joined
	})
	return &received
}

func TestF036RegisteredExecRetainsInitialScope(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires the Darwin descendant profile and audit-token fixture")
	}
	for _, reverse := range []bool{false, true} {
		name := "first-to-second"
		if reverse {
			name = "second-to-first"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newF036Fixture(t)
			fixture.grant = f036ConfinementCommand(t, fixture, fixture.command)
			second := filepath.Join(fixture.root, "second-command")
			secondGrant := f036ConfinementCommand(t, fixture, second)
			secondWrites, err := os.MkdirTemp(fixture.root, "second-writes-")
			if err != nil {
				t.Fatal(err)
			}
			firstFile := fileFixture(t, fixture.work, "first-scope", "preserved")
			secondFile := fileFixture(t, secondWrites, "second-scope", "preserved")
			firstEndpoint, secondEndpoint := filepath.Join(fixture.root, "first.sock"), filepath.Join(fixture.root, "second.sock")
			firstRequests := f036ConfinementEndpoint(t, firstEndpoint)
			secondRequests := f036ConfinementEndpoint(t, secondEndpoint)
			initial, target := fixture.command, second
			allowedFile, deniedFile := firstFile, secondFile
			allowedEndpoint, deniedEndpoint := firstEndpoint, secondEndpoint
			allowedRequests, deniedRequests := firstRequests, secondRequests
			if reverse {
				initial, target = second, fixture.command
				allowedFile, deniedFile = secondFile, firstFile
				allowedEndpoint, deniedEndpoint = secondEndpoint, firstEndpoint
				allowedRequests, deniedRequests = secondRequests, firstRequests
			}
			source := f036ConfinementSource(fixture, initial, "scope-exec", target, allowedFile, deniedFile, allowedEndpoint, deniedEndpoint)
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "public-cli", source: source,
				harness: "testdata/native-runtime/process-ownership-provider.c", harnessTransform: fixture.transform,
				hostBootstrap: "lo_test_host_init", sdkHarness: true,
				run: func(t *testing.T, artifact, work string) {
					actual := f036RunPolicyFixture(t, fixture, artifact,
						"--process-kernel-limit", fixture.kernelArgument(), "--process-allow", secondGrant,
						"--process-write-root", cleanupScope(t, second, "root", secondWrites),
						"--process-unix-endpoint", cleanupScope(t, second, "endpoint", fixture.socket),
						"--process-unix-endpoint", cleanupScope(t, fixture.command, "endpoint", firstEndpoint),
						"--process-unix-endpoint", cleanupScope(t, second, "endpoint", secondEndpoint))
					f036ConfinementComplete(t, fixture, actual)
					allowed, allowedErr := os.ReadFile(allowedFile)
					denied, deniedErr := os.ReadFile(deniedFile)
					if allowedErr != nil || deniedErr != nil || string(allowed) != "changed" || string(denied) != "preserved" {
						t.Fatalf("exec changed initial file scope: allowed=%q (%v) denied=%q (%v)", allowed, allowedErr, denied, deniedErr)
					}
					if allowedRequests.Load() != 1 || deniedRequests.Load() != 0 {
						t.Fatalf("exec changed initial endpoint scope: allowed=%d denied=%d", allowedRequests.Load(), deniedRequests.Load())
					}
				}}})
		})
	}
}
