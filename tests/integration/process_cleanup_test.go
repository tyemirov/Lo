package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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

type cleanupFixture struct {
	root, writes, prunes, executable, grant string
}

func newCleanupFixture(t *testing.T) cleanupFixture {
	return newCleanupFixtureSource(t, "process-cleanup-command.c")
}

func newCleanupFixtureSource(t *testing.T, source string) cleanupFixture {
	t.Helper()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires selected Darwin Process profile")
	}
	root, err := os.MkdirTemp(filepath.Dir(fixtureDirectory), "pc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	writes, err := os.MkdirTemp(root, "cache-")
	if err != nil {
		t.Fatal(err)
	}
	prunes, err := os.MkdirTemp(root, "store-")
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "command")
	command := exec.Command("cc", filepath.Join(repository, "testdata/native-runtime", source), "-o", executable)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build cleanup fixture: %v: %s", err, output)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return cleanupFixture{root, writes, prunes, executable, fmt.Sprintf("%x,%s", digest, executable)}
}

func cleanupScope(t *testing.T, executable, field, path string) string {
	t.Helper()
	data, err := json.Marshal(map[string]string{"executable": executable, field: path})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (fixture cleanupFixture) flags() []string {
	return []string{"--process-allow", fixture.grant, "--process-launcher", filepath.Join(repository, "bin", "lo-process-launcher"), "--process-read-root", fixture.root}
}

func (fixture cleanupFixture) source(t *testing.T, arguments ...string) string {
	t.Helper()
	inputs := make([]string, len(arguments))
	for index, argument := range arguments {
		inputs[index] = processLiteral(t, argument)
	}
	return processContextProgram("Int", fmt.Sprintf(`context = Process.Context.new(Map.new(), File.Path.new(%q)); executable = Process.Executable.new(File.Path.new(%q)); command = Process.Command.new(context, executable, List.new(%s), Bytes.new("")); result = commit command; return Process.Termination.exit_code(result.termination)`, fixture.root, fixture.executable, strings.Join(inputs, ",")))
}

func TestProcessCleanupDataWrites(t *testing.T) {
	fixture := newCleanupFixture(t)
	allowed := fileFixture(t, fixture.writes, "allowed", "before")
	denied := fileFixture(t, fixture.root, "outside", "preserved")
	flags := append(fixture.flags(), "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes))
	for _, test := range []struct{ name, operation, path, destination, code string }{
		{"write-granted", "write", allowed, "", "0"},
		{"write-outside", "write", denied, "", "1"},
		{"link-outside", "link", denied, filepath.Join(fixture.writes, "outside-link"), "1"},
		{"symlink-outside", "symlink-write", denied, filepath.Join(fixture.writes, "outside-symlink"), "1"},
		{"descendant-denied", "fork", fixture.root, "", "1"},
		{"spawn-descendant-denied", "spawn", fixture.executable, allowed, "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			arguments := []string{test.operation, test.path}
			if test.destination != "" {
				arguments = append(arguments, test.destination)
			}
			got := run(t, append([]string{"run", sourceFile(t, fixture.source(t, arguments...)), "--format", "json"}, flags...)...)
			valueJSON(t, fileOutput(t, got), processLiteral(t, test.code))
		})
	}
	data, err := os.ReadFile(allowed)
	if err != nil || string(data) != "changed" {
		t.Fatalf("granted write: %q %v", data, err)
	}
	data, err = os.ReadFile(denied)
	if err != nil || string(data) != "preserved" {
		t.Fatalf("outside data changed: %q %v", data, err)
	}
}

func TestProcessCleanupPruneHardlink(t *testing.T) {
	fixture := newCleanupFixture(t)
	protected := fileFixture(t, fixture.root, "protected", "preserved")
	alias := filepath.Join(fixture.prunes, "alias")
	if err := os.Link(protected, alias); err != nil {
		t.Fatal(err)
	}
	flags := append(fixture.flags(), "--process-prune-root", cleanupScope(t, fixture.executable, "root", fixture.prunes))
	for _, test := range []struct{ operation, path, code string }{{"write", alias, "1"}, {"unlink", alias, "0"}} {
		got := run(t, append([]string{"run", sourceFile(t, fixture.source(t, test.operation, test.path)), "--format", "json"}, flags...)...)
		valueJSON(t, fileOutput(t, got), processLiteral(t, test.code))
	}
	data, err := os.ReadFile(protected)
	if err != nil || string(data) != "preserved" {
		t.Fatalf("protected inode changed: %q %v", data, err)
	}
	if _, err := os.Lstat(alias); !os.IsNotExist(err) {
		t.Fatalf("prune alias remains: %v", err)
	}
}

func TestProcessCleanupPruneAliasWithSeparateWriteRoot(t *testing.T) {
	fixture := newCleanupFixture(t)
	protected := fileFixture(t, fixture.root, "protected", "preserved")
	alias := filepath.Join(fixture.prunes, "alias")
	if err := os.Link(protected, alias); err != nil {
		t.Fatal(err)
	}
	log := fileFixture(t, fixture.writes, "log", "before")
	linked := filepath.Join(fixture.writes, "linked-alias")
	moved := filepath.Join(fixture.writes, "moved-alias")
	flags := append(fixture.flags(), "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes), "--process-prune-root", cleanupScope(t, fixture.executable, "root", fixture.prunes))
	for _, test := range []struct{ operation, path, destination, code string }{{"write", alias, "", "1"}, {"link", alias, linked, "1"}, {"rename", alias, moved, "1"}, {"unlink", alias, "", "0"}, {"write", log, "", "0"}} {
		arguments := []string{test.operation, test.path}
		if test.destination != "" {
			arguments = append(arguments, test.destination)
		}
		got := run(t, append([]string{"run", sourceFile(t, fixture.source(t, arguments...)), "--format", "json"}, flags...)...)
		valueJSON(t, fileOutput(t, got), processLiteral(t, test.code))
	}
	data, err := os.ReadFile(protected)
	if err != nil || string(data) != "preserved" {
		t.Fatalf("pruned alias changed outside data: %q %v", data, err)
	}
	data, err = os.ReadFile(log)
	if err != nil || string(data) != "changed" {
		t.Fatalf("separate data-write root was not usable: %q %v", data, err)
	}
	if _, err := os.Lstat(alias); !os.IsNotExist(err) {
		t.Fatalf("namespace alias remains: %v", err)
	}
	for _, path := range []string{linked, moved} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("namespace alias entered a data-write root: %v", err)
		}
	}
}

