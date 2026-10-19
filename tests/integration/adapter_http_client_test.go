package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func httpClientSource(url, method, headers, body, tail string) string {
	return fmt.Sprintf(`flow Main() -> HTTP.Response! { method = HTTP.Method.new(%q); url = HTTP.URL.new(%q); headers = HTTP.Headers.new(%s); target = HTTP.Client.Request.new(method, url, headers, UTF8.Bytes.new(%q)); %s }; return Main()`, method, url, headers, body, tail)
}
func httpGrant(method, origin string) string { return method + "," + origin + ",127.0.0.1" }
func httpResultFields(t *testing.T, got result) map[string]runtimeValue {
	t.Helper()
	response := fileOutput(t, got)
	if response.Type != "HTTP.Response" {
		t.Fatalf("response type %s", response.Type)
	}
	var fields map[string]runtimeValue
	if err := json.Unmarshal(response.Value, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func TestAdapterHTTPClientExchange(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || string(body) != "payload" || len(r.Header.Values("X-Probe")) != 2 {
			t.Errorf("request method=%s body=%q headers=%v", r.Method, body, r.Header)
		}
		w.Header().Add("X-Repeat", "first")
		w.Header().Add("X-Repeat", "second")
		w.WriteHeader(418)
		_, _ = io.WriteString(w, "result")
	}))
	defer server.Close()
	headers := `List.new(Record.new(name: "X-Probe", value: UTF8.Bytes.new("one")), Record.new(name: "x-probe", value: UTF8.Bytes.new("two")))`
	source := sourceFile(t, httpClientSource(server.URL+"/resource?private=query", "POST", headers, "payload", `response = commit target; return response`))
	fields := httpResultFields(t, run(t, "run", source, "--http-allow", httpGrant("POST", server.URL), "--format", "json"))
	valueJSON(t, fields["status"], `"418"`)
	valueJSON(t, fields["body"], `"cmVzdWx0"`)
	if calls.Load() != 1 {
		t.Fatalf("exchange count %d", calls.Load())
	}
}

func TestAdapterHTTPClientConstruction(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	source := fmt.Sprintf(`flow Make() -> HTTP.Client.Request! { method = HTTP.Method.new("GET"); url = HTTP.URL.new(%q); headers = HTTP.Headers.new(List.new()); return HTTP.Client.Request.new(method, url, headers, Bytes.new("")) }; return Make()`, server.URL)
	got := run(t, "run", sourceFile(t, source), "--format", "json")
	if got.status != 0 || calls.Load() != 0 {
		t.Fatalf("construction activated request: %+v calls=%d", got, calls.Load())
	}
	for _, source := range []string{
		`return HTTP.URL.new("/relative")`, `return HTTP.URL.new("ftp://localhost/")`, `return HTTP.URL.new("http://user:password@localhost/")`,
		`return HTTP.Method.new("bad method")`,
		httpClientSource(server.URL, "CONNECT", `List.new()`, "", `response = commit target; return response`),
		httpClientSource(server.URL, "GET", `List.new(Record.new(name: "host", value: UTF8.Bytes.new("other")))`, "", `response = commit target; return response`),
	} {
		got := run(t, "run", sourceFile(t, source), "--format", "json")
		if got.status != 1 || calls.Load() != 0 {
			t.Fatalf("invalid construction: %+v calls=%d", got, calls.Load())
		}
	}
}

func TestAdapterHTTPClientAuthority(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	source := sourceFile(t, httpClientSource(server.URL, "GET", `List.new()`, "", `response = commit target; return response`))
	for _, args := range [][]string{nil, {"--http-allow", httpGrant("get", server.URL)}, {"--http-allow", "GET," + server.URL + ",127.0.0.2"}} {
		command := append([]string{"run", source, "--format", "json"}, args...)
		got := run(t, command...)
		if got.status != 2 || !strings.Contains(got.stderr, `runtime.capability-denied`) || calls.Load() != 0 {
			t.Fatalf("grant authority: %+v calls=%d", got, calls.Load())
		}
	}
	for _, grant := range []string{"GET," + server.URL, "GET," + server.URL + ",127.0.0.1,extra", "GET," + server.URL + "/path,127.0.0.1", "GET," + server.URL + ",localhost", "GET," + server.URL + ",fe80::1%zone", "GET, " + server.URL + ",127.0.0.1"} {
		got := run(t, "run", source, "--http-allow", grant, "--format", "json")
		if got.status != 2 || calls.Load() != 0 {
			t.Fatalf("invalid grant %s: %+v", grant, got)
		}
	}
}

func TestAdapterHTTPClientRedirectAndProxy(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Location", "/next")
		w.WriteHeader(302)
		_, _ = io.WriteString(w, "redirect")
	}))
	defer server.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	source := sourceFile(t, httpClientSource(server.URL, "GET", `List.new()`, "", `response = commit target; return response`))
	fields := httpResultFields(t, run(t, "run", source, "--http-allow", httpGrant("GET", server.URL), "--format", "json"))
	valueJSON(t, fields["status"], `"302"`)
	if calls.Load() != 1 {
		t.Fatalf("redirect replayed %d requests", calls.Load())
	}
}

