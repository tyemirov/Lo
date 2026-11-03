package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
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

func TestLLVMHTTPValues(t *testing.T) {
	for _, fixture := range []struct{ source, typ, payload string }{
		{`return HTTP.URL.new("HTTP://EXAMPLE.COM/path?q=1")`, "HTTP.URL", `"http://example.com:80/path?q=1"`},
		{`return HTTP.Method.new("POST")`, "HTTP.Method", `"POST"`},
		{`flow Main()->HTTP.Headers! { return HTTP.Headers.new(List.new(Record.new(name:"X-Z",value:UTF8.Bytes.new("z")),Record.new(name:"X-a",value:UTF8.Bytes.new("a")))) }; return Main()`, "HTTP.Headers", `{"entries":{"type":"List<Record{name: String, value: Bytes}>","value":[{"type":"Record{name: String, value: Bytes}","value":{"name":{"type":"String","value":"x-a"},"value":{"type":"Bytes","value":"YQ=="}}},{"type":"Record{name: String, value: Bytes}","value":{"name":{"type":"String","value":"x-z"},"value":{"type":"Bytes","value":"eg=="}}}]}}`},
		{`return HTTP.Response.Text.new(200,"ok")`, "HTTP.Response", `{"status":{"type":"Int","value":"200"},"headers":{"type":"HTTP.Headers","value":{"entries":{"type":"List<Record{name: String, value: Bytes}>","value":[]}}},"body":{"type":"Bytes","value":"b2s="}}`},
	} {
		actual := runProgram(t, sourceFile(t, fixture.source), "run", "--format", "json")
		nativeValueCheck(fixture.typ, fixture.payload)(t, actual)
	}
}

func TestLLVMHTTPKeepAlivePipelining(t *testing.T) {
	p := startServer(t, `flow Handle(r:HTTP.Request)->HTTP.Response{return HTTP.Response.Text.new(200,r.path)}; flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)}`+serveSuffix)
	defer stopServer(t, p)
	connection, err := net.DialTimeout("tcp", p.address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(connection, "GET /first HTTP/1.1\r\nHost: localhost\r\n\r\nGET /second HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	for _, want := range []string{"/first", "/second"} {
		response, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != 200 || string(body) != want {
			t.Fatalf("pipelined response: %d %q %v %v", response.StatusCode, body, readErr, closeErr)
		}
	}
}

func TestLLVMHTTPVerifiedTLS(t *testing.T) {
	directory := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	authority := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Lo local test authority"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	ca, err := x509.CreateCertificate(rand.Reader, authority, authority, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsedCA, err := x509.ParseCertificate(ca)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Lo loopback"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	cert, err := x509.CreateCertificate(rand.Reader, leaf, parsedCA, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private}))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = io.WriteString(w, "verified") }))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()
	roots := filepath.Join(directory, "roots.pem")
	writeLLVMFixture(t, roots, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca}))
	t.Setenv("SSL_CERT_FILE", roots)
	t.Setenv("SSL_CERT_DIR", directory)
	for _, test := range []struct {
		name, url string
		success   bool
	}{{"trusted-address", server.URL, runtime.GOOS != "darwin"}, {"wrong-name", strings.Replace(server.URL, "127.0.0.1", "localhost", 1), false}} {
		t.Run(test.name, func(t *testing.T) {
			input := sourceFile(t, httpClientSource(test.url, "GET", `List.new()`, "", `response=commit target; return response`))
			artifact, built := buildForTest(t, input)
			if built.status != 0 {
				t.Fatalf("build TLS fixture: %+v", built)
			}
			actual := runExecutable(t, artifact, "run", "--http-allow", httpGrant("GET", test.url), "--format", "json")
			if test.success {
				fields := httpResultFields(t, actual)
				valueJSON(t, fields["body"], `"dmVyaWZpZWQ="`)
			} else if actual.status != 1 || !strings.Contains(actual.stderr, "HTTP.ExchangeFailed") {
				t.Fatalf("TLS identity accepted: %+v", actual)
			}
			if bytes.Contains([]byte(actual.stderr), []byte("roots.pem")) {
				t.Fatalf("TLS details leaked in diagnostic: %s", actual.stderr)
			}
		})
	}
	expectedCalls := int32(1)
	if runtime.GOOS == "darwin" {
		expectedCalls = 0
	}
	if calls.Load() != expectedCalls {
		t.Fatalf("TLS request count: %d", calls.Load())
	}
}