func TestProcessCleanupRejectsOutsideAlias(t *testing.T) {
	fixture := newCleanupFixture(t)
	protected := fileFixture(t, fixture.root, "protected", "preserved")
	alias := filepath.Join(fixture.writes, "alias")
	if err := os.Link(protected, alias); err != nil {
		t.Fatal(err)
	}
	flags := append(fixture.flags(), "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes))
	got := run(t, append([]string{"run", sourceFile(t, fixture.source(t, "write", alias)), "--format", "json"}, flags...)...)
	diagnostic := receiptHostDiagnostic(t, got)
	if got.status != 2 || diagnostic.Code != "runtime.capability-denied" {
		t.Fatalf("outside hardlink admitted: %+v", got)
	}
	data, err := os.ReadFile(protected)
	if err != nil || string(data) != "preserved" {
		t.Fatalf("protected inode changed: %q %v", data, err)
	}
}

func TestProcessCleanupRejectsDataAliasInPruneRoot(t *testing.T) {
	fixture := newCleanupFixture(t)
	path := fileFixture(t, fixture.writes, "data", "preserved")
	alias := filepath.Join(fixture.prunes, "alias")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	flags := append(fixture.flags(), "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes), "--process-prune-root", cleanupScope(t, fixture.executable, "root", fixture.prunes))
	got := run(t, append([]string{"run", sourceFile(t, fixture.source(t, "write", path)), "--format", "json"}, flags...)...)
	if got.status != 2 {
		t.Fatalf("data authority crossed a prune root: %+v", got)
	}
	if diagnostic := receiptHostDiagnostic(t, got); diagnostic.Code != "runtime.capability-denied" {
		t.Fatalf("unexpected ownership diagnostic: %+v", got)
	}
	data, err := os.ReadFile(alias)
	if err != nil || string(data) != "preserved" {
		t.Fatalf("prune inode changed: %q %v", data, err)
	}
}

func TestProcessCleanupOwnedHardlinks(t *testing.T) {
	fixture := newCleanupFixture(t)
	path := fileFixture(t, fixture.writes, "data", "before")
	alias := filepath.Join(fixture.writes, "alias")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	flags := append(fixture.flags(), "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes))
	got := run(t, append([]string{"run", sourceFile(t, fixture.source(t, "write", path)), "--format", "json"}, flags...)...)
	valueJSON(t, fileOutput(t, got), `"0"`)
	data, err := os.ReadFile(alias)
	if err != nil || string(data) != "changed" {
		t.Fatalf("owned alias did not change: %q %v", data, err)
	}
}

func TestProcessCleanupUnicodeNamespaceNames(t *testing.T) {
	fixture := newCleanupFixture(t)
	opaque := fileFixture(t, fixture.writes, "naïve-日本語", "opaque")
	path := fileFixture(t, fixture.writes, "data", "before")
	flags := append(fixture.flags(), "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes))
	got := run(t, append([]string{"run", sourceFile(t, fixture.source(t, "write", path)), "--format", "json"}, flags...)...)
	valueJSON(t, fileOutput(t, got), `"0"`)
	data, err := os.ReadFile(opaque)
	if err != nil || string(data) != "opaque" {
		t.Fatalf("unselected entry changed: %q %v", data, err)
	}
}

func TestProcessCleanupScopedPathCharacters(t *testing.T) {
	for _, name := range []string{`cache<&>`, `cache"\`, `cache\ud800`, `cache") (allow default) (literal "`, "cache-日本語", "cache-🐩", "cache\u2028\u2029", "cache\n\t\r", "cache\x01\b\f"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			fixture := newCleanupFixture(t)
			selected := filepath.Join(fixture.root, name)
			if err := os.Rename(fixture.writes, selected); err != nil {
				t.Fatal(err)
			}
			fixture.writes = selected
			path := fileFixture(t, selected, `entry<&>"\`, "before")
			outside := fileFixture(t, fixture.root, "outside", "preserved")
			scope := strings.ReplaceAll(cleanupScope(t, fixture.executable, "root", selected), "🐩", `\ud83d\udc29`)
			flags := append(fixture.flags(), "--process-write-root", scope)
			got := run(t, append([]string{"run", sourceFile(t, fixture.source(t, "write", path)), "--format", "json"}, flags...)...)
			valueJSON(t, fileOutput(t, got), `"0"`)
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "changed" {
				t.Fatalf("scoped path characters changed authority: %q %v", data, err)
			}
			denied := run(t, append([]string{"run", sourceFile(t, fixture.source(t, "write", outside)), "--format", "json"}, flags...)...)
			valueJSON(t, fileOutput(t, denied), `"1"`)
			data, err = os.ReadFile(outside)
			if err != nil || string(data) != "preserved" {
				t.Fatalf("path text expanded authority: %q %v", data, err)
			}
		})
	}
}

func TestProcessCleanupExactEndpoint(t *testing.T) {
	fixture := newCleanupFixture(t)
	allowed, denied := filepath.Join(fixture.root, `<&>"\`), filepath.Join(fixture.root, "d")
	listeners := make([]*net.UnixListener, 0, 2)
	for _, path := range []string{allowed, denied} {
		listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := listener.Close(); err != nil {
				t.Error(err)
			}
		})
		listeners = append(listeners, listener)
	}
	flags := append(fixture.flags(), "--process-unix-endpoint", cleanupScope(t, fixture.executable, "endpoint", allowed))
	for _, test := range []struct{ path, code string }{{allowed, "0"}, {denied, "1"}} {
		got := run(t, append([]string{"run", sourceFile(t, fixture.source(t, "connect", test.path)), "--format", "json"}, flags...)...)
		valueJSON(t, fileOutput(t, got), processLiteral(t, test.code))
	}
	for index, listener := range listeners {
		if err := listener.SetDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		connection, err := listener.AcceptUnix()
		if index == 0 {
			if err != nil {
				t.Fatalf("granted endpoint observed no connection: %v", err)
			}
			if err := connection.Close(); err != nil {
				t.Fatal(err)
			}
		} else if err == nil {
			connection.Close()
			t.Fatal("ungranted sibling endpoint observed a connection")
		} else if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
			t.Fatal(err)
		}
	}
}

func TestProcessCleanupInitialExecutableScope(t *testing.T) {
	fixture := newCleanupFixture(t)
	second := filepath.Join(fixture.root, "second-command")
	command := exec.Command("cc", filepath.Join(repository, "testdata/native-runtime/process-cleanup-command.c"), "-o", second)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build second cleanup fixture: %v: %s", err, output)
	}
	data, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	path := fileFixture(t, fixture.writes, "data", "before")
	flags := append(fixture.flags(), "--process-allow", fmt.Sprintf("%x,%s", digest, second), "--process-write-root", cleanupScope(t, second, "root", fixture.writes), "--process-prune-root", cleanupScope(t, fixture.executable, "root", fixture.prunes))
	source := processContextProgram("Record{first: Int, second: Int}", fmt.Sprintf(`context = Process.Context.new(Map.new(), File.Path.new(%q)); first = Process.Executable.new(File.Path.new(%q)); second = Process.Executable.new(File.Path.new(%q)); firstCommand = Process.Command.new(context, first, List.new("exec-write", %q, %q), Bytes.new("")); firstResult = commit firstCommand; secondCommand = Process.Command.new(context, second, List.new("write", %q), Bytes.new("")); secondResult = commit secondCommand; return Record.new(first: Process.Termination.exit_code(firstResult.termination), second: Process.Termination.exit_code(secondResult.termination))`, fixture.root, fixture.executable, second, second, path, path))
	got := run(t, append([]string{"run", sourceFile(t, source), "--format", "json"}, flags...)...)
	fields := processFields(t, fileOutput(t, got))
	valueJSON(t, fields["first"], `"1"`)
	valueJSON(t, fields["second"], `"0"`)
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "changed" {
		t.Fatalf("second scope did not write: %q %v", data, err)
	}
}

func TestProcessCleanupRechecksNamespaceAtActivation(t *testing.T) {
	fixture := newCleanupFixture(t)
	protected := fileFixture(t, fixture.root, "protected", "preserved")
	alias := filepath.Join(fixture.writes, "late-alias")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if err := os.Link(protected, alias); err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	observation := fmt.Sprintf(`method = HTTP.Method.new("GET"); url = HTTP.URL.new(%q); headers = HTTP.Headers.new(List.new()); observe = HTTP.Client.Request.new(method, url, headers, Bytes.new("")); observed = commit observe; `, server.URL)
	source := strings.Replace(fixture.source(t, "write", alias), "context =", observation+"context =", 1)
	flags := append(fixture.flags(), "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes), "--http-allow", httpGrant("GET", server.URL))
	got := run(t, append([]string{"run", sourceFile(t, source), "--format", "json"}, flags...)...)
	if got.status != 2 || calls.Load() != 1 {
		t.Fatalf("late alias was not checked after the observation: %+v calls=%d", got, calls.Load())
	}
	if diagnostic := receiptHostDiagnostic(t, got); diagnostic.Code != "runtime.capability-denied" {
		t.Fatalf("late alias diagnostic: %+v", got)
	}
	data, err := os.ReadFile(protected)
	if err != nil || string(data) != "preserved" {
		t.Fatalf("late outside alias changed: %q %v", data, err)
	}
}

func TestProcessCleanupAdmissionDepthLimit(t *testing.T) {
	fixture := newCleanupFixture(t)
	parent := fixture.writes
	for index := 0; index < 129; index++ {
		child, err := os.MkdirTemp(parent, "d")
		if err != nil {
			t.Fatal(err)
		}
		next := filepath.Join(parent, "d")
		if err := os.Rename(child, next); err != nil {
			t.Fatal(err)
		}
		parent = next
	}
	path := fileFixture(t, fixture.writes, "data", "preserved")
	flags := append(fixture.flags(), "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes))
	got := run(t, append([]string{"run", sourceFile(t, fixture.source(t, "write", path)), "--format", "json"}, flags...)...)
	if got.status != 2 {
		t.Fatalf("unbounded cleanup admission: %+v", got)
	}
	if diagnostic := receiptHostDiagnostic(t, got); diagnostic.Code != "runtime.limit" {
		t.Fatalf("depth diagnostic: %+v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "preserved" {
		t.Fatalf("depth admission launched a write: %q %v", data, err)
	}
}

func TestProcessCleanupResourceLimitsKeepConfirmedMutation(t *testing.T) {
	for _, operation := range []string{"flood", "wait"} {
		t.Run(operation, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			path := fileFixture(t, fixture.writes, "data", "before")
			later := filepath.Join(fixture.root, "later")
			source := strings.Replace(fixture.source(t, operation, path), "return Process.Termination", fmt.Sprintf(`later = File.Write.new(File.Path.new(%q), Bytes.new("wrong")); commit later; return Process.Termination`, later), 1)
			flags := append(fixture.flags(), "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes), "--write-root", fixture.root)
			got := run(t, append([]string{"run", sourceFile(t, source), "--format", "json"}, flags...)...)
			diagnostic := receiptHostDiagnostic(t, got)
			code := "runtime.limit"
			if operation == "wait" {
				code = "runtime.cancelled"
			}
			if got.status != 2 || diagnostic.Code != code || diagnostic.ProcessProgress == nil {
				t.Fatalf("cleanup resource limit: %+v", got)
			}
			progress := processFields(t, *diagnostic.ProcessProgress)
			valueJSON(t, progress["start_confirmed"], `true`)
			valueJSON(t, progress["cleanup_complete"], `true`)
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "changed" {
				t.Fatalf("confirmed mutation lost: %q %v", data, err)
			}
			if _, err := os.Stat(later); !os.IsNotExist(err) {
				t.Fatalf("later commit ran after process limit: %v", err)
			}
		})
	}
}

func TestProcessContextInheritedWorkingDirectoryIgnoresPWD(t *testing.T) {
	fixture := newCleanupFixture(t)
	alias := filepath.Join(fixture.root, "logical-cwd")
	if err := os.Symlink(fixture.root, alias); err != nil {
		t.Fatal(err)
	}
	path := fileFixture(t, fixture.writes, "data", "before")
	body := fmt.Sprintf(`context = Process.Context.current(); pwd = Process.Context.environment_value(context, "PWD"); executable = Process.Executable.new(File.Path.new(%q)); command = Process.Command.new(context, executable, List.new("write", %q), Bytes.new("")); result = commit command; return Record.new(code: Process.Termination.exit_code(result.termination), pwd: pwd)`, fixture.executable, path)
	source := sourceFile(t, processContextProgram("Record{code: Int, pwd: String}", body))
	artifact, built := buildForTest(t, source)
	if built.status != 0 {
		t.Fatalf("build inherited directory fixture: %+v", built)
	}
	flags := append(fixture.flags(), "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes))
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			executable, arguments := binary, []string{"run", source, "--format", "json"}
			if native {
				executable, arguments = artifact, []string{"run", "--format", "json"}
			}
			command := exec.Command(executable, append(arguments, flags...)...)
			command.Dir = fixture.root
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "PWD=") {
					command.Env = append(command.Env, entry)
				}
			}
			command.Env = append(command.Env, "PWD="+alias)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			if err := command.Run(); err != nil {
				t.Fatalf("inherited directory follows ambient PWD: %v: %s", err, &stderr)
			}
			fields := processFields(t, fileOutput(t, result{stdout.String(), stderr.String(), command.ProcessState.ExitCode()}))
			valueJSON(t, fields["code"], `"0"`)
			valueJSON(t, fields["pwd"], processLiteral(t, alias))
		})
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "changed" {
		t.Fatalf("inherited directory did not activate cleanup: %q %v", data, err)
	}
}

func TestProcessCleanupScopeLimits(t *testing.T) {
	for _, resource := range []string{"prune", "endpoint"} {
		t.Run(resource, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			flags := fixture.flags()
			var roots []string
			for index := 0; index < 16; index++ {
				if resource == "prune" {
					root, err := os.MkdirTemp(fixture.root, "p")
					if err != nil {
						t.Fatal(err)
					}
					roots = append(roots, root)
					flags = append(flags, "--process-prune-root", cleanupScope(t, fixture.executable, "root", root))
				} else {
					endpoint := filepath.Join(fixture.root, "e"+strconv.Itoa(index))
					listener, err := net.Listen("unix", endpoint)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						if err := listener.Close(); err != nil {
							t.Error(err)
						}
					})
					roots = append(roots, endpoint)
					flags = append(flags, "--process-unix-endpoint", cleanupScope(t, fixture.executable, "endpoint", endpoint))
				}
			}
			operation, path := "connect", roots[0]
			if resource == "prune" {
				operation, path = "unlink", fileFixture(t, roots[0], "entry", "disposable")
			}
			got := run(t, append([]string{"run", sourceFile(t, fixture.source(t, operation, path)), "--format", "json"}, flags...)...)
			valueJSON(t, fileOutput(t, got), `"0"`)
		})
	}
}
