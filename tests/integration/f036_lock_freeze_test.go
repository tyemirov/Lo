package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestF036SharedSessionLockFreeze(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Darwin process and audit-token fixtures")
	}
	fixture := newF036Fixture(t)
	later := filepath.Join(fixture.root, "later-lock")
	writeLLVMFixture(t, later, nil)
	source := fmt.Sprintf(`
flow Run()->HTTP.Response!{context=Process.Context.new(Map.new(),File.Path.new(%q));executable=Process.Executable.new(File.Path.new(%q));command=Process.Command.new(context,executable,List.new(%q,"gate",%q,%q,"initial"),Bytes.new(""));result=commit command;return HTTP.Response.Text.new(200,"finished")}
flow Acquire(path:File.Path)->HTTP.Response!{target=Lock.File.new(path);lease=commit target;return HTTP.Response.Text.new(200,"acquired")}
flow NoRuns()->List<Int>{return List.new()}
flow NoPaths()->List<File.Path>{return List.new()}
flow Handle(request:HTTP.Request)->HTTP.Response!{runs=Match.new(request.path,"/run":List.new(0),_:NoRuns());flow RunStep(total:Int,item:Int)->Int!{response=Run();return 0};started=Flow.fold(runs,0,RunStep);paths=Match.new(request.path,"/run":NoPaths(),"/same":List.new(File.Path.new(%q)),_:List.new(File.Path.new(%q)));flow LockStep(total:Int,path:File.Path)->Int!{response=Acquire(path);return 0};finished=Flow.fold(paths,started,LockStep);return HTTP.Response.Text.new(200,Match.new(request.path,"/run":"finished",_:"acquired"))}
flow Failed(error:Error)->HTTP.Response{return HTTP.Response.Text.new(409,String.concat(error.kind,":",error.message))}
flow Main()->Int!{target=Lock.File.new(File.Path.new(%q));lease=commit target;server=HTTP.Server.new(0,Handle,Failed);commit server;return 0};return Main()
`, fixture.work, fixture.command, fixture.socket, fixture.ready, fixture.release, fixture.lock, later, fixture.lock)
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "concurrent-http-contexts", number: 14, source: source,
		harness: "testdata/native-runtime/process-ownership-provider.c", hostBootstrap: "lo_test_host_init",
		sdkHarness: true, harnessTransform: fixture.transform,
		run: func(t *testing.T, artifact, work string) {
			stdoutPath, stderrPath := filepath.Join(work, "server.stdout"), filepath.Join(work, "server.stderr")
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
			arguments := fixture.flags(t, artifact)
			arguments[0] = "serve"
			arguments = append(arguments, "--listen", "0", "--lock-file", later, "--process-kernel-limit", "4096")
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
						t.Error("HTTP caller did not exit after teardown")
					}
				}
			}()
			var ready serveEvent
			fixture.wait(t, "HTTP readiness", func() bool {
				contents, err := os.ReadFile(stdoutPath)
				return err == nil && json.NewDecoder(bytes.NewReader(contents)).Decode(&ready) == nil && ready.Status == "ready" && len(ready.Servers) == 1
			})
			transport := &http.Transport{DisableKeepAlives: true}
			client := &http.Client{Transport: transport, Timeout: 4 * time.Second}
			defer transport.CloseIdleConnections()
			type response struct {
				status int
				body   string
				err    error
			}
			get := func(path string) response {
				reply, err := client.Get("http://" + ready.Servers[0].Address + path)
				if err != nil {
					return response{err: err}
				}
				body, readErr := io.ReadAll(io.LimitReader(reply.Body, 16384))
				closeErr := reply.Body.Close()
				if readErr == nil {
					readErr = closeErr
				}
				return response{reply.StatusCode, string(body), readErr}
			}
			activation := make(chan response, 1)
			go func() { activation <- get("/run") }()
			fixture.wait(t, "pending command", func() bool { _, err := os.Stat(fixture.ready); return err == nil })
			for _, path := range []string{"/later", "/same"} {
				actual := get(path)
				if actual.err != nil || actual.status != 409 || !strings.HasPrefix(actual.body, "Lock.Failed:") || !strings.Contains(actual.body, "remote") {
					t.Fatalf("new lock during remote ownership: %+v", actual)
				}
			}
			independent := fixture
			independent.lock = later
			if independent.lockBusy(t) {
				t.Fatal("rejected lock acquired a lease before freeze validation")
			}
			writeLLVMFixture(t, fixture.release, []byte("release"))
			select {
			case actual := <-activation:
				if actual.err != nil || actual.status != 200 || actual.body != "finished" {
					t.Fatalf("original command: %+v", actual)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("original request did not finish")
			}
			actual := get("/later")
			if actual.err != nil || actual.status != 200 || actual.body != "acquired" {
				t.Fatalf("lock freeze survived proven completion: %+v", actual)
			}
			fixture.signalRole(t, "caller", syscall.SIGTERM)
			select {
			case <-finished:
				waited = true
			case <-time.After(4 * time.Second):
				t.Fatal("HTTP shutdown did not finish")
			}
			fixture.wait(t, "session lock release", func() bool { return !fixture.lockBusy(t) && !independent.lockBusy(t) })
		}}})
}
