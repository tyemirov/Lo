package integration_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func cleanupDockerSource(t *testing.T, fixture cleanupFixture, endpoint string, commands ...[]string) string {
	t.Helper()
	var body strings.Builder
	fmt.Fprintf(&body, `context = Process.Context.new(Map.new(Record.new(key: "LO_CLEANUP_DOCKER_SOCKET", value: %q)), File.Path.new(%q)); executable = Process.Executable.new(File.Path.new(%q)); `, endpoint, fixture.root, fixture.executable)
	var results []string
	for index, arguments := range commands {
		var quoted []string
		for _, argument := range append([]string{"--context", "desktop-linux"}, arguments...) {
			quoted = append(quoted, processLiteral(t, argument))
		}
		fmt.Fprintf(&body, `command%d = Process.Command.new(context, executable, List.new(%s), Bytes.new("")); result%d = commit command%d; `, index, strings.Join(quoted, ","), index, index)
		results = append(results, fmt.Sprintf("result%d", index))
	}
	fmt.Fprintf(&body, "return List.new(%s)", strings.Join(results, ","))
	return processContextProgram("List<Process.Result>", body.String())
}

func cleanupDockerServer(t *testing.T, fixture cleanupFixture, handler http.Handler) string {
	t.Helper()
	endpoint := filepath.Join(fixture.root, "daemon")
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
		if err := <-done; err != http.ErrServerClosed {
			t.Errorf("fixture daemon: %v", err)
		}
	})
	return endpoint
}

func TestProcessDockerCleanupRequests(t *testing.T) {
	fixture := newCleanupFixtureSource(t, "process-docker-command.c")
	var mutex sync.Mutex
	var requests []string
	effect := filepath.Join(fixture.prunes, "daemon-effect")
	endpoint := cleanupDockerServer(t, fixture, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		requests = append(requests, request.Method+" "+request.URL.RequestURI())
		if request.Method == "POST" {
			file, err := os.Create(effect)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			_, writeErr := fmt.Fprintln(file, len(requests))
			closeErr := file.Close()
			if writeErr != nil || closeErr != nil {
				t.Errorf("daemon effect: %v %v", writeErr, closeErr)
				w.WriteHeader(500)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(w, `{"SpaceReclaimed":42}`); err != nil {
			t.Error(err)
		}
	}))
	flags := append(fixture.flags(), "--process-unix-endpoint", cleanupScope(t, fixture.executable, "endpoint", endpoint))
	source := cleanupDockerSource(t, fixture, endpoint, []string{"system", "df"}, []string{"buildx", "prune", "--builder", "desktop-linux", "--all", "--force"}, []string{"image", "prune", "--all", "--force"})
	got := fileOutput(t, run(t, append([]string{"run", sourceFile(t, source), "--format", "json"}, flags...)...))
	var results []runtimeValue
	if err := json.Unmarshal(got.Value, &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("Docker results: %+v", got)
	}
	for _, result := range results {
		fields := processFields(t, result)
		termination := processFields(t, fields["termination"])
		valueJSON(t, termination["exit_code"], `"0"`)
		var encoded string
		if err := json.Unmarshal(fields["stdout"].Value, &encoded); err != nil {
			t.Fatal(err)
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || !bytes.Contains(data, []byte(`{"SpaceReclaimed":42}`)) {
			t.Fatalf("daemon response: %q %v", data, err)
		}
	}
	mutex.Lock()
	defer mutex.Unlock()
	expected := []string{"GET /v1.47/system/df", "POST /v1.47/build/prune?all=true", "POST /v1.47/images/prune?all=true"}
	if !reflect.DeepEqual(requests, expected) {
		t.Fatalf("daemon requests: %v", requests)
	}
	data, err := os.ReadFile(effect)
	if err != nil || string(data) != "3\n" {
		t.Fatalf("independent daemon effect: %q %v", data, err)
	}
}

func TestProcessDockerCancellationKeepsConfirmedDaemonEffect(t *testing.T) {
	fixture := newCleanupFixtureSource(t, "process-docker-command.c")
	confirmed := make(chan int, 1)
	release := make(chan struct{})
	defer close(release)
	effect := filepath.Join(fixture.prunes, "confirmed-daemon-effect")
	endpoint := cleanupDockerServer(t, fixture, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		pid, err := strconv.Atoi(request.Header.Get("X-Fixture-Pid"))
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		file, err := os.Create(effect)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		_, writeErr := file.WriteString("confirmed")
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Errorf("daemon confirmation: %v %v", writeErr, closeErr)
			w.WriteHeader(500)
			return
		}
		confirmed <- pid
		select {
		case <-request.Context().Done():
		case <-release:
		}
	}))
	flags := append(fixture.flags(), "--process-unix-endpoint", cleanupScope(t, fixture.executable, "endpoint", endpoint))
	source := sourceFile(t, cleanupDockerSource(t, fixture, endpoint, []string{"image", "prune", "--all", "--force"}))
	command := runtimeCommand(t, append([]string{"run", source, "--format", "json"}, flags...)...)
	command.Dir = repository
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	joined := false
	defer func() {
		if !joined {
			_ = command.Process.Kill()
			<-done
		}
	}()
	var pid int
	select {
	case pid = <-confirmed:
	case err := <-done:
		joined = true
		t.Fatalf("client ended before daemon confirmation: %v %s", err, &stderr)
	case <-time.After(10 * time.Second):
		t.Fatal("no confirmed daemon request")
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		joined = true
	case <-time.After(10 * time.Second):
		t.Fatal("client cancellation did not settle")
	}
	if stdout.Len() != 0 || command.ProcessState.ExitCode() != 2 || !strings.Contains(stderr.String(), `"code":"runtime.cancelled"`) {
		t.Fatalf("cancelled client: %s %s", &stdout, &stderr)
	}
	if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("owned client remains after cancellation: %v", err)
	}
	data, err := os.ReadFile(effect)
	if err != nil || string(data) != "confirmed" {
		t.Fatalf("client cancellation lost confirmed daemon effect: %q %v", data, err)
	}
}

