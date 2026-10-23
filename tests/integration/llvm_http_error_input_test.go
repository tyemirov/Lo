package integration_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The fixture retains only the real OnError argument at the public callback ABI.
// A trusted replacement entry publishes it through the ordinary CLI formatter.
// This does not model persistence between two legal Lo program evaluations.
func TestLLVMHTTPErrorInputs(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	const headers = `HTTP.Headers.new(List.new(Record.new(name:"X-Probe",value:UTF8.Bytes.new("one")),Record.new(name:"X-Probe",value:UTF8.Bytes.new("two"))))`
	response := func(status int) string {
		return fmt.Sprintf(`HTTP.Response.new(%d,%s,UTF8.Bytes.new("payloadé"))`, status, headers)
	}
	tests := []struct {
		name, kind, message, method string
		path                        []byte
		status                      int
	}{
		{name: "invalid-decoded-UTF8-path", kind: "HTTP.InvalidRequest", message: "request text is not valid UTF-8", method: "PATCH", path: []byte{'/', 255}},
		{name: "declared-body-limit", kind: "HTTP.RequestTooLarge", message: "request body exceeds its byte limit", method: "POST", path: []byte("/declared/body")},
		{name: "chunked-overflow", kind: "HTTP.RequestTooLarge", message: "request body exceeds its byte limit", method: "POST", path: []byte("/chunks/overflow")},
		{name: "truncated-content-length", kind: "HTTP.RequestReadFailed", message: "read request body: unexpected EOF", method: "POST", path: []byte("/length/short")},
		{name: "truncated-chunk", kind: "HTTP.RequestReadFailed", message: "read request body: unexpected EOF", method: "POST", path: []byte("/chunk/short")},
		{name: "response-status99", kind: "HTTP.InvalidResponse", message: "response status must be between 200 and 599", status: 99},
		{name: "response-status600", kind: "HTTP.InvalidResponse", message: "response status must be between 200 and 599", status: 600},
		{name: "response-body204", kind: "HTTP.InvalidResponse", message: "response status prohibits a body", status: 204},
		{name: "response-body205", kind: "HTTP.InvalidResponse", message: "response status prohibits a body", status: 205},
		{name: "response-body304", kind: "HTTP.InvalidResponse", message: "response status prohibits a body", status: 304},
	}
	var scenarios []llvmRuntimeScenario
	for index, test := range tests {
		status := test.status
		if status == 0 {
			status = 200
		}
		source := `flow Handle(r:HTTP.Request)->HTTP.Response!{headers=` + headers + `;return HTTP.Response.new(` + fmt.Sprint(status) + `,headers,UTF8.Bytes.new("payloadé"))};flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,"handled")};server=HTTP.Server.new(34447,Handle,OnError);commit server;return Error.new("fixture.Marker","marker",Record.new())`
		var expected any
		if test.method != "" {
			expected = map[string]any{"method": map[string]any{"type": "Bytes", "value": base64.StdEncoding.EncodeToString([]byte(test.method))}, "path": map[string]any{"type": "Bytes", "value": base64.StdEncoding.EncodeToString(test.path)}}
		} else {
			// Establish the canonical header/value encoding through the independent
			// public interpreter, without reading the native error being asserted.
			input := sourceFile(t, `flow Expected()->HTTP.Response!{return `+response(status)+`};return Expected()`)
			got := runExecutable(t, binary, "run", input, "--format", "json")
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("response input reference fixture: %+v", got)
			}
			var envelope struct{ Result runtimeValue }
			if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
				t.Fatal(err)
			}
			value := successfulValue(t, envelope.Result)
			if value.Type != "HTTP.Response" {
				t.Fatalf("reference response type: %+v", value)
			}
			if err := json.Unmarshal(value.Value, &expected); err != nil {
				t.Fatal(err)
			}
		}
		scenarios = append(scenarios, llvmRuntimeScenario{name: test.name, source: source, number: index, harness: "testdata/native-qualification/http-error-input.c", timeout: 20 * time.Second, transform: httpErrorInputCapture, verify: func(t *testing.T, got result) {
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("HTTP error input ABI fixture failed: %+v", got)
			}
			var envelope struct {
				Command, Status string
				Result          runtimeValue
			}
			if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
				t.Fatalf("decode retained real Error: %v: %s", err, got.stdout)
			}
			value := envelope.Result
			if envelope.Command != "run" || envelope.Status != "ok" || value.Type != "Error" || value.Kind != test.kind || value.Message != test.message || value.Input == nil || value.Input.Type != "Record" {
				t.Fatalf("HTTP error identity/input: %+v", envelope)
			}
			var actual any
			if err := json.Unmarshal(value.Input.Value, &actual); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				want, _ := json.Marshal(expected)
				t.Fatalf("HTTP Error.input changed: got%s want%s", value.Input.Value, want)
			}
		}})
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}

func httpErrorInputCapture(t *testing.T, module []byte) []byte {
	t.Helper()
	var metadata struct{ Flows []struct{ Name string } }
	if err := json.Unmarshal(locationMetadataBytes(t, module), &metadata); err != nil {
		t.Fatal(err)
	}
	tablePattern := regexp.MustCompile(`(?m)^@lo_program_flows = constant \[[0-9]+ x ptr\] \[([^\n]+)\]$`)
	table := tablePattern.FindStringSubmatch(string(module))
	if table == nil {
		t.Fatal("missing generated callback table")
	}
	pointers := regexp.MustCompile(`ptr @([A-Za-z0-9_]+)`).FindAllStringSubmatch(table[1], -1)
	if len(pointers) != len(metadata.Flows) {
		t.Fatal("callback table/metadata count differs")
	}
	symbol := ""
	for index, flow := range metadata.Flows {
		if flow.Name == "OnError" {
			if symbol != "" {
				t.Fatal("duplicate OnError flow")
			}
			symbol = pointers[index][1]
		}
	}
	if symbol == "" {
		t.Fatal("OnError callback missing")
	}
	text := string(module)
	if strings.Count(text, "define void @lo_entry(") != 1 || strings.Count(text, "define void @"+symbol+"(") != 1 {
		t.Fatal("generated ABI definitions differ")
	}
	text = strings.Replace(text, "define void @lo_entry(", "define void @lo_http_original_entry(", 1)
	text = strings.ReplaceAll(text, "@"+symbol+"(", "@lo_http_original_onerror(")
	replacement := strings.Replace(table[0], "ptr @"+symbol, "ptr @lo_http_error_capture", 1)
	text = strings.Replace(text, table[0], replacement, 1)
	return []byte(text + "\ndeclare void @lo_http_error_capture(ptr,ptr,i64,i64,ptr)\n")
}
