package integration_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientResponseGrammar(t *testing.T) {
	for _, test := range []struct {
		name, status, trailer, stage string
	}{
		{name: "valid-status", status: "200"},
		{name: "valid-unassigned-status", status: "299"},
		{name: "signed-status", status: "+200", stage: "read-response"},
		{name: "long-status", status: "0200", stage: "read-response"},
		{name: "short-status", status: "20", stage: "read-response"},
		{name: "nondigit-status", status: "2a0", stage: "read-response"},
		{name: "valid-trailer", status: "200", trailer: "X-Foo: bar\r\nX-Foo: baz\r\n"},
		{name: "folded-trailer", status: "200", trailer: "X-Foo: bar\r\n baz\r\n"},
		{name: "missing-trailer-colon", status: "200", trailer: "INVALID\r\n", stage: "read-body"},
		{name: "empty-trailer-name", status: "200", trailer: ": value\r\n", stage: "read-body"},
		{name: "invalid-trailer-name", status: "200", trailer: "Bad\x7fName: value\r\n", stage: "read-body"},
		{name: "invalid-trailer-value", status: "200", trailer: "X-Foo: bad\x01value\r\n", stage: "read-body"},
		{name: "invalid-folded-value", status: "200", trailer: "X-Foo: bar\r\n bad\x7fvalue\r\n", stage: "read-body"},
		{name: "initial-fold", status: "200", trailer: " bad\r\n", stage: "read-body"},
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
				if err == nil {
					_ = request.Body.Close()
					_, _ = fmt.Fprintf(connection, "HTTP/1.1 %s Status\r\nTransfer-Encoding: chunked\r\n\r\n1\r\na\r\n0\r\n%s\r\n", test.status, test.trailer)
				}
			}()
			directory := t.TempDir()
			later := filepath.Join(directory, "later")
			origin := "http://" + listener.Addr().String()
			tail := fmt.Sprintf(`response=commit target; write=File.Write.new(File.Path.new(%q),Bytes.new("committed")); commit write; return response`, later)
			got := run(t, "run", sourceFile(t, httpClientSource(origin, "GET", `List.new()`, "", tail)), "--http-allow", httpGrant("GET", origin), "--write-root", directory, "--format", "json")
			_ = listener.Close()
			<-done
			if test.stage == "" {
				fields := httpResultFields(t, got)
				valueJSON(t, fields["body"], `"YQ=="`)
				data, err := os.ReadFile(later)
				if err != nil || string(data) != "committed" {
					t.Fatalf("valid exchange did not reach later commit: %q %v", data, err)
				}
				return
			}
			if got.status != 1 || !strings.Contains(got.stderr, "HTTP.ExchangeFailed") || got.stdout != "" {
				t.Fatalf("malformed response succeeded: %+v", got)
			}
			progress := httpClientFailureProgress(t, got)
			valueJSON(t, progress["stage"], fmt.Sprintf("%q", test.stage))
			if _, err := os.Stat(later); !os.IsNotExist(err) {
				t.Fatalf("later commit after malformed response: %v", err)
			}
		})
	}
}

func TestHTTPRequestTargetPath(t *testing.T) {
	p := startServer(t, `flow Handle(r:HTTP.Request)->HTTP.Response{return HTTP.Response.Text.new(200,r.path)};flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)}`+serveSuffix)
	defer stopServer(t, p)
	for _, test := range []struct{ method, target, path string }{
		{"GET", "/hello?q=1", "/hello"},
		{"GET", "http://localhost/hello?q=1", "/hello"},
		{"GET", "https://[::1]:443/a%2Fb%3Fc?q=/other", "/a/b?c"},
		{"GET", "http://localhost/caf%C3%A9", "/café"},
		{"GET", "http://localhost?q=1", ""},
		{"GET", "http://localhost", ""},
		{"GET", "//hello/world?q=1", "//hello/world"},
		{"GET", "*", "*"},
		{"CONNECT", "localhost:443", ""},
	} {
		t.Run(test.target, func(t *testing.T) {
			connection, err := net.DialTimeout("tcp", p.address, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
			if _, err := fmt.Fprintf(connection, "%s %s HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n", test.method, test.target); err != nil {
				t.Fatal(err)
			}
			r, err := http.ReadResponse(bufio.NewReader(connection), nil)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(r.Body)
			closeErr := r.Body.Close()
			if err != nil || closeErr != nil || r.StatusCode != 200 || string(body) != test.path {
				t.Fatalf("request target %q: status=%d path=%q want=%q read=%v close=%v", test.target, r.StatusCode, body, test.path, err, closeErr)
			}
		})
	}
}

func TestHTTPClientDisconnectIsolation(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "body")
	writeLLVMFixture(t, input, bytes.Repeat([]byte("x"), 1<<20))
	source := fmt.Sprintf(`flow Handle(r:HTTP.Request)->HTTP.Response!{body=File.read(File.Path.new(%q));return HTTP.Response.Bytes.new(200,body)};flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)}`, input) + serveSuffix
	p := startServer(t, source, "--read-root", directory)
	resetHTTPResponse(t, p)
	select {
	case <-p.done:
		t.Fatalf("client reset stopped service: %+v", p.result)
	case <-time.After(200 * time.Millisecond):
	}
	status, body := response(t, p, "GET", "/healthy", "")
	if status != 200 || len(body) != 1<<20 {
		t.Fatalf("request after reset: %d %d bytes", status, len(body))
	}
	stopServer(t, p)
}

func TestLLVMHTTPDisconnectAfterCommit(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	directory := t.TempDir()
	input := filepath.Join(directory, "body")
	later := filepath.Join(directory, "committed")
	writeLLVMFixture(t, input, bytes.Repeat([]byte("x"), 1<<20))
	source := fmt.Sprintf(`flow Handle(r:HTTP.Request)->HTTP.Response!{body=File.read(File.Path.new(%q));target=File.Write.new(File.Path.new(%q),Bytes.new("done"));commit target;return HTTP.Response.Bytes.new(200,body)};flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)}`, input, later) + serveSuffix
	p := startServer(t, source, "--read-root", directory, "--write-root", directory)
	resetHTTPResponse(t, p)
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("disconnect after commit did not report terminal progress")
	}
	if p.result.status != 2 || !strings.Contains(p.result.stderr, `"code":"runtime.cancelled"`) || !strings.Contains(p.result.stderr, `"operation":"File.write"`) {
		t.Fatalf("disconnect lost committed effect progress: %+v", p.result)
	}
	data, err := os.ReadFile(later)
	if err != nil || string(data) != "done" {
		t.Fatalf("callback commit: %q %v", data, err)
	}
}

func resetHTTPResponse(t *testing.T, p *serverProcess) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", p.address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	tcp := connection.(*net.TCPConn)
	if err := tcp.SetReadBuffer(1024); err != nil {
		t.Fatal(err)
	}
	_ = tcp.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.WriteString(tcp, "GET /cancel HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(tcp, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	if err := tcp.SetLinger(0); err != nil {
		t.Fatal(err)
	}
	if err := tcp.Close(); err != nil {
		t.Fatal(err)
	}
}
