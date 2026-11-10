package integration_test

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type f036Fixture struct {
	root, work, command, grant, socket, tokens, log, release, ready, lock string
	kernelLimit                                                           uint32
}

func newF036Fixture(t *testing.T) f036Fixture {
	t.Helper()
	root, err := os.MkdirTemp(filepath.Dir(fixtureDirectory), "po-")
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(repository, root)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatal("descendant fixture leaves Lo")
	}
	work, err := os.MkdirTemp(root, "work-")
	if err != nil {
		t.Fatal(err)
	}
	fixture := f036Fixture{root: root, work: work, command: filepath.Join(root, "command"), socket: filepath.Join(root, "events"),
		tokens: filepath.Join(root, "tokens"), log: filepath.Join(root, "events.log"), release: filepath.Join(work, "release"),
		ready: filepath.Join(work, "ready"), lock: filepath.Join(root, "lock")}
	if len(fixture.socket) >= 104 {
		t.Fatal("descendant fixture socket path exceeds Darwin bound")
	}
	for _, path := range []string{fixture.tokens, fixture.log, fixture.lock} {
		writeLLVMFixture(t, path, nil)
	}
	t.Cleanup(func() {
		fixture.teardown(t)
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	endpoint, err := net.ListenUnix("unix", &net.UnixAddr{Name: fixture.socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	endpoint.SetUnlinkOnClose(false)
	broker, err := net.ListenUnix("unix", &net.UnixAddr{Name: fixture.socket + ".owner", Net: "unix"})
	if err != nil {
		_ = endpoint.Close()
		t.Fatal(err)
	}
	broker.SetUnlinkOnClose(false)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		for {
			connection, err := broker.AcceptUnix()
			if err != nil {
				return
			}
			file, err := endpoint.File()
			if err != nil {
				t.Errorf("duplicate fixture listener: %v", err)
				_ = connection.Close()
				continue
			}
			_ = connection.SetWriteDeadline(time.Now().Add(time.Second))
			_, _, sent := connection.WriteMsgUnix([]byte{1}, syscall.UnixRights(int(file.Fd())), nil)
			closed := file.Close()
			connectionClosed := connection.Close()
			if sent != nil || closed != nil || connectionClosed != nil {
				t.Errorf("transfer fixture listener: %v, %v, %v", sent, closed, connectionClosed)
			}
		}
	}()
	t.Cleanup(func() {
		if err := broker.Close(); err != nil {
			t.Error(err)
		}
		<-joined
		if err := endpoint.Close(); err != nil {
			t.Error(err)
		}
	})
	compiler, flags := llvmDarwinSDKHarness(t, work)
	arguments := append(flags, filepath.Join(repository, "testdata/native-runtime/process-ownership-command.c"), "-o", fixture.command)
	command := exec.Command(compiler, arguments...)
	command.Dir, command.Env = work, fixture.environment()
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build controlled descendant command: %v\n%s", err, output)
	}
	contents, err := os.ReadFile(fixture.command)
	if err != nil {
		t.Fatal(err)
	}
	fixture.grant = fmt.Sprintf("%x,%s", sha256.Sum256(contents), fixture.command)
	fixture.kernelLimit = fixture.hostProcessLimit(t)
	return fixture
}

func (fixture f036Fixture) hostProcessLimit(t *testing.T, prefix ...string) uint32 {
	t.Helper()
	command := exec.Command(fixture.command, append(prefix, "--host-process-limits")...)
	command.Dir, command.Env = fixture.work, fixture.environment()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("read fixture process limits: %v: %q", err, output)
	}
	if len(output) > 100 || !strings.HasSuffix(string(output), "\n") {
		t.Fatalf("invalid fixture process-limit response: %q", output)
	}
	fields := strings.Split(strings.TrimSuffix(string(output), "\n"), " ")
	if len(fields) != 3 {
		t.Fatalf("invalid fixture process-limit fields: %q", output)
	}
	var limits [3]uint64
	for index, field := range fields {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil || value == 0 || strconv.FormatUint(value, 10) != field {
			t.Fatalf("invalid fixture process-limit value %q: %v", field, err)
		}
		limits[index] = value
	}
	if limits[0] > limits[1] || limits[2] > uint64(^uint32(0)) {
		t.Fatalf("inconsistent fixture process limits: %q", output)
	}
	selected := min(limits[1], limits[2])
	t.Logf("fixture process limits: inherited soft=%d hard=%d kernel ceiling=%d selected=%d", limits[0], limits[1], limits[2], selected)
	return uint32(selected)
}

