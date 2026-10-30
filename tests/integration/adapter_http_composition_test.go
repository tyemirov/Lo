package integration_test

import (
	"bytes"
	"context"
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

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/runtimeops"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
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
		got := run(t, "run", sourceFile(t, program), "--http-allow", httpGrant("GET", server.URL), "--write-root", root, "--format", "json")
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
	got := run(t, "run", sourceFile(t, program), "--format", "json")
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
	got := run(t, "run", sourceFile(t, `url=HTTP.URL.new("http://127.0.0.1:1/private?`+secret+`"); return Error.new("Example.Failure","failed",Record.new(url:url))`), "--format", "json")
	if got.status != 0 || strings.Contains(got.stdout, secret) || !strings.Contains(got.stdout, `http://127.0.0.1:1/`) {
		t.Fatalf("URL snapshot: %+v", got)
	}
}

func TestAdapterHTTPClientPostCommitBudget(t *testing.T) {
	const secret = "completed-exchange-private-sentinel"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("X-Private", secret)
		_, _ = io.WriteString(w, secret)
	}))
	defer server.Close()
	files, err := adapter.OpenFiles(adapter.FileRoots{}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	policy, err := adapter.NewHTTPPolicy([]string{httpGrant("GET", server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := adapter.WithHTTP(files, policy)
	if err != nil {
		t.Fatal(err)
	}
	method, err := value.NewMethod("GET")
	if err != nil {
		t.Fatal(err)
	}
	url, err := value.NewURL(server.URL + "/?" + secret)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := value.NewList(types.HeaderEntries(), nil)
	if err != nil {
		t.Fatal(err)
	}
	headers, err := value.NewHeaders(entries)
	if err != nil {
		t.Fatal(err)
	}
	target, err := value.NewClientRequest(method, url, headers, value.NewBytes(nil))
	if err != nil {
		t.Fatal(err)
	}
	text, err := source.New(filepath.Join(fileFixtureRoot(t), "program.lo"), "result = commit target")
	if err != nil {
		t.Fatal(err)
	}
	program, err := execution.NewProgram(func(session execution.Session) (value.Value, error) {
		for i := 0; i < 4; i++ {
			if err := session.ChargeValue(value.NewBytes(make([]byte, execution.MaxValueWeight-16))); err != nil {
				return nil, err
			}
		}
		return runtimeops.Commit(session, target, text.Span(0, 22))
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, runErr := program.Run(context.Background(), boundary, execution.DefaultBudget(), device.DefaultConfig())
	if output != nil || runErr == nil {
		t.Fatalf("budget result %v error %v", output, runErr)
	}
	var stderr bytes.Buffer
	status := runner.ReportRuntime(&stderr, runner.JsonFormat, runErr)
	var document struct{ Diagnostics []struct{ Code string } }
	var raw struct{ Diagnostics []map[string]json.RawMessage }
	if err := json.Unmarshal(stderr.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stderr.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if status != 2 || len(document.Diagnostics) != 1 || document.Diagnostics[0].Code != "runtime.limit" || calls.Load() != 1 {
		t.Fatalf("completion failure: %s calls=%d", stderr.String(), calls.Load())
	}
	var completed struct {
		Operation  string
		ResultType string `json:"result_type"`
		Status     runtimeValue
		Receipt    json.RawMessage
	}
	if err := json.Unmarshal(raw.Diagnostics[0]["last_completed_activation"], &completed); err != nil {
		t.Fatal(err)
	}
	if completed.Operation != "HTTP.request" || completed.ResultType != "HTTP.Response" || len(completed.Receipt) != 0 {
		t.Fatalf("wrong completion summary: %s", stderr.String())
	}
	valueJSON(t, completed.Status, `"200"`)
	if strings.Contains(stderr.String(), secret) || strings.Contains(stderr.String(), base64.StdEncoding.EncodeToString([]byte(secret))) {
		t.Fatalf("completion payload leak: %s", stderr.String())
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

type failingHTTPBoundary struct {
	execution.Boundary
	err error
}

func (b failingHTTPBoundary) Commit(context.Context, value.Target) (value.Value, error) {
	return nil, b.err
}

func TestAdapterHTTPClientCleanupEvidence(t *testing.T) {
	for _, host := range []bool{false, true} {
		t.Run(fmt.Sprintf("host-%t", host), func(t *testing.T) {
			files, err := adapter.OpenFiles(adapter.FileRoots{}, adapter.NativeFileIO())
			if err != nil {
				t.Fatal(err)
			}
			defer files.Close()
			method, err := value.NewMethod("GET")
			if err != nil {
				t.Fatal(err)
			}
			url, err := value.NewURL("http://127.0.0.1:1/")
			if err != nil {
				t.Fatal(err)
			}
			entries, err := value.NewList(types.HeaderEntries(), nil)
			if err != nil {
				t.Fatal(err)
			}
			headers, err := value.NewHeaders(entries)
			if err != nil {
				t.Fatal(err)
			}
			target, err := value.NewClientRequest(method, url, headers, value.NewBytes(nil))
			if err != nil {
				t.Fatal(err)
			}
			const secret = "close-cause-private-sentinel"
			var primary error = io.ErrUnexpectedEOF
			if host {
				primary = context.Canceled
			}
			boundary := failingHTTPBoundary{files, injectedHTTPFailure{execution.WithOperationCleanup(primary, fmt.Errorf("close %s", secret))}}
			text, err := source.New(filepath.Join(fileFixtureRoot(t), "program.lo"), "commit target")
			if err != nil {
				t.Fatal(err)
			}
			program, err := execution.NewProgram(func(session execution.Session) (value.Value, error) {
				return runtimeops.Commit(session, target, text.Span(0, 13))
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			output, runErr := program.Run(context.Background(), boundary, execution.DefaultBudget(), device.DefaultConfig())
			if output != nil || runErr == nil {
				t.Fatalf("failed exchange result %v %v", output, runErr)
			}
			var stderr bytes.Buffer
			status := runner.ReportRuntime(&stderr, runner.JsonFormat, runErr)
			expected := 1
			if host {
				expected = 2
			}
			if status != expected || !strings.Contains(stderr.String(), "cleanup_errors") || !strings.Contains(stderr.String(), "HTTP resource release failed") || strings.Contains(stderr.String(), secret) {
				t.Fatalf("lost or unsafe cleanup evidence: %s", stderr.String())
			}
		})
	}
}
