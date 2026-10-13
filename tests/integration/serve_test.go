package integration_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type serveEvent struct {
	Command string `json:"command"`
	Status  string `json:"status"`
	Servers []struct {
		Address string `json:"address"`
	} `json:"servers"`
}
type serverProcess struct {
	cmd       *exec.Cmd
	events    chan serveEvent
	done      chan struct{}
	result    result
	address   string
	addresses []string
}

func startServer(t *testing.T, source string, extra ...string) *serverProcess {
	t.Helper()
	return startServerFile(t, sourceFile(t, source), extra...)
}
func startServerFile(t *testing.T, path string, extra ...string) *serverProcess {
	t.Helper()
	args := append([]string{"serve", path, "--listen", "0", "--format", "json"}, extra...)
	p := &serverProcess{cmd: exec.Command(binary, args...), events: make(chan serveEvent, 4), done: make(chan struct{})}
	p.cmd.Dir = repository
	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	p.cmd.Stderr = &stderr
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		decoder := json.NewDecoder(stdout)
		for {
			var event serveEvent
			err := decoder.Decode(&event)
			if err != nil {
				if err != io.EOF {
					p.result.stdout = err.Error()
				}
				break
			}
			p.events <- event
		}
		err := p.cmd.Wait()
		if err != nil {
			if _, ok := err.(*exec.ExitError); !ok {
				p.result.stdout += err.Error()
			}
		}
		p.result.status = p.cmd.ProcessState.ExitCode()
		p.result.stderr = stderr.String()
		close(p.done)
	}()
	t.Cleanup(func() {
		select {
		case <-p.done:
			return
		default:
		}
		if err := p.cmd.Process.Kill(); err != nil && !strings.Contains(err.Error(), "already finished") {
			t.Errorf("kill server: %v", err)
		}
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			t.Error("server did not exit after kill")
		}
	})
	select {
	case event := <-p.events:
		if event.Command != "serve" || event.Status != "ready" || len(event.Servers) == 0 {
			t.Fatalf("startup event: %+v", event)
		}
		p.address = event.Servers[0].Address
		for _, server := range event.Servers {
			p.addresses = append(p.addresses, server.Address)
		}
	case <-p.done:
		t.Fatalf("server exited before readiness: %+v", p.result)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not report readiness")
	}
	return p
}
func stopServer(t *testing.T, p *serverProcess) {
	t.Helper()
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.done:
	case <-time.After(7 * time.Second):
		t.Fatal("server did not stop")
	}
	if p.result.status != 0 || p.result.stderr != "" || p.result.stdout != "" {
		t.Fatalf("shutdown: %+v", p.result)
	}
	select {
	case event := <-p.events:
		if event.Status != "stopped" {
			t.Fatalf("shutdown event: %+v", event)
		}
	default:
		t.Fatal("missing stopped event")
	}
	conn, err := net.DialTimeout("tcp", p.address, time.Second)
	if err == nil {
		_ = conn.Close()
		t.Fatal("listener remained open after shutdown")
	}
}
func response(t *testing.T, p *serverProcess, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, "http://"+p.address+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Probe", "present")
	client := &http.Client{Timeout: 3 * time.Second}
	r, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(r.Body)
	closeErr := r.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("response body: %v, %v", readErr, closeErr)
	}
	return r.StatusCode, string(data)
}

const serveSuffix = `
server = HTTP.Server.new(0, Handle, OnError)
commit server
`

func TestServeEchoExample(t *testing.T) {
	p := startServerFile(t, filepath.Join(repository, "examples/http_echo.lo"))
	status, body := response(t, p, "POST", "/example", "café")
	if status != 200 || body != "café" {
		t.Fatalf("HTTP example: %d %q", status, body)
	}
	stopServer(t, p)
}

func TestServeHTTP(t *testing.T) {
	source := `
flow Handle(request: HTTP.Request) -> HTTP.Response! {
  probe = Map.get(request.headers, "X-Probe")
  body = UTF8.String.new(request.body)
  message = String.concat(request.method, " ", request.path, " ", probe, " ", body)
  return HTTP.Response.Text.new(201, message)
}
flow OnError(error: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, error.kind) }
` + serveSuffix
	p := startServer(t, source)
	status, body := response(t, p, "POST", "/hello?ignored=yes", "é😀")
	if status != 201 || body != "POST /hello present é😀" {
		t.Fatalf("HTTP response: %d %q", status, body)
	}
	stopServer(t, p)
}