func TestAdapterHTTPClientNoReplay(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			calls.Add(1)
			_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
			request, err := http.ReadRequest(bufio.NewReader(connection))
			if err == nil {
				_, _ = io.Copy(io.Discard, request.Body)
				_ = request.Body.Close()
			}
			_ = connection.Close()
		}
	}()
	const secret = "http-client-secret-sentinel"
	origin := "http://" + listener.Addr().String()
	source := sourceFile(t, httpClientSource(origin+"/drop?"+secret, "POST", `List.new(Record.new(name: "authorization", value: UTF8.Bytes.new("`+secret+`")))`, secret, `response = commit target; return response`))
	got := run(t, "run", source, "--http-allow", httpGrant("POST", origin), "--format", "json")
	_ = listener.Close()
	<-done
	if got.status != 1 || !strings.Contains(got.stderr, `HTTP.ExchangeFailed`) || calls.Load() != 1 {
		t.Fatalf("request replay/failure: %+v calls=%d", got, calls.Load())
	}
	if strings.Contains(got.stderr, secret) || strings.Contains(got.stderr, base64.StdEncoding.EncodeToString([]byte(secret))) {
		t.Fatalf("secret in exchange diagnostic: %+v", got)
	}
	if !strings.Contains(got.stderr, `"request_written"`) || !strings.Contains(got.stderr, `"unknown"`) {
		t.Fatalf("missing exchange progress: %+v", got)
	}
}

func TestAdapterHTTPClientBodyLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, strings.Repeat("x", (1<<20)+1)) }))
	defer server.Close()
	source := sourceFile(t, httpClientSource(server.URL, "GET", `List.new()`, "", `response = commit target; return response`))
	got := run(t, "run", source, "--http-allow", httpGrant("GET", server.URL), "--format", "json")
	if got.status != 2 || !strings.Contains(got.stderr, `runtime.limit`) {
		t.Fatalf("body limit: %+v", got)
	}
	progress := httpClientFailureProgress(t, got)
	valueJSON(t, progress["stage"], `"read-body"`)
	valueJSON(t, progress["remote_outcome"], `"unknown"`)
}

func TestAdapterHTTPClientProtocolLimits(t *testing.T) {
	for _, test := range []struct{ name, wire, code string }{
		{"http-1-0-interim", "HTTP/1.0 103 Early Hints\r\nX-Hint: yes\r\n\r\nHTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n", "HTTP.ExchangeFailed"},
		{"http-1-0", "HTTP/1.0 200 OK\r\nContent-Length: 0\r\n\r\n", "HTTP.ExchangeFailed"},
		{"bare-lf", "HTTP/1.1 200 OK\nContent-Length: 0\n\n", "HTTP.ExchangeFailed"},
		{"header-section", "HTTP/1.1 200 OK\r\nX-Large: " + strings.Repeat("x", 64<<10) + "\r\nContent-Length: 0\r\n\r\n", "runtime.limit"},
		{"interim-count", strings.Repeat("HTTP/1.1 103 Early Hints\r\nX-Hint: yes\r\n\r\n", 9) + "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n", "runtime.limit"},
		{"interim-aggregate", strings.Repeat("HTTP/1.1 103 Early Hints\r\nX-Hint: "+strings.Repeat("x", 40<<10)+"\r\n\r\n", 2) + "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n", "runtime.limit"},
		{"protocol-switch", "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n", "HTTP.ExchangeFailed"},
		{"invalid-final-status", "HTTP/1.1 600 Invalid\r\nContent-Length: 0\r\n\r\n", "HTTP.ExchangeFailed"},
		{"bodyless-205", "HTTP/1.1 205 Reset Content\r\nContent-Length: 3\r\n\r\nbad", "HTTP.ExchangeFailed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				connection, err := listener.Accept()
				if err != nil {
					return
				}
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
				request, err := http.ReadRequest(bufio.NewReader(connection))
				if err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, request.Body)
				_ = request.Body.Close()
				_, _ = io.WriteString(connection, test.wire)
			}()
			origin := "http://" + listener.Addr().String()
			source := sourceFile(t, httpClientSource(origin, "GET", `List.new()`, "", `response = commit target; return response`))
			got := run(t, "run", source, "--http-allow", httpGrant("GET", origin), "--format", "json")
			_ = listener.Close()
			<-done
			if !strings.Contains(got.stderr, test.code) || got.stdout != "" {
				t.Fatalf("protocol boundary: %+v", got)
			}
		})
	}
}

func TestAdapterHTTPClientHeadAndEmptyStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/empty" {
			w.WriteHeader(204)
			return
		}
		_, _ = io.WriteString(w, "omitted")
	}))
	defer server.Close()
	for _, test := range []struct{ method, path string }{{"HEAD", "/head"}, {"GET", "/empty"}} {
		source := sourceFile(t, httpClientSource(server.URL+test.path, test.method, `List.new()`, "", `response = commit target; return response`))
		fields := httpResultFields(t, run(t, "run", source, "--http-allow", httpGrant(test.method, server.URL), "--format", "json"))
		valueJSON(t, fields["body"], `""`)
	}
}

