package integration_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func processGateSource(root string) string {
	return fmt.Sprintf(`executable = Process.Executable.new(File.Path.new(%q)); target = Process.Command.new(executable, List.new("private-argument"), Map.new(Record.new(key: "PRIVATE", value: "private-environment")), File.Path.new(%q), UTF8.Bytes.new("private-stdin"));`, filepath.Join(root, "absent-executable"), filepath.Join(root, "absent-directory"))
}

func assertProcessUnavailable(t *testing.T, got result) {
	t.Helper()
	diagnostic := receiptHostDiagnostic(t, got)
	if got.status != 2 || got.stdout != "" || diagnostic.Code != "runtime.process-unavailable" || diagnostic.Error != nil {
		t.Fatalf("Process availability must be a host failure: %+v", got)
	}
	if diagnostic.LastCompleted != nil {
		t.Fatalf("unavailable Process produced completion evidence: %+v", got)
	}
	for _, secret := range []string{"private-argument", "private-environment", "private-stdin"} {
		if strings.Contains(got.stderr, secret) {
			t.Fatalf("Process failure contains payload: %+v", got)
		}
	}
}

func TestAdapterProcessUnavailable(t *testing.T) {
	for _, bound := range []bool{false, true} {
		t.Run(fmt.Sprintf("bound-%t", bound), func(t *testing.T) {
			root := fileFixtureRoot(t)
			later := filepath.Join(root, "later")
			commit := "commit target"
			if bound {
				commit = "outcome = commit target"
			}
			program := `flow Main() -> Int! { ` + processGateSource(root) + commit + fmt.Sprintf(`; next = File.Write.new(File.Path.new(%q), UTF8.Bytes.new("unexpected")); commit next; return 0 }; return Main()`, later)
			got := run(t, "run", sourceFile(t, program), "--write-root", root, "--format", "json")
			assertProcessUnavailable(t, got)
			if _, err := os.ReadFile(later); !os.IsNotExist(err) {
				t.Fatalf("effect after unavailable commit ran: %v", err)
			}
		})
	}
}

func TestAdapterProcessUnavailableBypassesHandlers(t *testing.T) {
	root := fileFixtureRoot(t)
	program := `flow Recover(error: Error) -> Int { return 73 }; flow Attempt() -> Int onError Recover { ` + processGateSource(root) + ` outcome = commit target; return 1 }; return Attempt()`
	assertProcessUnavailable(t, run(t, "run", sourceFile(t, program), "--format", "json"))
	program = `flow Recover(error: Error) -> Int { return 73 }; flow Step(total: Int, item: Int) -> Int! { ` + processGateSource(root) + ` commit target; return Math.add(total,item) }; flow Attempt() -> Int onError Recover { return Flow.fold(List.new(1),0,Step) }; return Attempt()`
	assertProcessUnavailable(t, run(t, "run", sourceFile(t, program), "--format", "json"))
}

func TestAdapterProcessUnavailableInServerCallbacks(t *testing.T) {
	for _, errorCallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("error-callback-%t", errorCallback), func(t *testing.T) {
			root := fileFixtureRoot(t)
			body := processGateSource(root) + ` outcome = commit target; return HTTP.Response.Text.new(200,"unexpected")`
			program := `flow Handle(request: HTTP.Request) -> HTTP.Response! { ` + body + ` }; flow OnError(error: Error) -> HTTP.Response { return HTTP.Response.Text.new(200,"caught host failure") }`
			if errorCallback {
				program = `flow Handle(request: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200,"ok") }; flow Recover(error: Error) -> HTTP.Response { return HTTP.Response.Text.new(200,"caught") }; flow OnError(error: Error) -> HTTP.Response onError Recover { ` + body + ` }`
				got := run(t, "check", sourceFile(t, program+serveSuffix), "--format", "json")
				if got.status == 0 || !strings.Contains(got.stderr, "check.effectful-callback") {
					t.Fatalf("HTTP error callback can activate a command: %+v", got)
				}
				return
			}
			server := startServer(t, program+serveSuffix)
			client := &http.Client{Timeout: 3 * time.Second}
			response, err := client.Get("http://" + server.address + "/")
			if err == nil {
				_ = response.Body.Close()
				t.Fatal("Process host failure became an HTTP response")
			}
			select {
			case <-server.done:
			case <-time.After(7 * time.Second):
				t.Fatal("Process host failure did not stop the server")
			}
			assertProcessUnavailable(t, server.result)
		})
	}
}
