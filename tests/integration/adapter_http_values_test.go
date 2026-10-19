package integration_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const headerSecret = "header-secret-sentinel"
const headerEntriesSource = `List.new(Record.new(name: "X-Z", value: UTF8.Bytes.new("z")), Record.new(name: "X-A", value: UTF8.Bytes.new("first")), Record.new(name: "x-a", value: UTF8.Bytes.new("second")))`

func TestAdapterHTTPHeaderValues(t *testing.T) {
	source := `flow Main() -> List<Record{name: String, value: Bytes}>! { headers = HTTP.Headers.new(` + headerEntriesSource + `); return headers.entries }; return Main()`
	got := fileOutput(t, run(t, "run", sourceFile(t, source), "--format", "json"))
	valueJSON(t, got, `[{"type":"Record{name: String, value: Bytes}","value":{"name":{"type":"String","value":"x-a"},"value":{"type":"Bytes","value":"Zmlyc3Q="}}},{"type":"Record{name: String, value: Bytes}","value":{"name":{"type":"String","value":"x-a"},"value":{"type":"Bytes","value":"c2Vjb25k"}}},{"type":"Record{name: String, value: Bytes}","value":{"name":{"type":"String","value":"x-z"},"value":{"type":"Bytes","value":"eg=="}}}]`)
	for _, test := range []struct{ name, want string }{{"X-a", `[{"type":"Bytes","value":"Zmlyc3Q="},{"type":"Bytes","value":"c2Vjb25k"}]`}, {"absent", `[]`}} {
		source := fmt.Sprintf(`flow Main() -> List<Bytes>! { headers = HTTP.Headers.new(%s); return HTTP.Headers.values(headers, %q) }; return Main()`, headerEntriesSource, test.name)
		got := fileOutput(t, run(t, "run", sourceFile(t, source), "--format", "json"))
		valueJSON(t, got, test.want)
	}
}

func TestAdapterHTTPHeaderValidation(t *testing.T) {
	for _, name := range []string{"", "bad name", "bad:name", "héader", "x\r\ny"} {
		source := fmt.Sprintf(`return HTTP.Headers.new(List.new(Record.new(name: %q, value: UTF8.Bytes.new("ok"))))`, name)
		got := run(t, "run", sourceFile(t, source), "--format", "json")
		v := receiptHostDiagnostic(t, got)
		if got.status != 1 || v.Error == nil || v.Error.Kind != "HTTP.InvalidHeaders" {
			t.Fatalf("invalid header name accepted: %+v", v)
		}
	}
	for _, content := range []string{"nul\x00", "line\r", "line\n", "control\x01", "delete\x7f"} {
		literal, err := json.Marshal(content)
		if err != nil {
			t.Fatal(err)
		}
		source := fmt.Sprintf(`return HTTP.Headers.new(List.new(Record.new(name: "x", value: UTF8.Bytes.new(%s))))`, strings.ReplaceAll(string(literal), "\x7f", `\u007f`))
		got := run(t, "run", sourceFile(t, source), "--format", "json")
		v := receiptHostDiagnostic(t, got)
		if got.status != 1 || v.Error == nil || v.Error.Kind != "HTTP.InvalidHeaders" {
			t.Fatalf("invalid header bytes accepted: %+v", v)
		}
	}
	invalidLookup := `flow Main() -> List<Bytes>! { headers = HTTP.Headers.new(List.new()); return HTTP.Headers.values(headers, "bad name") }; return Main()`
	got := run(t, "run", sourceFile(t, invalidLookup), "--format", "json")
	v := receiptHostDiagnostic(t, got)
	if got.status != 1 || v.Error == nil || v.Error.Kind != "HTTP.InvalidHeaders" {
		t.Fatalf("invalid lookup accepted: %+v", v)
	}
	for _, source := range []string{`return HTTP.Response.new(200, Map.new(), Bytes.new(""))`, `return HTTP.Headers.new(List.new(Record.new(name: "x", value: "text")))`} {
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status == 0 {
			t.Fatalf("obsolete header shape accepted: %s", source)
		}
	}
}

func TestAdapterHTTPGeneralResponse(t *testing.T) {
	source := `flow Main() -> HTTP.Response! { headers = HTTP.Headers.new(List.new()); return HTTP.Response.new(200, headers, UTF8.Bytes.new("body")) }; return Main()`
	got := fileOutput(t, run(t, "run", sourceFile(t, source), "--format", "json"))
	if got.Type != "HTTP.Response" {
		t.Fatalf("response type %s", got.Type)
	}
	var fields map[string]runtimeValue
	if err := json.Unmarshal(got.Value, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["headers"].Type != "HTTP.Headers" {
		t.Fatalf("headers type %s", fields["headers"].Type)
	}
	valueJSON(t, fields["body"], `"Ym9keQ=="`)
}

func TestAdapterHTTPRepeatedByteHeaders(t *testing.T) {
	p := startServer(t, `flow Handle(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.new(200, r.headers, r.body) }; flow OnError(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, e.kind) }`+serveSuffix)
	defer stopServer(t, p)
	req, err := http.NewRequest("POST", "http://"+p.address+"/headers", strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Add("X-Repeat", "first")
	req.Header.Add("X-Repeat", "second")
	req.Header.Add("X-Opaque", string([]byte{0xff, '\t', 'x'}))
	response, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || string(body) != "body" || response.StatusCode != 200 {
		t.Fatalf("response %d %q %v %v", response.StatusCode, body, readErr, closeErr)
	}
	if !reflect.DeepEqual(response.Header.Values("X-Repeat"), []string{"first", "second"}) {
		t.Fatalf("repeated headers %v", response.Header)
	}
	if response.Header.Get("X-Opaque") != string([]byte{0xff, '\t', 'x'}) {
		t.Fatalf("opaque header %q", response.Header.Get("X-Opaque"))
	}
}

func TestAdapterHTTPHeaderDiagnosticRedaction(t *testing.T) {
	sources := []string{
		`flow Make(entries: List<Record{name: String, value: Bytes}>) -> HTTP.Headers! { return HTTP.Headers.new(entries) }; return Make(List.new(Record.new(name: "bad name", value: UTF8.Bytes.new("` + headerSecret + `"))))`,
		`flow Fail(headers: HTTP.Headers) -> Int! { return Math.div(1, 0) }; flow Main() -> Int! { headers = HTTP.Headers.new(List.new(Record.new(name: "authorization", value: UTF8.Bytes.new("` + headerSecret + `")))); return Fail(headers) }; return Main()`,
		`flow Fail(response: HTTP.Response) -> Int! { return Math.div(1, 0) }; flow Main() -> Int! { headers = HTTP.Headers.new(List.new(Record.new(name: "authorization", value: UTF8.Bytes.new("` + headerSecret + `")))); response = HTTP.Response.new(200, headers, Bytes.new("")); return Fail(response) }; return Main()`,
	}
	for _, source := range sources {
		got := run(t, "run", sourceFile(t, source), "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, `"error":`) {
			t.Fatalf("expected returned flow error: %+v", got)
		}
		if strings.Contains(got.stdout+got.stderr, headerSecret) || strings.Contains(got.stdout+got.stderr, base64.StdEncoding.EncodeToString([]byte(headerSecret))) {
			t.Fatalf("header secret in diagnostic: %+v", got)
		}
	}
}