func TestServeLoErrors(t *testing.T) {
	source := `
flow Handle(request: HTTP.Request) -> HTTP.Response! {
  n = Math.div(1, 0)
  return HTTP.Response.Text.new(200, Int.to_string(n))
}
flow OnError(error: Error) -> HTTP.Response {
  return HTTP.Response.Text.new(400, String.concat(error.kind, ":", Int.to_string(List.length(error.trace))))
}
` + serveSuffix
	p := startServer(t, source)
	for i := 0; i < 2; i++ {
		status, body := response(t, p, "GET", "/", "")
		if status != 400 || body != "Math.DivideByZero:2" {
			t.Fatalf("adapter error: %d %q", status, body)
		}
	}
	stopServer(t, p)
}

func TestServeRequestBoundary(t *testing.T) {
	source := `
flow Handle(request: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "handler entered") }
flow OnError(error: Error) -> HTTP.Response { return HTTP.Response.Text.new(413, String.concat(error.kind, ":", Int.to_string(List.length(error.trace)))) }
` + serveSuffix
	p := startServer(t, source)
	status, body := response(t, p, "POST", "/", strings.Repeat("a", (1<<20)+1))
	if status != 413 || body != "HTTP.RequestTooLarge:1" {
		t.Fatalf("request boundary: %d %q", status, body)
	}
	stopServer(t, p)
}

func TestServeResponseBoundary(t *testing.T) {
	source := `
flow Handle(request: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(99, "invalid") }
flow OnError(error: Error) -> HTTP.Response { return HTTP.Response.Text.new(502, error.kind) }
` + serveSuffix
	p := startServer(t, source)
	status, body := response(t, p, "GET", "/", "")
	if status != 502 || body != "HTTP.InvalidResponse" {
		t.Fatalf("response boundary: %d %q", status, body)
	}
	stopServer(t, p)
}

func TestServeStartupFailures(t *testing.T) {
	flows := `flow Handle(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "ok") }; flow OnError(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, "error") }; `
	for _, port := range []string{"-1", "65536", "18446744073709551616"} {
		got := run(t, "serve", sourceFile(t, flows+"s = HTTP.Server.new("+port+", Handle, OnError); commit s"), "--format", "json")
		if got.status != 1 || got.stdout != "" || !strings.Contains(got.stderr, `"kind":"HTTP.ServerStartFailed"`) {
			t.Fatalf("invalid port: %+v", got)
		}
	}
	got := run(t, "serve", sourceFile(t, flows+serveSuffix), "--format", "json")
	if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.capability-denied" {
		t.Fatalf("listen authorization: %+v", got)
	}
	got = run(t, "run", sourceFile(t, flows+serveSuffix), "--format", "json")
	if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.server-requires-serve" {
		t.Fatalf("run lifecycle: %+v", got)
	}
	got = run(t, "serve", sourceFile(t, "return 1"), "--format", "json")
	if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.no-server" {
		t.Fatalf("empty service: %+v", got)
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port
	source := flows + fmt.Sprintf("s = HTTP.Server.new(%d, Handle, OnError); commit s", port)
	got = run(t, "serve", sourceFile(t, source), "--listen", strconv.Itoa(port), "--format", "json")
	if got.status != 1 || got.stdout != "" || !strings.Contains(got.stderr, `"kind":"HTTP.ServerStartFailed"`) {
		t.Fatalf("startup failure: %+v", got)
	}
}

func TestServeServerLimit(t *testing.T) {
	program := `flow H(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "ok") }; flow E(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, e.kind) }; s = HTTP.Server.new(0,H,E);` + strings.Repeat("commit s;", 65)
	got := run(t, "serve", sourceFile(t, program), "--listen", "0", "--format", "json")
	if got.status != 2 || got.stdout != "" || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" {
		t.Fatalf("active server limit: %+v", got)
	}
}