func TestLLVMHTTPRequestContentLength(t *testing.T) {
	p := startServer(t, `flow Decode(s:String,b:Bytes)->String!{return UTF8.String.new(b)}; flow Handle(r:HTTP.Request)->HTTP.Response!{values=HTTP.Headers.values(r.headers,"content-length"); value=List.fold(values,"",Decode); return HTTP.Response.Text.new(200,value)}; flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)}`+serveSuffix)
	defer stopServer(t, p)
	status, body := response(t, p, "POST", "/", "ab")
	if status != 200 || body != "2" {
		t.Fatalf("request Content-Length: %d %q", status, body)
	}
}

func TestLLVMHTTPURLCanonicalization(t *testing.T) {
	for index, fixture := range []struct{ url, canonical string }{
		{"http://example.com/a?b=é", "http://example.com:80/a?b=é"},
		{"http://example.com/é?b=é", "http://example.com:80/%C3%A9?b=é"},
		{"http://example.com/%2f?a=%xx", "http://example.com:80/%2f?a=%xx"},
		{"http://example.com/a\\b", "http://example.com:80/a%5Cb"},
		{"http://[::ffff:127.0.0.1]/", "http://127.0.0.1:80/"},
		{"http://example.com:00080/", "http://example.com:80/"},
		{"http://[example.com]/", ""}, {"ftp://host/", ""}, {"http://host:", ""},
		{"http://user:pass@host/", ""}, {"http://host/%xx", ""},
	} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			actual := runProgram(t, sourceFile(t, fmt.Sprintf("return HTTP.URL.new(%q)", fixture.url)), "run", "--format", "json")
			if fixture.canonical == "" {
				nativeFailure(t, actual, 1, "runtime.unhandled-error", "HTTP.InvalidURL")
			} else {
				nativeValueCheck("HTTP.URL", strconv.Quote(fixture.canonical))(t, actual)
			}
		})
	}
}

func TestLLVMHTTPStartupSelfRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	origin := fmt.Sprintf("http://127.0.0.1:%d", port)
	source := fmt.Sprintf(`flow Handle(r:HTTP.Request)->HTTP.Response{return HTTP.Response.Text.new(200,"ready")};flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)};s=HTTP.Server.new(%d,Handle,OnError);commit s;flow Probe()->HTTP.Response!{method=HTTP.Method.new("GET");url=HTTP.URL.new(%q);headers=HTTP.Headers.new(List.new());target=HTTP.Client.Request.new(method,url,headers,Bytes.new(""));r=commit target;return r};return Probe()`, port, origin+"/startup")
	p := startServer(t, source, "--listen", fmt.Sprint(port), "--http-allow", httpGrant("GET", origin))
	stopServer(t, p)
}

func TestLLVMHTTPErrorHandlerOnce(t *testing.T) {
	arguments := strings.TrimSuffix(strings.Repeat(`"x",`, 100), ",")
	source := `flow Handle(r:HTTP.Request)->HTTP.Response!{n=Math.div(1,0);return HTTP.Response.Text.new(200,Int.to_string(n))};flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(99,String.concat(` + arguments + `))}` + serveSuffix
	p := startServer(t, source, "--max-steps", "180")
	client := &http.Client{Timeout: 3 * time.Second}
	r, err := client.Get("http://" + p.address + "/")
	if err == nil {
		_ = r.Body.Close()
		t.Fatal("invalid onError response was sent")
	}
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("invalid onError response did not stop service")
	}
	if p.result.status != 2 || !strings.Contains(p.result.stderr, "runtime.host-failure") {
		t.Fatalf("invalid onError response must be terminal after one invocation: %+v", p.result)
	}
}

func linuxHTTPDriver(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	output := qualificationFixture(t, "linux-http-driver")
	if output == "" {
		helper, err := os.ReadFile(filepath.Join(repository, "tests/integration/testdata/native-http/linux_driver.go.txt"))
		if err != nil {
			t.Fatal(err)
		}
		writeLLVMFixture(t, filepath.Join(directory, "main.go"), helper)
		output = filepath.Join(directory, "linux-http-driver")
		command := exec.Command("go", "build", "-o", output, filepath.Join(directory, "main.go"))
		command.Dir = directory
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "GOOS=") && !strings.HasPrefix(entry, "GOARCH=") && !strings.HasPrefix(entry, "CGO_ENABLED=") && !strings.HasPrefix(entry, "GOWORK=") {
				command.Env = append(command.Env, entry)
			}
		}
		command.Env = append(command.Env, "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0", "GOWORK=off")
		if data, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build isolated guest HTTP driver: %v: %s", err, data)
		}
	}
	return output
}