func (fixture f036Fixture) kernelArgument() string {
	return strconv.FormatUint(uint64(fixture.kernelLimit), 10)
}

func (fixture f036Fixture) environment() []string {
	return []string{"CI=1", "PATH=", "TMPDIR=" + fixture.work, "TMP=" + fixture.work, "TEMP=" + fixture.work,
		"CLANG_MODULE_CACHE_PATH=" + fixture.work, "LC_ALL=C", "LANG=C"}
}

func (fixture f036Fixture) tokenRecords(t *testing.T) [][2]string {
	t.Helper()
	contents, err := os.ReadFile(fixture.tokens)
	if err != nil || len(contents) > 32768 {
		t.Fatalf("read bounded fixture token inventory: %v", err)
	}
	var records [][2]string
	for _, line := range strings.Split(string(contents), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || len(fields[1]) != 64 {
			// A writer can have an incomplete last line while the process is active.
			if !strings.HasSuffix(string(contents), "\n") && strings.HasSuffix(string(contents), line) {
				break
			}
			t.Fatalf("invalid fixture token record: %q", line)
		}
		records = append(records, [2]string{fields[0], fields[1]})
	}
	return records
}

func (fixture f036Fixture) signalToken(t *testing.T, token string, signal syscall.Signal) {
	t.Helper()
	command := exec.Command(fixture.command, "--host-signal", token, strconv.Itoa(int(signal)))
	command.Dir, command.Env = fixture.work, fixture.environment()
	if output, err := command.CombinedOutput(); err != nil {
		t.Errorf("signal controlled audit token: %v: %s", err, output)
	}
}

func (fixture f036Fixture) signalRole(t *testing.T, role string, signal syscall.Signal) {
	t.Helper()
	for _, record := range fixture.tokenRecords(t) {
		if record[0] == role {
			fixture.signalToken(t, record[1], signal)
			return
		}
	}
	t.Fatalf("fixture did not record %s identity", role)
}

func (fixture f036Fixture) alive(token string) bool {
	command := exec.Command(fixture.command, "--host-alive", token)
	command.Dir, command.Env = fixture.work, fixture.environment()
	return command.Run() == nil
}