func TestAdapterHTTPClientRequestBodyLimit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	root := fileFixtureRoot(t)
	path := fileFixture(t, root, "request-body", strings.Repeat("x", (1<<20)+1))
	source := fmt.Sprintf(`flow Main() -> HTTP.Response! { method = HTTP.Method.new("POST"); url = HTTP.URL.new(%q); headers = HTTP.Headers.new(List.new()); body = File.read(File.Path.new(%q)); target = HTTP.Client.Request.new(method, url, headers, body); result = commit target; return result }; return Main()`, server.URL, path)
	got := run(t, "run", sourceFile(t, source), "--read-root", root, "--http-allow", httpGrant("POST", server.URL), "--format", "json")
	if got.status != 2 || !strings.Contains(got.stderr, "runtime.limit") || calls.Load() != 0 {
		t.Fatalf("request-body limit: %+v calls=%d", got, calls.Load())
	}
	progress := httpClientFailureProgress(t, got)
	valueJSON(t, progress["stage"], `"authorize"`)
	valueJSON(t, progress["request_written"], `false`)
	valueJSON(t, progress["remote_outcome"], `"not_started"`)
}

func TestAdapterHTTPClientTLSAuthority(t *testing.T) {
	var calls atomic.Int32
	serverName := make(chan string, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) { serverName <- info.ServerName; return nil, nil }}
	server.StartTLS()
	defer server.Close()
	origin := strings.Replace(server.URL, "127.0.0.1", "localhost", 1)
	source := sourceFile(t, httpClientSource(origin+"/private", "GET", `List.new()`, "", `response = commit target; return response`))
	got := run(t, "run", source, "--http-allow", httpGrant("GET", origin), "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, "HTTP.ExchangeFailed") || calls.Load() != 0 {
		t.Fatalf("untrusted TLS authority: %+v calls=%d", got, calls.Load())
	}
	select {
	case name := <-serverName:
		if name != "localhost" {
			t.Fatalf("TLS SNI %q", name)
		}
	default:
		t.Fatal("TLS handshake omitted")
	}
}

func httpClientFailureProgress(t *testing.T, got result) map[string]runtimeValue {
	t.Helper()
	var envelope struct {
		Diagnostics []struct {
			Progress *runtimeValue `json:"http_progress"`
			Error    *runtimeValue
		}
	}
	if err := json.Unmarshal([]byte(got.stderr), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Diagnostics) != 1 {
		t.Fatalf("missing HTTP diagnostic: %+v", got)
	}
	progress := envelope.Diagnostics[0].Progress
	if progress == nil && envelope.Diagnostics[0].Error != nil {
		var input map[string]runtimeValue
		if err := json.Unmarshal(envelope.Diagnostics[0].Error.Input.Value, &input); err != nil {
			t.Fatal(err)
		}
		value, ok := input["progress"]
		if ok {
			progress = &value
		}
	}
	if progress == nil {
		t.Fatalf("missing public HTTP progress: %+v", got)
	}
	var fields map[string]runtimeValue
	if err := json.Unmarshal(progress.Value, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["stage"].Type != "String" || fields["request_written"].Type != "Bool" || fields["remote_outcome"].Type != "String" {
		t.Fatalf("invalid HTTP progress: %+v", fields)
	}
	return fields
}

func TestAdapterHTTPClientCancellationProgress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		cancel()
		<-r.Context().Done()
	}))
	defer server.Close()
	policy, err := adapter.NewHTTPPolicy([]string{httpGrant("GET", server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	files, err := adapter.OpenFiles(adapter.FileRoots{}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := adapter.WithHTTP(files, policy)
	if err != nil {
		t.Fatal(err)
	}
	text, err := source.New("http-cancel.lo", httpClientSource(server.URL, "GET", `List.new()`, "", `response = commit target; return response`))
	if err != nil {
		t.Fatal(err)
	}
	checked, diagnostic := compiler.Check(text)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	program, err := eval.Program(checked)
	if err != nil {
		t.Fatal(err)
	}
	output, runErr := program.Run(ctx, boundary, execution.DefaultBudget(), device.DefaultConfig())
	runErr = execution.PreserveCleanup(runErr, files.Close())
	if output != nil {
		t.Fatalf("canceled exchange published %v", output)
	}
	var stderr bytes.Buffer
	status := runner.ReportRuntime(&stderr, runner.JsonFormat, runErr)
	got := result{stderr: stderr.String(), status: status}
	if status != 2 || !strings.Contains(got.stderr, "runtime.cancelled") {
		t.Fatalf("HTTP cancellation: %+v", got)
	}
	progress := httpClientFailureProgress(t, got)
	valueJSON(t, progress["remote_outcome"], `"unknown"`)
}