func TestLLVMLinuxCompanionExecutable(t *testing.T) {
	artifact := linuxLLVMArtifact(t, cliStackProgram("Program.Exit", `return Program.Exit.new(7, Bytes.new("companion stdout"), Bytes.new("companion stderr"))`))
	got := runLinuxLLVMExecutableWithCompanion(t, linuxHTTPDriver(t), artifact, "--run-companion")
	if got.status != 7 || got.stdout != "companion stdout" || got.stderr != "companion stderr" {
		t.Fatalf("Linux companion executable: %+v", got)
	}
}

func TestLLVMLinuxHTTPVerifiedTLS(t *testing.T) {
	artifact := linuxLLVMArtifact(t, httpClientSource("https://127.0.0.1:34443/resource", "GET", `List.new()`, "", `response=commit target; return response`))
	got := runLinuxLLVMExecutableWithCompanion(t, linuxHTTPDriver(t), artifact)
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("Linux verified TLS: %+v", got)
	}
	for _, scenario := range []string{"trusted", "wrong-name", "untrusted"} {
		if !strings.Contains(got.stdout, `"scenario":"`+scenario+`","status":"pass"`) {
			t.Fatalf("Linux TLS receipt missing %s: %+v", scenario, got)
		}
	}
}

func TestLLVMHTTPServerRequestFraming(t *testing.T) {
	p := startServer(t, `flow Handle(r:HTTP.Request)->HTTP.Response{return HTTP.Response.Text.new(200,r.path)};flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)}`+serveSuffix)
	defer stopServer(t, p)
	for _, request := range []string{"GET /bare-lf HTTP/1.1\nHost: localhost\nConnection: close\n\n", "GET /old-version HTTP/1.0\r\n\r\n"} {
		connection, err := net.DialTimeout("tcp", p.address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(connection, request); err != nil {
			t.Fatal(err)
		}
		response, err := http.ReadResponse(bufio.NewReader(connection), nil)
		if err != nil {
			connection.Close()
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		connection.Close()
		if err != nil || closeErr != nil || response.StatusCode != 200 || !strings.Contains(request, string(body)) {
			t.Fatalf("request framing: %d %q %v %v", response.StatusCode, body, err, closeErr)
		}
	}
}

func TestLLVMHTTPClientCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-release }))
	defer server.Close()
	defer close(release)
	source := sourceFile(t, httpClientSource(server.URL, "GET", `List.new()`, "", `response=commit target;return response`))
	artifact, built := buildForTest(t, source)
	if built.status != 0 {
		t.Fatalf("build HTTP cancellation: %+v", built)
	}
	command := exec.Command(artifact, "run", "--http-allow", httpGrant("GET", server.URL), "--format", "json")
	command.Dir = repository
	command.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			<-done
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP exchange did not start")
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP exchange did not stop after cancellation")
	}
	if command.ProcessState.ExitCode() != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"code":"runtime.cancelled"`) || !strings.Contains(stderr.String(), `"http_progress"`) {
		t.Fatalf("HTTP cancellation progress: %d %s %s", command.ProcessState.ExitCode(), stdout.String(), stderr.String())
	}
	progress := httpClientFailureProgress(t, result{status: command.ProcessState.ExitCode(), stdout: stdout.String(), stderr: stderr.String()})
	values := make(map[string]any, len(progress))
	for name, field := range progress {
		var value any
		if err := json.Unmarshal(field.Value, &value); err != nil {
			t.Fatal(err)
		}
		values[name] = value
	}
	if len(values) != 3 || values["remote_outcome"] != "unknown" || !((values["stage"] == "write-request" && values["request_written"] == false) || (values["stage"] == "read-response" && values["request_written"] == true)) {
		t.Fatalf("HTTP cancellation progress tuple: %+v", progress)
	}
}

func TestLLVMHTTPRequestLifetime(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmHTTPRequestLifetimeScenarios())
}
func TestLLVMLinuxHTTPRequestLifetime(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmHTTPRequestLifetimeScenarios())
}
func llvmHTTPRequestLifetimeScenarios() []llvmRuntimeScenario {
	return []llvmRuntimeScenario{{name: "1000-requests", source: `flow Handle(r:HTTP.Request)->HTTP.Response{return HTTP.Response.Text.new(200,"ok")};flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)};s=HTTP.Server.new(34445,Handle,OnError);commit s`, harness: "testdata/native-runtime/http-lifetime.c"}}
}

func TestLLVMHTTPCheckedCleanup(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmHTTPCheckedCleanupScenarios())
}
func TestLLVMLinuxHTTPCheckedCleanup(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmHTTPCheckedCleanupScenarios())
}
func llvmHTTPCheckedCleanupScenarios() []llvmRuntimeScenario {
	scenarios := []llvmRuntimeScenario{}
	for index, name := range []string{"connect", "finalize", "read-response"} {
		scenarios = append(scenarios, llvmRuntimeScenario{name: name, source: httpClientSource(fmt.Sprintf("http://127.0.0.1:%d/", 34446+index), "GET", `List.new()`, "", `response=commit target;return response`), number: index, harness: "testdata/native-runtime/http-cleanup.c"})
	}
	return scenarios
}

func TestLLVMHTTPChunkLengthGrammar(t *testing.T) {
	for _, test := range []struct {
		name, chunk string
		accepted    bool
	}{
		{"leading-plus", "+1", false},
		{"trailing-space", "1 ", true},
		{"trailing-tab", "1\t", true},
		{"extension", "1;extension=ok", true},
		{"space-before-extension", "1 ;extension=ok", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire := "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n" + test.chunk + "\r\na\r\n0\r\n\r\n"
			// The standard HTTP reader independently checks the declared outcomes.
			reference, err := http.ReadResponse(bufio.NewReader(strings.NewReader(wire)), &http.Request{Method: "GET"})
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(reference.Body)
			_ = reference.Body.Close()
			if (readErr == nil) != test.accepted || (test.accepted && string(body) != "a") {
				t.Fatalf("independent chunk expectation: body %q, error %v", body, readErr)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				for call := 0; call < 1; call++ {
					connection, err := listener.Accept()
					if err != nil {
						return
					}
					_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
					request, err := http.ReadRequest(bufio.NewReader(connection))
					if err == nil {
						_, _ = io.Copy(io.Discard, request.Body)
						_ = request.Body.Close()
						_, _ = io.WriteString(connection, wire)
					}
					connection.Close()
				}
			}()
			origin := "http://" + listener.Addr().String()
			input := sourceFile(t, httpClientSource(origin, "GET", `List.new()`, "", `response=commit target;return response`))
			artifact, built := buildForTest(t, input)
			if built.status != 0 {
				t.Fatalf("build chunk fixture: %+v", built)
			}
			arguments := []string{"--http-allow", httpGrant("GET", origin), "--format", "json"}
			actual := runExecutable(t, artifact, append([]string{"run"}, arguments...)...)
			listener.Close()
			<-done
			if test.accepted {
				value := nativeValue(t, actual)
				valueJSON(t, recordField(t, value, "body"), `"YQ=="`)
			} else {
				nativeFailure(t, actual, 1, "runtime.unhandled-error", "HTTP.ExchangeFailed")
			}
		})
	}
}

type llvmHTTPWireResult struct {
	First, Second                             bool
	Body                                      string
	BodyFailure                               bool
	Length, Transfer, Connection, ContentType string
	Trailer                                   string
	Status                                    int
	Exit                                      int
}

func llvmHTTPWireExchange(t *testing.T, artifact, method string) llvmHTTPWireResult {
	t.Helper()
	return llvmHTTPWireExchangeConnection(t, artifact, method, "")
}

func llvmHTTPWireExchangeConnection(t *testing.T, artifact, method, connectionValue string) llvmHTTPWireResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, artifact, "serve", "--listen", "0", "--format", "json")
	command.Dir = repository
	command.Env = os.Environ()
	var stderr bytes.Buffer
	command.Stderr = &stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	var ready serveEvent
	if err := json.NewDecoder(stdout).Decode(&ready); err != nil || ready.Status != "ready" || len(ready.Servers) != 1 {
		t.Fatalf("wire server startup: %v %+v %s", err, ready, stderr.String())
	}
	result := llvmHTTPReadWire(t, ready.Servers[0].Address, method, connectionValue)
	_ = command.Process.Signal(os.Interrupt)
	_ = command.Wait()
	finished = true
	result.Exit = command.ProcessState.ExitCode()
	return result
}
func llvmHTTPReadWire(t *testing.T, address, method, connectionValue string) llvmHTTPWireResult {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	connectionHeader := ""
	if connectionValue != "" {
		connectionHeader = "Connection: " + connectionValue + "\r\n"
	}
	_, err = fmt.Fprintf(connection, "%s / HTTP/1.1\r\nHost: localhost\r\n%s\r\nGET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n", method, connectionHeader)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	first, err := http.ReadResponse(reader, &http.Request{Method: method})
	result := llvmHTTPWireResult{}
	if err == nil {
		result.First = true
		result.Status = first.StatusCode
		result.Length = first.Header.Get("Content-Length")
		result.Transfer = strings.Join(first.TransferEncoding, ",")
		result.Connection = first.Header.Get("Connection")
		result.ContentType = first.Header.Get("Content-Type")
		body, readErr := io.ReadAll(first.Body)
		first.Body.Close()
		result.Body = string(body)
		result.BodyFailure = readErr != nil
		result.Trailer = first.Trailer.Get("X-Foo")
		second, err := http.ReadResponse(reader, &http.Request{Method: "GET"})
		result.Second = err == nil
		if err == nil {
			_, _ = io.Copy(io.Discard, second.Body)
			second.Body.Close()
		}
	}
	return result
}

type llvmHTTPHeader struct{ name, value string }

func llvmHTTPHeaderSource(headers []llvmHTTPHeader) string {
	entries := make([]string, len(headers))
	for index, header := range headers {
		entries[index] = fmt.Sprintf(`Record.new(name:%q,value:UTF8.Bytes.new(%q))`, header.name, header.value)
	}
	return "List.new(" + strings.Join(entries, ",") + ")"
}

func llvmHTTPReferenceWireExchange(t *testing.T, headers []llvmHTTPHeader, status int, body, method, connectionValue string) llvmHTTPWireResult {
	t.Helper()
	var writeFailed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		for _, header := range headers {
			writer.Header().Add(header.name, header.value)
		}
		writer.WriteHeader(status)
		if request.Method != http.MethodHead && body != "" {
			if _, err := io.WriteString(writer, body); err != nil {
				writeFailed.Store(true)
				panic(http.ErrAbortHandler)
			}
		}
	}))
	defer server.Close()
	observed := llvmHTTPReadWire(t, server.Listener.Addr().String(), method, connectionValue)
	// A response write failure is terminal for the native service contract.
	// The reference supplies HTTP bytes only. This status is an explicit expectation.
	if writeFailed.Load() {
		observed.Exit = 2
	}
	return observed
}

func TestLLVMHTTPResponseFraming(t *testing.T) {
	cases := []struct {
		name         string
		headers      []llvmHTTPHeader
		body, method string
		status       int
	}{
		{"chunked", []llvmHTTPHeader{{"transfer-encoding", "chunked"}}, "abc", "GET", 200},
		{"short-length", []llvmHTTPHeader{{"content-length", "1"}}, "abc", "GET", 200},
		{"long-length", []llvmHTTPHeader{{"content-length", "5"}}, "abc", "GET", 200},
		{"close", []llvmHTTPHeader{{"connection", "close"}}, "abc", "GET", 200},
		{"204-framing", []llvmHTTPHeader{{"transfer-encoding", "chunked"}, {"content-length", "5"}}, "", "GET", 204},
		{"304-framing", []llvmHTTPHeader{{"transfer-encoding", "chunked"}, {"content-length", "5"}}, "", "GET", 304},
		{"head-default", nil, "abc", "HEAD", 200},
		{"close-token", []llvmHTTPHeader{{"connection", "keep-alive, close"}}, "abc", "GET", 200},
		{"trailer", []llvmHTTPHeader{{"trailer", "X-Foo"}, {"x-foo", "bar"}}, "abc", "GET", 200},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source := sourceFile(t, fmt.Sprintf(`flow Handle(r:HTTP.Request)->HTTP.Response!{headers=HTTP.Headers.new(%s);return HTTP.Response.new(%d,headers,UTF8.Bytes.new(%q))};flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)}`+serveSuffix, llvmHTTPHeaderSource(test.headers), test.status, test.body))
			native, built := buildForTest(t, source)
			if built.status != 0 {
				t.Fatalf("build response framing: %+v", built)
			}
			expected := llvmHTTPReferenceWireExchange(t, test.headers, test.status, test.body, test.method, "")
			actual := llvmHTTPWireExchange(t, native, test.method)
			if actual != expected {
				t.Fatalf("response framing differs: native %+v standard-library HTTP reference %+v", actual, expected)
			}
		})
	}
}

func TestLLVMHTTPRequestConnectionTokens(t *testing.T) {
	source := sourceFile(t, `flow Handle(r:HTTP.Request)->HTTP.Response{return HTTP.Response.Text.new(200,"ok")};flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)}`+serveSuffix)
	native, built := buildForTest(t, source)
	if built.status != 0 {
		t.Fatalf("build connection tokens: %+v", built)
	}
	expected := llvmHTTPReferenceWireExchange(t, nil, 200, "ok", "GET", "keep-alive, close")
	actual := llvmHTTPWireExchangeConnection(t, native, "GET", "keep-alive, close")
	if actual != expected {
		t.Fatalf("request connection tokens differ: native %+v standard-library HTTP reference %+v", actual, expected)
	}
}

func TestLLVMHTTPResponseContentType(t *testing.T) {
	p := startServer(t, `flow Handle(r:HTTP.Request)->HTTP.Response{return HTTP.Response.Bytes.new(200,r.body)};flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)}`+serveSuffix)
	defer stopServer(t, p)
	payloads := [][]byte{nil, []byte("text"), []byte("  <hTmL>"), []byte("<!-- comment"), []byte("\n<?xml version"), []byte("%PDF-1.7"), []byte("%!PS-Adobe-"), {0xfe, 0xff, 0, 1}, {0xff, 0xfe, 0, 1}, {0xef, 0xbb, 0xbf, 0}, {0, 0, 1, 0}, []byte("BM"), []byte("GIF89a"), []byte("RIFFxxxxWEBPVP"), []byte("\x89PNG\r\n\x1a\n"), {0xff, 0xd8, 0xff}, []byte("FORMxxxxAIFF"), []byte("ID3"), []byte("OggS\x00"), []byte("MThd\x00\x00\x00\x06"), []byte("RIFFxxxxAVI "), []byte("RIFFxxxxWAVE"), {0x1a, 0x45, 0xdf, 0xa3}, []byte("OTTO"), []byte("ttcf"), []byte("wOFF"), []byte("wOF2"), {0x1f, 0x8b, 0x08}, []byte("PK\x03\x04"), []byte("Rar!\x1a\x07\x00"), []byte("\x00asm"), {0, 1, 2}, bytes.Repeat([]byte("a"), 4096)}
	client := &http.Client{Timeout: 3 * time.Second}
	for index, payload := range payloads {
		response, err := client.Post("http://"+p.address+"/", "", bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		want := http.DetectContentType(payload)
		if len(payload) == 0 {
			want = ""
		}
		if readErr != nil || closeErr != nil || !bytes.Equal(body, payload) || response.Header.Get("Content-Type") != want || response.Header.Get("Date") == "" {
			t.Fatalf("content type case %d: %q want %q body %d errors %v %v", index, response.Header.Get("Content-Type"), want, len(body), readErr, closeErr)
		}
	}
}

func TestLLVMHTTPSharedRequestDeadline(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer upstream.Close()
	defer close(release)
	source := fmt.Sprintf(`flow Handle(r:HTTP.Request)->HTTP.Response!{method=HTTP.Method.new("GET");url=HTTP.URL.new(%q);headers=HTTP.Headers.new(List.new());target=HTTP.Client.Request.new(method,url,headers,Bytes.new(""));response=commit target;return response};flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)}`+serveSuffix, upstream.URL)
	p := startServer(t, source, "--http-allow", httpGrant("GET", upstream.URL))
	connection, err := net.DialTimeout("tcp", p.address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(8 * time.Second))
	_, err = io.WriteString(connection, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 4\r\nExpect: 100-continue\r\nConnection: close\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	interim, err := http.ReadResponse(reader, &http.Request{Method: "POST"})
	if err != nil || interim.StatusCode != 100 {
		t.Fatalf("request body handshake: %v %v", interim, err)
	}
	_ = interim.Body.Close()
	time.Sleep(4 * time.Second)
	_, err = io.WriteString(connection, "done")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.done:
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("HTTP body and callback do not share the request deadline")
	}
	if p.result.status != 2 || !strings.Contains(p.result.stderr, `"code":"runtime.cancelled"`) || !strings.Contains(p.result.stderr, `"http_progress"`) {
		t.Fatalf("shared HTTP request deadline: %+v", p.result)
	}
}