func (fixture f036Fixture) teardown(t *testing.T) {
	t.Helper()
	// This independent host path also destroys quarantined owners after assertions.
	// Forced test teardown is not a successful controller cleanup receipt.
	for _, record := range fixture.tokenRecords(t) {
		fixture.signalToken(t, record[1], syscall.SIGKILL)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		remaining := false
		for _, record := range fixture.tokenRecords(t) {
			if fixture.alive(record[1]) {
				remaining = true
				fixture.signalToken(t, record[1], syscall.SIGKILL)
			}
		}
		if !remaining {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("controlled process remains after token-bound fixture teardown")
}

func (fixture f036Fixture) wait(t *testing.T, subject string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	log, _ := os.ReadFile(fixture.log)
	t.Fatalf("wait for %s: provider log %q", subject, log)
}

func (fixture f036Fixture) logged(text string) bool {
	contents, err := os.ReadFile(fixture.log)
	return err == nil && strings.Contains(string(contents), text)
}

func (fixture f036Fixture) lockBusy(t *testing.T) bool {
	t.Helper()
	file, err := os.Open(fixture.lock)
	if err != nil {
		t.Fatal(err)
	}
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if closeErr := file.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err == syscall.EWOULDBLOCK {
		return true
	}
	if err != nil {
		t.Fatalf("observe fixture lock exclusion: %v", err)
	}
	return false
}

func (fixture f036Fixture) flags(t *testing.T, artifact string) []string {
	t.Helper()
	return []string{"run", "--format", "json", "--process-descendants", "--process-allow", fixture.grant,
		"--process-launcher", artifact, "--process-read-root", fixture.root,
		"--process-write-root", cleanupScope(t, fixture.command, "root", fixture.work),
		"--process-unix-endpoint", cleanupScope(t, fixture.command, "endpoint", fixture.socket),
		"--lock-file", fixture.lock, "--max-process-ms", "1500", "--max-process-cleanup-ms", "200"}
}

func (fixture f036Fixture) transform(t *testing.T, source []byte) []byte {
	t.Helper()
	return []byte(strings.NewReplacer(
		"@F036_ROOT@", strconv.Quote(fixture.root), "@F036_SOCKET@", strconv.Quote(fixture.socket),
		"@F036_TOKENS@", strconv.Quote(fixture.tokens), "@F036_LOG@", strconv.Quote(fixture.log),
		"@F036_RELEASE@", strconv.Quote(fixture.release)).Replace(string(source)))
}

func (fixture f036Fixture) source(operation string) string {
	return fmt.Sprintf(`flow Recover(error:Error)->Int{return Match.new(String.eq(error.kind,"Process.Timeout"),true:0,_:99)};flow Main()->Int onError Recover{lock=Lock.File.new(File.Path.new(%q));lease=commit lock;context=Process.Context.new(Map.new(),File.Path.new(%q));executable=Process.Executable.new(File.Path.new(%q));command=Process.Command.new(context,executable,List.new(%q,%q,%q,%q,"initial"),Bytes.new(""));result=commit command;return Process.Termination.exit_code(result.termination)};return Main()`, fixture.lock, fixture.work, fixture.command, fixture.socket, operation, fixture.ready, fixture.release)
}

func TestF036InjectedDescendantOwnership(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Darwin process and audit-token fixtures")
	}
	for _, scenario := range []struct {
		name, operation string
		number          int
		quarantine      bool
	}{
		{"fork", "fork", 0, false}, {"spawn", "spawn", 1, false},
		{"detached-session", "detach", 2, false}, {"leader-exit-inherited-pipe", "leader", 3, false},
		{"registered-exec-chain", "exec", 4, false}, {"caller-eof-retains-lock", "leader", 5, false},
		{"sequence-gap-quarantine", "pause", 6, true}, {"pending-exec-denied-after-freeze", "exec-pause", 7, false},
		{"allowed-exec-notification-after-freeze", "exec-pause", 8, false},
		{"missing-final-marker", "finish", 9, true}, {"wrong-sync-generation", "finish", 10, true},
		{"event-work-limit", "pause", 11, true}, {"unregistered-exec-identity", "finish", 12, false},
		{"provider-close-failure", "finish", 13, true},
		{"fork-delivery-after-cancellation", "leader", 15, false},
		{"ready-without-launcher-exec", "finish", 16, true},
		{"premature-synchronization", "finish", 17, true},
		{"exit-of-proposed-token", "finish", 18, true},
		{"open-by-proposed-token", "finish", 19, true},
		{"token-ledger-capacity", "finish", 20, true},
		{"damaged-retained-authorization", "finish", 21, true},
		{"process-ledger-capacity", "fork", 22, true},
		{"late-fork-after-event-budget", "leader", 23, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newF036Fixture(t)
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "public-cli", source: fixture.source(scenario.operation),
				number: scenario.number, harness: "testdata/native-runtime/process-ownership-provider.c",
				hostBootstrap: "lo_test_host_init", sdkHarness: true, harnessTransform: fixture.transform,
				run: func(t *testing.T, artifact, work string) {
					stdoutPath, stderrPath := filepath.Join(work, "caller.stdout"), filepath.Join(work, "caller.stderr")
					stdout, err := os.Create(stdoutPath)
					if err != nil {
						t.Fatal(err)
					}
					defer stdout.Close()
					stderr, err := os.Create(stderrPath)
					if err != nil {
						t.Fatal(err)
					}
					defer stderr.Close()
					arguments := append(fixture.flags(t, artifact), "--process-kernel-limit", fixture.kernelArgument())
					if scenario.number == 11 || scenario.number == 23 {
						arguments = append(arguments, "--max-process-events", "32")
					}
					if scenario.number == 20 {
						arguments = append(arguments, "--max-process-tokens", "1")
					}
					if scenario.number == 22 {
						arguments = append(arguments, "--max-process-descendants", "1")
					}
					command := exec.Command(artifact, arguments...)
					command.Dir, command.Env = fixture.work, fixture.environment()
					command.Stdout, command.Stderr = stdout, stderr
					if err := command.Start(); err != nil {
						t.Fatal(err)
					}
					finished := make(chan error, 1)
					go func() { finished <- command.Wait() }()
					waited := false
					defer func() {
						fixture.teardown(t)
						if !waited {
							select {
							case <-finished:
							case <-time.After(2 * time.Second):
								t.Error("caller did not exit after fixture teardown")
							}
						}
					}()
					if scenario.number == 5 {
						fixture.wait(t, "descendant ready", func() bool { _, err := os.Stat(fixture.ready); return err == nil })
						if !fixture.lockBusy(t) {
							t.Fatal("activation did not retain the program lock")
						}
						fixture.signalRole(t, "caller", syscall.SIGUSR1)
					} else if scenario.number == 7 || scenario.number == 8 || scenario.number == 15 {
						pending := "H\n"
						if scenario.number == 8 {
							pending = "A-ready\n"
						}
						if scenario.number == 15 {
							pending = "F-pending\n"
						}
						fixture.wait(t, "pending exec boundary", func() bool { return fixture.logged(pending) })
						fixture.signalRole(t, "caller", syscall.SIGTERM)
						fixture.wait(t, "cleanup signal", func() bool { return fixture.logged("K\n") })
						writeLLVMFixture(t, fixture.release, []byte("release"))
					}
					select {
					case <-finished:
						waited = true
					case <-time.After(5 * time.Second):
						t.Fatal("public CLI did not finish within fixture bound")
					}
					output, _ := os.ReadFile(stdoutPath)
					diagnostic, _ := os.ReadFile(stderrPath)
					actual := result{string(output), string(diagnostic), command.ProcessState.ExitCode()}
					if scenario.number < 5 {
						nativeValueCheck("Int", `"0"`)(t, actual)
						macCleanupPresent(t, fixture.ready, true)
						pids, tokens := map[string]bool{}, map[string]bool{}
						for _, record := range fixture.tokenRecords(t) {
							if record[0] == "command" {
								pids[record[1][40:48]] = true
								tokens[record[1]] = true
							}
						}
						if scenario.number < 4 && len(pids) < 2 {
							t.Fatal("positive descendant case did not create a distinct child")
						}
						if scenario.number == 4 && len(tokens) < 3 {
							t.Fatal("exec-chain case did not confirm its execution transitions")
						}
					} else if scenario.number == 5 {
						if actual.status != 0 || actual.stdout != "" {
							t.Fatalf("caller EOF fixture: %+v", actual)
						}
						fixture.wait(t, "supervisor cleanup after EOF", func() bool { return fixture.logged("K\n") })
						if !fixture.lockBusy(t) {
							t.Fatal("caller exit released a transferred lock before cleanup")
						}
						writeLLVMFixture(t, fixture.release, []byte("release"))
					} else if actual.status != 2 || !strings.Contains(actual.stderr, "runtime.") {
						t.Fatalf("ownership failure was not terminal: %+v", actual)
					}
					if scenario.number == 20 && !fixture.logged("D-provider\n") {
						t.Fatal("exhausted AUTH was not denied by the provider")
					}
					if scenario.number == 21 && (!fixture.logged("D\n") || fixture.logged("D-provider\n")) {
						t.Fatal("retained damaged AUTH did not receive exactly one controller denial")
					}
					if scenario.number == 23 && !fixture.logged("F-after-limit\n") {
						t.Fatal("event-budget case did not deliver its late fork")
					}
					if scenario.number == 16 || scenario.number == 17 {
						macCleanupPresent(t, fixture.ready, false)
					}
					if scenario.number == 7 || scenario.number == 12 {
						fixture.wait(t, "exec denial", func() bool { return fixture.logged("D\n") })
					}
					if scenario.quarantine {
						if !fixture.lockBusy(t) {
							t.Fatal("quarantined supervisor released the transferred lock")
						}
						if fixture.logged("C\n") {
							t.Fatal("quarantine closed the observation client")
						}
					} else {
						fixture.wait(t, "complete owner release", func() bool { return fixture.logged("C\n") && !fixture.lockBusy(t) })
						fixture.wait(t, "all owned process exits", func() bool {
							for _, token := range fixture.tokenRecords(t) {
								if fixture.alive(token[1]) {
									return false
								}
							}
							return true
						})
					}
				}}})
		})
	}
}
