package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func assertServerProcessFailure(t *testing.T, got result, operation string) {
	t.Helper()
	diagnostic := receiptHostDiagnostic(t, got)
	if got.status != 2 || diagnostic.Code != "runtime.process-unavailable" || diagnostic.Error != nil {
		t.Fatalf("server Process failure: %+v", got)
	}
	if operation == "" {
		if diagnostic.LastCompleted != nil {
			t.Fatalf("parent activation entered callback diagnostic: %+v", got)
		}
	} else if diagnostic.LastCompleted == nil || diagnostic.LastCompleted.Operation != operation {
		t.Fatalf("completed activation %q missing: %+v", operation, got)
	}
	var document struct {
		Diagnostics []struct {
			Message string   `json:"message"`
			Cleanup []string `json:"cleanup_errors"`
		}
	}
	if err := json.Unmarshal([]byte(got.stderr), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Diagnostics) != 1 || len(document.Diagnostics[0].Cleanup) != 0 || strings.Contains(document.Diagnostics[0].Message, "; cleanup:") {
		t.Fatalf("callback failure was reported again as cleanup: %+v", got)
	}
}

func TestAdapterServerFailureContext(t *testing.T) {
	for _, completedWrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("callback-write-%t", completedWrite), func(t *testing.T) {
			root := fileFixtureRoot(t)
			path := filepath.Join(root, "completed")
			prefix, operation := "", ""
			if completedWrite {
				prefix = fmt.Sprintf(`write = File.Write.new(File.Path.new(%q),UTF8.Bytes.new("done")); commit write; `, path)
				operation = "File.write"
			}
			program := `flow Handle(request:HTTP.Request)->HTTP.Response!{` + prefix + processGateSource(root) + `commit target; return HTTP.Response.Text.new(200,"unexpected")}; flow OnError(error:Error)->HTTP.Response{return HTTP.Response.Text.new(200,"caught")}` + serveSuffix
			server := startServer(t, program, "--write-root", root)
			client := &http.Client{Timeout: 3 * time.Second}
			response, err := client.Get("http://" + server.address + "/")
			if err == nil {
				_ = response.Body.Close()
				t.Fatal("Process host failure became an HTTP response")
			}
			select {
			case <-server.done:
			case <-time.After(7 * time.Second):
				t.Fatal("callback failure did not stop the service")
			}
			assertServerProcessFailure(t, server.result, operation)
			if completedWrite {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "done" {
					t.Fatalf("completed callback write: %q %v", data, err)
				}
			}
		})
	}
}

func TestAdapterServerStartupFailureContext(t *testing.T) {
	root := fileFixtureRoot(t)
	program := `flow Handle(request:HTTP.Request)->HTTP.Response{return HTTP.Response.Text.new(200,"ok")}; flow OnError(error:Error)->HTTP.Response{return HTTP.Response.Text.new(500,"error")}; flow Main()->Int!{server=HTTP.Server.new(0,Handle,OnError); commit server; ` + processGateSource(root) + `commit target; return 0}; return Main()`
	got := run(t, "serve", sourceFile(t, program), "--listen", "0", "--format", "json")
	assertServerProcessFailure(t, got, "HTTP.listen")
}

func TestLLVMServerCallbackIndependentCleanup(t *testing.T) {
	var scenarios []llvmRuntimeScenario
	for _, completedWrite := range []bool{false, true} {
		prefix := ""
		if completedWrite {
			prefix = `write=File.Write.new(File.Path.new("completed"),UTF8.Bytes.new("done"));commit write; `
		}
		program := `flow Handle(request:HTTP.Request)->HTTP.Response!{` + prefix + processGateSource(repository) + `commit target;return HTTP.Response.Text.new(200,"unexpected")};flow OnError(error:Error)->HTTP.Response{return HTTP.Response.Text.new(200,"caught")};server=HTTP.Server.new(34521,Handle,OnError);commit server`
		scenarios = append(scenarios, llvmRuntimeScenario{name: fmt.Sprintf("callback-write-%t-and-session-cleanup", completedWrite), source: program, harness: "testdata/native-qualification/server-process-cleanup.c", verify: func(t *testing.T, got result) {
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("server cleanup fixture: %+v", got)
			}
			diagnostic := receiptHostDiagnostic(t, result{stderr: got.stdout})
			if diagnostic.Code != "runtime.process-unavailable" || (completedWrite && (diagnostic.LastCompleted == nil || diagnostic.LastCompleted.Operation != "File.write")) || (!completedWrite && diagnostic.LastCompleted != nil) {
				t.Fatalf("callback primary or receipt lost: %+v", got)
			}
			var document struct {
				Diagnostics []struct {
					Message string   `json:"message"`
					Cleanup []string `json:"cleanup_errors"`
				}
			}
			if err := json.Unmarshal([]byte(got.stdout), &document); err != nil {
				t.Fatal(err)
			}
			failure := document.Diagnostics[0]
			if len(failure.Cleanup) != 1 || !strings.Contains(failure.Cleanup[0], "close runtime resources") || strings.Contains(failure.Cleanup[0], "Process activation") || strings.Count(failure.Message, "Process activation") != 1 || strings.Count(failure.Message, "; cleanup:") != 1 {
				t.Fatalf("independent cleanup composition: %+v", got)
			}
		}})
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}