func TestServeRepeatedActivationAndConcurrentRequests(t *testing.T) {
	source := `
flow Handle(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, r.path) }
flow OnError(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, e.kind) }
unused = HTTP.Server.new(-1, Handle, OnError)
server = HTTP.Server.new(0, Handle, OnError)
commit server
commit server
`
	p := startServer(t, source)
	if len(p.addresses) != 2 || p.addresses[0] == p.addresses[1] {
		t.Fatalf("repeated server commits: %v", p.addresses)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			client := &http.Client{Timeout: 3 * time.Second}
			path := fmt.Sprintf("/request-%d", i)
			r, err := client.Get("http://" + p.addresses[i%2] + path)
			if err != nil {
				t.Error(err)
				return
			}
			data, err := io.ReadAll(r.Body)
			closeErr := r.Body.Close()
			if err != nil || closeErr != nil || r.StatusCode != 200 || string(data) != path {
				t.Errorf("request isolation: %d %q %v %v", r.StatusCode, data, err, closeErr)
			}
		}(i)
	}
	wg.Wait()
	stopServer(t, p)
	for _, address := range p.addresses {
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if err == nil {
			_ = conn.Close()
			t.Errorf("listener remained open: %s", address)
		}
	}
}

func TestServeFatalBoundaryFailures(t *testing.T) {
	for _, tc := range []struct{ name, source, code string }{
		{"invalid-error-response", `flow Handle(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(99, "bad") }; flow OnError(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(99, "also bad") }` + serveSuffix, "runtime.host-failure"},
		{"authorization", `flow Handle(r: HTTP.Request) -> HTTP.Response! { bytes = File.read(File.Path.new("unauthorized")); return HTTP.Response.Bytes.new(200, bytes) }; flow OnError(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(200, "caught host failure") }` + serveSuffix, "runtime.capability-denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := startServer(t, tc.source)
			client := &http.Client{Timeout: 3 * time.Second}
			r, err := client.Get("http://" + p.address + "/")
			if err == nil {
				_ = r.Body.Close()
				t.Fatal("fatal boundary failure produced an HTTP response")
			}
			select {
			case <-p.done:
			case <-time.After(7 * time.Second):
				t.Fatal("fatal HTTP failure did not stop the service")
			}
			if p.result.status != 2 || diagnostics(t, p.result.stderr).Diagnostics[0].Code != tc.code {
				t.Fatalf("fatal failure: %+v", p.result)
			}
		})
	}
}

func TestServeBodylessResponses(t *testing.T) {
	for _, status := range []int{204, 205, 304} {
		p := startServer(t, fmt.Sprintf(`flow Handle(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(%d, "forbidden") }; flow OnError(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(502, e.kind) }`, status)+serveSuffix)
		got, body := response(t, p, "GET", "/", "")
		if got != 502 || body != "HTTP.InvalidResponse" {
			t.Fatalf("bodyless status: %d %q", got, body)
		}
		stopServer(t, p)
	}
	p := startServer(t, `flow Handle(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "omitted for HEAD") }; flow OnError(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, e.kind) }`+serveSuffix)
	status, body := response(t, p, "HEAD", "/", "")
	if status != 200 || body != "" {
		t.Fatalf("HEAD: %d %q", status, body)
	}
	stopServer(t, p)
}

func TestServeTruncatedRequest(t *testing.T) {
	p := startServer(t, `flow Handle(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "entered") }; flow OnError(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(400, e.kind) }`+serveSuffix)
	conn, err := net.DialTimeout("tcp", p.address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 10\r\nConnection: close\r\n\r\nshort"); err != nil {
		t.Fatal(err)
	}
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	r, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(r.Body)
	closeErr := r.Body.Close()
	if err != nil || closeErr != nil || r.StatusCode != 400 || string(body) != "HTTP.RequestReadFailed" {
		t.Fatalf("truncated request: %d %q %v %v", r.StatusCode, body, err, closeErr)
	}
	stopServer(t, p)
}

