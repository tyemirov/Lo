package integration_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAdapterHTTPClientFileComposition(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/missing" {
			w.WriteHeader(404)
		}
		_, _ = io.WriteString(w, "received")
	}))
	defer server.Close()
	for _, test := range []struct {
		path    string
		written bool
	}{{"/found", true}, {"/missing", false}} {
		root := fileFixtureRoot(t)
		path := filepath.Join(root, "response.txt")
		program := fmt.Sprintf(`flow Empty() -> List<HTTP.Response> { return List.new() }
flow Save(response: HTTP.Response, path: File.Path) -> Int! { write = File.Write.new(path, response.body); receipt = commit write; return receipt.bytes_written }
flow Main() -> Int! { method = HTTP.Method.new("GET"); url = HTTP.URL.new(%q); headers = HTTP.Headers.new(List.new()); target = HTTP.Client.Request.new(method,url,headers,Bytes.new("")); response = commit target; writes = Match.new(response.status, 200: List.new(response), _: Empty()); flow Step(total: Int, response: HTTP.Response) -> Int! { count = Save(response, File.Path.new(%q)); return Math.add(total,count) }; return Flow.fold(writes,0,Step) }; return Main()`, server.URL+test.path, path)
		got := runProgram(t, sourceFile(t, program), "run", "--http-allow", httpGrant("GET", server.URL), "--write-root", root, "--format", "json")
		output := fileOutput(t, got)
		if test.written {
			valueJSON(t, output, `"8"`)
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "received" {
				t.Fatalf("file %q, %v", data, err)
			}
		} else {
			valueJSON(t, output, `"0"`)
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("unexpected write: %v", err)
			}
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestAdapterHTTPClientInspectionMethod(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	program := httpClientSource(server.URL+"/resource", "POST", `List.new()`, "payload", `response = commit target; return response`)
	_, _, output := inspectSource(t, program)
	var envelope struct {
		Report struct {
			Capabilities []struct {
				Operation string
				Method    *inspectionDescription
				Target    inspectionDescription
			}
		}
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, capability := range envelope.Report.Capabilities {
		if capability.Operation == "HTTP.request" {
			found = true
			if capability.Method == nil || capability.Method.Value != "POST" || capability.Method.Type != "HTTP.Method" || capability.Target.Type != "HTTP.URL" {
				t.Fatalf("incomplete HTTP authority: %s", output)
			}
		}
	}
	if !found || calls.Load() != 0 {
		t.Fatalf("inspection authority or activation: %s calls=%d", output, calls.Load())
	}
}

func TestAdapterHTTPClientDiagnosticTarget(t *testing.T) {
	const secret = "client-target-private-sentinel"
	program := fmt.Sprintf(`flow Main() -> Error! {method = HTTP.Method.new("POST"); url = HTTP.URL.new("http://127.0.0.1:1/private?%s"); headers = HTTP.Headers.new(List.new(Record.new(name: "authorization",value: UTF8.Bytes.new("%s")))); target = HTTP.Client.Request.new(method,url,headers,UTF8.Bytes.new("%s")); return Error.new("Example.Failure","failed",Record.new(target:target)) }; return Main()`, secret, secret, secret)
	got := runProgram(t, sourceFile(t, program), "run", "--format", "json")
	if got.status != 0 {
		t.Fatalf("target snapshot: %+v", got)
	}
	if strings.Contains(got.stdout, secret) || strings.Contains(got.stdout, base64.StdEncoding.EncodeToString([]byte(secret))) {
		t.Fatalf("target payload leaked: %+v", got)
	}
	if !strings.Contains(got.stdout, `HTTP.Client.Request`) {
		t.Fatalf("target type lost: %+v", got)
	}
}

func TestAdapterHTTPClientDiagnosticURL(t *testing.T) {
	const secret = "nominal-url-query-private-sentinel"
	got := runProgram(t, sourceFile(t, `url=HTTP.URL.new("http://127.0.0.1:1/private?`+secret+`"); return Error.new("Example.Failure","failed",Record.new(url:url))`), "run", "--format", "json")
	if got.status != 0 || strings.Contains(got.stdout, secret) || !strings.Contains(got.stdout, `http://127.0.0.1:1/`) {
		t.Fatalf("URL snapshot: %+v", got)
	}
}

type injectedHTTPFailure struct{ cause error }

func (e injectedHTTPFailure) Error() string        { return "HTTP.request read-response failed" }
func (e injectedHTTPFailure) Unwrap() error        { return e.cause }
func (e injectedHTTPFailure) Stage() string        { return "read-response" }
func (e injectedHTTPFailure) RequestWritten() bool { return true }
func (e injectedHTTPFailure) Outcome() string      { return "unknown" }
func (e injectedHTTPFailure) CleanupEvidence() []string {
	return []string{"HTTP resource release failed"}
}
