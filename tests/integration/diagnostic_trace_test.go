package integration_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestDiagnosticTraceArguments(t *testing.T) {
	const secret = "trace-raw-header-secret"
	const entriesType = "List<Record{name: String, value: Bytes}>"
	entries := `List.new(Record.new(name: "authorization", value: UTF8.Bytes.new("` + secret + `")))`
	for _, test := range []struct {
		name, source string
		inputs       []map[string]string
	}{
		{"flow", `flow Fail(entries: List<Record{name: String, value: Bytes}>) -> Int! { headers = HTTP.Headers.new(entries); return Math.div(1, 0) }; return Fail(` + entries + `)`, []map[string]string{{"entries": entriesType}}},
		{"callback", `flow Fail(entries: List<Record{name: String, value: Bytes}>, item: Int) -> Int! { headers = HTTP.Headers.new(entries); return Math.div(item, 0) }; return List.map(List.new(1), ` + entries + `, Fail)`, []map[string]string{{"entries": entriesType, "item": "Int"}, {"index": "Int", "element": "Int"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := run(t, "run", sourceFile(t, test.source), "--format", "json")
			diagnostic := receiptHostDiagnostic(t, got)
			if got.status != 1 || diagnostic.Error == nil || diagnostic.Error.Kind != "Math.DivideByZero" {
				t.Fatalf("expected unrelated math failure: %+v", got)
			}
			for _, payload := range []string{secret, base64.StdEncoding.EncodeToString([]byte(secret))} {
				if strings.Contains(got.stdout+got.stderr, payload) {
					t.Fatalf("automatic trace exposed argument payload: %+v", got)
				}
			}
			if len(diagnostic.Error.Trace) != len(test.inputs) {
				t.Fatalf("trace length %d, want %d", len(diagnostic.Error.Trace), len(test.inputs))
			}
			for index, expected := range test.inputs {
				frame := traceFields(t, diagnostic.Error.Trace[index])
				input := traceFields(t, frame["input"])
				if len(input) != len(expected) {
					t.Fatalf("frame argument names: %+v", input)
				}
				for name, typ := range expected {
					summary := input[name]
					if summary.Type != "Record{type: String}" {
						t.Fatalf("argument %s summary type %s", name, summary.Type)
					}
					fields := traceFields(t, summary)
					if len(fields) != 1 || fields["type"].Type != "String" {
						t.Fatalf("argument %s has payload fields: %+v", name, fields)
					}
					valueJSON(t, fields["type"], fmt.Sprintf("%q", typ))
				}
			}
		})
	}
}

func TestDiagnosticExplicitErrorData(t *testing.T) {
	source := `flow Describe(payload: String) -> Error { return Error.new("Explicit", "program data", Record.new(payload: payload)) }; return Describe("explicit-program-value")`
	result := runValue(t, source)
	if result.Kind != "Explicit" || result.Input == nil || len(result.Trace) != 0 {
		t.Fatalf("explicit error value changed: %+v", result)
	}
	fields := traceFields(t, *result.Input)
	valueJSON(t, fields["payload"], `"explicit-program-value"`)
}

func traceFields(t *testing.T, value runtimeValue) map[string]runtimeValue {
	t.Helper()
	var fields map[string]runtimeValue
	if err := json.Unmarshal(value.Value, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}