func TestServeGracefulActiveRequest(t *testing.T) {
	p := startServer(t, `flow Handle(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Bytes.new(200, r.body) }; flow OnError(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, e.kind) }`+serveSuffix)
	conn, err := net.DialTimeout("tcp", p.address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 4\r\nExpect: 100-continue\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	interim, err := http.ReadResponse(reader, nil)
	if err != nil || interim.StatusCode != 100 {
		t.Fatalf("request-read handshake: %v, %v", interim, err)
	}
	if err := interim.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(conn, "done"); err != nil {
		t.Fatal(err)
	}
	r, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(r.Body)
	closeErr := r.Body.Close()
	if err != nil || closeErr != nil || r.StatusCode != 200 || string(body) != "done" {
		t.Fatalf("active request during shutdown: %d %q %v %v", r.StatusCode, body, err, closeErr)
	}
	select {
	case <-p.done:
	case <-time.After(7 * time.Second):
		t.Fatal("shutdown did not finish after active request")
	}
	if p.result.status != 0 || p.result.stderr != "" {
		t.Fatalf("graceful shutdown: %+v", p.result)
	}
}

func TestServeShutdownPendingConnection(t *testing.T) {
	p := startServer(t, `flow Handle(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "ok") }; flow OnError(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, e.kind) }`+serveSuffix)
	conn, err := net.DialTimeout("tcp", p.address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := fmt.Fprint(conn, "GET / HTTP/1.1\r\nHost: "); err != nil {
		t.Fatal(err)
	}
	// A later accepted request establishes that the earlier connection reached the server.
	status, body := response(t, p, "GET", "/", "")
	if status != 200 || body != "ok" {
		t.Fatalf("server request: %d %q", status, body)
	}
	stopServer(t, p)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var buffer [1]byte
	if _, err := conn.Read(buffer[:]); err == nil {
		t.Fatal("pending request connection remained usable")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("pending request connection remained open")
	}
}

func TestServeRequestValidation(t *testing.T) {
	p := startServer(t, `flow Handle(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "entered") }; flow OnError(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(400, e.kind) }`+serveSuffix)
	for _, request := range []string{
		"GET /%FF HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n",
		"GET / HTTP/1.1\r\nHost: localhost\r\nX-Invalid: \xff\r\nConnection: close\r\n\r\n",
	} {
		conn, err := net.DialTimeout("tcp", p.address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(conn, request); err != nil {
			t.Fatal(err)
		}
		r, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r.Body)
		closeErr := r.Body.Close()
		connErr := conn.Close()
		if err != nil || closeErr != nil || connErr != nil || r.StatusCode != 400 || string(body) != "HTTP.InvalidRequest" {
			t.Fatalf("invalid request: %d %q %v %v %v", r.StatusCode, body, err, closeErr, connErr)
		}
	}
	req, err := http.NewRequest("POST", "http://"+p.address+"/", io.NopCloser(strings.NewReader(strings.Repeat("x", (1<<20)+1))))
	if err != nil {
		t.Fatal(err)
	}
	r, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(r.Body)
	closeErr := r.Body.Close()
	if err != nil || closeErr != nil || r.StatusCode != 400 || string(body) != "HTTP.RequestTooLarge" {
		t.Fatalf("chunked request limit: %d %q %v %v", r.StatusCode, body, err, closeErr)
	}
	stopServer(t, p)
}

func TestServeSelfHandledFailures(t *testing.T) {
	p := startServer(t, `
flow Recover(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(200, e.kind) }
flow Handle(r: HTTP.Request) -> HTTP.Response onError Recover { value = Math.div(1, 0); return HTTP.Response.Text.new(200, Int.to_string(value)) }
flow OnError(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, "adapter error handler") }
flow RecoverStartup(e: Error) -> Int { return 0 }
flow StartInvalid() -> Int onError RecoverStartup { bad = HTTP.Server.new(-1, Handle, OnError); commit bad; return 1 }
ignored = StartInvalid()
`+serveSuffix)
	status, body := response(t, p, "GET", "/", "")
	if status != 200 || body != "Math.DivideByZero" {
		t.Fatalf("self-handled error: %d %q", status, body)
	}
	stopServer(t, p)
}