func TestProcessDockerFailureResponses(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(fmt.Sprint(malformed), func(t *testing.T) {
			fixture := newCleanupFixtureSource(t, "process-docker-command.c")
			endpoint := cleanupDockerServer(t, fixture, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if malformed {
					connection, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_, writeErr := connection.Write([]byte("invalid response\r\n\r\n"))
					closeErr := connection.Close()
					if writeErr != nil || closeErr != nil {
						t.Errorf("malformed daemon reply: %v %v", writeErr, closeErr)
					}
					return
				}
				w.WriteHeader(500)
				if _, err := fmt.Fprint(w, "daemon rejected request\n"); err != nil {
					t.Error(err)
				}
			}))
			flags := append(fixture.flags(), "--process-unix-endpoint", cleanupScope(t, fixture.executable, "endpoint", endpoint))
			source := cleanupDockerSource(t, fixture, endpoint, []string{"image", "prune", "--all", "--force"})
			got := fileOutput(t, run(t, append([]string{"run", sourceFile(t, source), "--format", "json"}, flags...)...))
			var results []runtimeValue
			if err := json.Unmarshal(got.Value, &results); err != nil || len(results) != 1 {
				t.Fatalf("daemon failure result: %+v %v", got, err)
			}
			fields := processFields(t, results[0])
			termination := processFields(t, fields["termination"])
			code, stderr := "7", "daemon rejected request\n"
			if malformed {
				code, stderr = "68", ""
			}
			valueJSON(t, termination["exit_code"], processLiteral(t, code))
			valueJSON(t, fields["stdout"], `""`)
			valueJSON(t, fields["stderr"], processLiteral(t, base64.StdEncoding.EncodeToString([]byte(stderr))))
		})
	}
}
