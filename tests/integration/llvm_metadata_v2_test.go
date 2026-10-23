package integration_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const metadataV2LiteralSource = `return List.new(0,1,256)`
const metadataV2GradientSource = `flow Square(x:Tensor<FP32>)->Tensor<FP32>!{return Tensor.mul(x,x)};flow Loss(p:Record{x:Tensor<FP32>})->Tensor<FP32>!{return Square(p.x)};flow Run()->Tensor<FP32>!{x=Tensor.new(List.new(2.0),List.new());first=Autodiff.grad(Record.new(x:x),Loss);second=Autodiff.grad(Record.new(x:x),Loss);return Tensor.add(first.gradients.x,second.gradients.x)};return Run()`

const metadataV2CallbackSource = `flow Twice(n:Int)->Int{return Math.mul(n,2)};return List.map(List.new(0,1,256),Twice)`
const metadataV2RecoverableSource = `return Math.div(1,0)`

// The admission fixtures mutate only the current canonical wire document.
func metadataV2FixtureDocument(t *testing.T, module []byte) map[string]any {
	t.Helper()
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(locationMetadataBytes(t, module)))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		t.Fatal(err)
	}
	if root["abi"] != "lo-native-2" {
		t.Fatal("fixture requires canonical ABI2 producer")
	}
	return root
}
func metadataV2Row(t *testing.T, root map[string]any, action string) map[string]any {
	t.Helper()
	for _, raw := range root["locations"].([]any) {
		row := raw.(map[string]any)
		if row["action"] == action {
			return row
		}
	}
	t.Fatalf("missing %s fixture row", action)
	return nil
}
func metadataV2Payload(t *testing.T, root map[string]any, action string) map[string]any {
	t.Helper()
	return metadataV2Row(t, root, action)["payload"].(map[string]any)
}
func metadataV2FirstSpan(root map[string]any) map[string]any {
	return root["spans"].([]any)[0].(map[string]any)
}
func metadataV2Duplicate(t *testing.T, object map[string]any, prefix string) json.RawMessage {
	t.Helper()
	encoded := string(locationMetadataJSON(object))
	return json.RawMessage("{" + prefix + "," + encoded[1:])
}
func TestLLVMMetadataV2Admission(t *testing.T) {
	type fixture struct {
		name, source, message                       string
		valid, originalABI, syntax, coordinateError bool
		edit                                        func(*testing.T, map[string]any)
		raw                                         func(*testing.T, map[string]any) []byte
	}
	fixtures := []fixture{
		{name: "canonical-literals", valid: true},
		{name: "canonical-flow-callback", source: metadataV2CallbackSource, valid: true},
		{name: "canonical-two-derivative-plans", source: metadataV2GradientSource, valid: true},
		{name: "duplicate-unused-valid-tables", valid: true, edit: func(_ *testing.T, root map[string]any) {
			root["files"] = append(root["files"].([]any), root["files"].([]any)[0], "unused.lo")
			root["spans"] = append(root["spans"].([]any), metadataV2FirstSpan(root))
		}},
		{name: "root-files-last-wins", valid: true, raw: func(_ *testing.T, root map[string]any) []byte {
			return []byte(`{"files":false,` + string(locationMetadataJSON(root))[1:])
		}},
		{name: "root-locations-array-last-wins", valid: true, raw: func(_ *testing.T, root map[string]any) []byte {
			return []byte(`{"locations":false,` + string(locationMetadataJSON(root))[1:])
		}},
		{name: "root-locations-nonarray-last-wins", message: "absent metadata locations", raw: func(_ *testing.T, root map[string]any) []byte {
			encoded := string(locationMetadataJSON(root))
			return []byte(encoded[:len(encoded)-1] + `,"locations":false}`)
		}},
		{name: "root-locations-missing", message: "absent metadata locations", edit: func(_ *testing.T, root map[string]any) {
			delete(root, "locations")
		}},
		{name: "row-null", message: "invalid metadata site", edit: func(_ *testing.T, root map[string]any) { root["locations"].([]any)[0] = nil }},
		{name: "row-boolean", message: "invalid metadata site", edit: func(_ *testing.T, root map[string]any) { root["locations"].([]any)[0] = false }},
		{name: "row-number", message: "invalid metadata site", edit: func(_ *testing.T, root map[string]any) { root["locations"].([]any)[0] = 1 }},
		{name: "row-string", message: "invalid metadata site", edit: func(_ *testing.T, root map[string]any) { root["locations"].([]any)[0] = "row" }},
		{name: "row-array", message: "invalid metadata site", edit: func(_ *testing.T, root map[string]any) { root["locations"].([]any)[0] = []any{false} }},
		{name: "row-empty-object", message: "invalid metadata span", edit: func(_ *testing.T, root map[string]any) { root["locations"].([]any)[0] = map[string]any{} }},
		{name: "row-null-span", message: "invalid metadata span", edit: func(_ *testing.T, root map[string]any) {
			root["locations"].([]any)[0].(map[string]any)["span"] = nil
		}},
		{name: "row-unknown-key-order", message: "unknown site metadata field a_unknown", edit: func(_ *testing.T, root map[string]any) {
			root["locations"].([]any)[0] = json.RawMessage(`{"z_unknown":null,"span":false,"a_unknown":false}`)
		}},
		{name: "row-escaped-known-keys-last-wins", valid: true, edit: func(t *testing.T, root map[string]any) {
			rows := root["locations"].([]any)
			rows[0] = metadataV2Duplicate(t, rows[0].(map[string]any), `"sp\u0061n":false,"act\u0069on":false,"ty\u0070e":false,"own\u0065r":false,"fl\u006fw":false,"payl\u006fad":false`)
		}},
		{name: "row-escaped-key-last-value", valid: true, edit: func(_ *testing.T, root map[string]any) {
			rows := root["locations"].([]any)
			encoded := string(locationMetadataJSON(rows[0].(map[string]any)))
			rows[0] = json.RawMessage(`{"action":false,` + strings.Replace(encoded[1:], `"action":`, `"act\u0069on":`, 1))
		}},
		{name: "row-action-last-wins", valid: true, edit: func(t *testing.T, root map[string]any) {
			rows := root["locations"].([]any)
			rows[0] = metadataV2Duplicate(t, rows[0].(map[string]any), `"action":false`)
		}},
		{name: "payload-text-last-wins", valid: true, edit: func(t *testing.T, root map[string]any) {
			row := metadataV2Row(t, root, "literal")
			row["payload"] = metadataV2Duplicate(t, row["payload"].(map[string]any), `"text":[]`)
		}},
		{name: "span-file-last-wins", valid: true, edit: func(t *testing.T, root map[string]any) {
			spans := root["spans"].([]any)
			spans[0] = metadataV2Duplicate(t, spans[0].(map[string]any), `"file_index":-1`)
		}},
		{name: "coordinate-extras-and-last-wins", source: metadataV2RecoverableSource, valid: true, coordinateError: true, edit: func(_ *testing.T, root map[string]any) {
			for _, raw := range root["spans"].([]any) {
				span := raw.(map[string]any)
				for _, key := range []string{"start", "end"} {
					coordinate := span[key].(map[string]any)
					coordinate["a_before"] = nil
					coordinate["d_between"] = json.RawMessage(`[null,{"κ":"é界"}]`)
					coordinate["m_between"] = json.RawMessage(`18446744073709551615`)
					coordinate["z_after"] = json.RawMessage(`{"nested":[true,false,1]}`)
					encoded := string(locationMetadataJSON(coordinate))
					span[key] = json.RawMessage(`{"column":null,` + encoded[1:len(encoded)-1] + `,"a_before":true,"a_b\u0065fore":null}`)
				}
			}
		}},
		{name: "reject-ABI1-identity", originalABI: true},
		{name: "unknown-root", edit: func(_ *testing.T, root map[string]any) { root["unknown"] = true }},
		{name: "unknown-span", edit: func(_ *testing.T, root map[string]any) { metadataV2FirstSpan(root)["unknown"] = true }},
		{name: "unknown-row", edit: func(t *testing.T, root map[string]any) { metadataV2Row(t, root, "literal")["unknown"] = true }},
		{name: "unknown-payload", edit: func(t *testing.T, root map[string]any) { metadataV2Payload(t, root, "literal")["unknown"] = true }},
		{name: "missing-files", edit: func(_ *testing.T, root map[string]any) { delete(root, "files") }},
		{name: "nonstring-file", edit: func(_ *testing.T, root map[string]any) { root["files"].([]any)[0] = false }},
		{name: "span-file-out-of-range", edit: func(_ *testing.T, root map[string]any) { metadataV2FirstSpan(root)["file_index"] = 999999 }},
		{name: "invalid-unused-span", edit: func(_ *testing.T, root map[string]any) {
			root["spans"] = append(root["spans"].([]any), map[string]any{"file_index": 999999, "start": metadataV2FirstSpan(root)["start"], "end": metadataV2FirstSpan(root)["end"]})
		}},
		{name: "row-span-out-of-range", edit: func(t *testing.T, root map[string]any) { metadataV2Row(t, root, "literal")["span"] = 999999 }},
		{name: "missing-coordinate", edit: func(_ *testing.T, root map[string]any) { delete(metadataV2FirstSpan(root), "start") }},
		{name: "zero-coordinate-line", edit: func(_ *testing.T, root map[string]any) {
			metadataV2FirstSpan(root)["start"].(map[string]any)["line"] = 0
		}},
		{name: "reversed-span", edit: func(_ *testing.T, root map[string]any) {
			metadataV2FirstSpan(root)["start"].(map[string]any)["offset"] = 999999
		}},
		{name: "unknown-action", edit: func(t *testing.T, root map[string]any) { metadataV2Row(t, root, "literal")["action"] = "unknown" }},
		{name: "missing-literal-text", edit: func(t *testing.T, root map[string]any) { delete(metadataV2Payload(t, root, "literal"), "text") }},
		{name: "nonfloat-float-bits", edit: func(t *testing.T, root map[string]any) {
			metadataV2Payload(t, root, "literal")["float_bits"] = "0000000000000000"
		}},
		{name: "missing-body-provenance", edit: func(t *testing.T, root map[string]any) { delete(metadataV2Payload(t, root, "body"), "derivative_plan") }},
		{name: "invalid-derivative-plan", source: metadataV2GradientSource, edit: func(t *testing.T, root map[string]any) {
			metadataV2Payload(t, root, "derivative_call")["derivative_plan"] = 999999
		}},
		{name: "missing-derivative-frame", source: metadataV2GradientSource, edit: func(t *testing.T, root map[string]any) {
			delete(metadataV2Payload(t, root, "derivative_enter"), "derivative_frame")
		}},

		{name: "root-closure-before-ABI", message: "unknown program metadata field unknown", edit: func(_ *testing.T, root map[string]any) { root["unknown"] = true; root["abi"] = "wrong" }},
		{name: "files-before-spans", message: "invalid metadata file", edit: func(_ *testing.T, root map[string]any) {
			root["files"].([]any)[0] = false
			metadataV2FirstSpan(root)["file_index"] = 999999
		}},
		{name: "spans-before-rows", message: "invalid metadata column", edit: func(t *testing.T, root map[string]any) {
			metadataV2FirstSpan(root)["start"].(map[string]any)["column"] = nil
			metadataV2Row(t, root, "literal")["action"] = "unknown"
		}},
		{name: "row-span-before-action", message: "unknown site span", edit: func(t *testing.T, root map[string]any) {
			row := metadataV2Row(t, root, "literal")
			row["span"] = 999999
			row["action"] = "unknown"
		}},
		{name: "row-action-before-type", message: "unknown native action unknown", edit: func(t *testing.T, root map[string]any) {
			row := metadataV2Row(t, root, "literal")
			row["action"] = "unknown"
			row["type"] = 999999
		}},
		{name: "row-type-before-owner", message: "unknown site type", edit: func(t *testing.T, root map[string]any) {
			row := metadataV2Row(t, root, "literal")
			row["type"] = 999999
			row["owner"] = false
		}},
		{name: "row-owner-before-flow", message: "invalid metadata owner", edit: func(t *testing.T, root map[string]any) {
			row := metadataV2Row(t, root, "literal")
			row["owner"] = false
			row["flow"] = -1
		}},
		{name: "row-flow-before-payload", message: "invalid metadata flow", edit: func(t *testing.T, root map[string]any) {
			row := metadataV2Row(t, root, "literal")
			row["flow"] = -1
			row["payload"] = false
		}},
		{name: "row-payload-shape", message: "invalid metadata payload", edit: func(t *testing.T, root map[string]any) { metadataV2Row(t, root, "literal")["payload"] = false }},
		{name: "span-offset-before-line", message: "invalid metadata offset", edit: func(_ *testing.T, root map[string]any) {
			start := metadataV2FirstSpan(root)["start"].(map[string]any)
			start["offset"] = "bad"
			start["line"] = 0
		}},
		{name: "span-line-before-column", message: "invalid location coordinate", edit: func(_ *testing.T, root map[string]any) {
			start := metadataV2FirstSpan(root)["start"].(map[string]any)
			start["line"] = 0
			start["column"] = nil
		}},
		{name: "span-start-before-end", message: "invalid metadata column", edit: func(_ *testing.T, root map[string]any) {
			span := metadataV2FirstSpan(root)
			span["start"].(map[string]any)["column"] = nil
			span["end"].(map[string]any)["offset"] = "bad"
		}},
		{name: "span-end-before-reversed", message: "invalid metadata line", edit: func(_ *testing.T, root map[string]any) {
			span := metadataV2FirstSpan(root)
			span["start"].(map[string]any)["offset"] = 999999
			span["end"].(map[string]any)["line"] = "bad"
		}},
		{name: "JSON-before-unknown-key", syntax: true, raw: func(_ *testing.T, root map[string]any) []byte {
			encoded := string(locationMetadataJSON(root))
			return []byte(encoded[:len(encoded)-1] + `,"unknown":1e999}`)
		}},
		{name: "overwritten-value-syntax-error", syntax: true, raw: func(_ *testing.T, root map[string]any) []byte {
			return []byte(`{"files":[1e999],` + string(locationMetadataJSON(root))[1:])
		}},
		{name: "overwritten-locations-syntax-error", syntax: true, raw: func(_ *testing.T, root map[string]any) []byte {
			return []byte(`{"locations":[1e999],` + string(locationMetadataJSON(root))[1:])
		}},
		{name: "overwritten-row-field-syntax-error", syntax: true, edit: func(t *testing.T, root map[string]any) {
			rows := root["locations"].([]any)
			rows[0] = metadataV2Duplicate(t, rows[0].(map[string]any), `"span":1e999`)
		}},
		{name: "unknown-row-field-syntax-error", syntax: true, edit: func(_ *testing.T, root map[string]any) {
			root["locations"].([]any)[0] = json.RawMessage(`{"a_unknown":false,"z_unknown":1e999}`)
		}},
		{name: "nonobject-row-syntax-error", syntax: true, edit: func(_ *testing.T, root map[string]any) {
			root["locations"].([]any)[0] = json.RawMessage(`[false,1e999]`)
		}},
	}
	scenarios := []llvmRuntimeScenario{}
	for _, test := range fixtures {
		fixture := test
		source := fixture.source
		if source == "" {
			source = metadataV2LiteralSource
		}
		scenario := llvmRuntimeScenario{name: fixture.name, source: source, harness: "testdata/native-qualification/metadata-v2.c"}
		if fixture.valid {
			scenario.number = 1
		}
		if fixture.coordinateError {
			scenario.number = 3
		}
		if fixture.syntax || fixture.message != "" {
			scenario.number = 2
		}
		if fixture.valid && !fixture.coordinateError {
			reference := runExecutable(t, binary, "run", sourceFile(t, source), "--format", "json")
			if reference.status != 0 || reference.stderr != "" {
				t.Fatalf("fixture independent language result: %+v", reference)
			}
			scenario.expectedStdout = reference.stdout
		}
		var expectedFile string
		scenario.transform = func(t *testing.T, module []byte) []byte {
			root := metadataV2FixtureDocument(t, module)
			if fixture.originalABI {
				root["abi"] = "lo-native-1"
			}
			expectedFile = root["source"].(map[string]any)["path"].(string)
			if fixture.edit != nil {
				fixture.edit(t, root)
			}
			encoded := locationMetadataJSON(root)
			if fixture.raw != nil {
				encoded = fixture.raw(t, root)
			}
			return replaceLocationMetadata(t, module, encoded)
		}

		if fixture.message != "" {
			scenario.verify = func(t *testing.T, got result) {
				if got.status != 2 || got.stdout != "" || got.stderr != "runtime.host-failure: "+fixture.message+"\n" {
					t.Fatalf("ABI2 admission subject order: %+v; want %q", got, fixture.message)
				}
			}
		}
		if fixture.syntax {
			scenario.verify = func(t *testing.T, got result) {
				if got.status != 2 || got.stdout != "" || !strings.HasPrefix(got.stderr, "runtime.host-failure: program metadata: number out of range at line 1 column ") {
					t.Fatalf("full JSON syntax must precede ABI2 semantic admission: %+v", got)
				}
			}
		}
		if fixture.coordinateError {
			scenario.verify = func(t *testing.T, got result) {
				if got.status != 1 || got.stdout != "" {
					t.Fatalf("coordinate diagnostic: %+v", got)
				}
				var report struct {
					Diagnostics []struct {
						Code string
						Span json.RawMessage
					}
				}
				if err := json.Unmarshal([]byte(got.stderr), &report); err != nil {
					t.Fatal(err)
				}
				if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "runtime.unhandled-error" {
					t.Fatal(got.stderr)
				}
				var span struct{ File string }
				if err := json.Unmarshal(report.Diagnostics[0].Span, &span); err != nil {
					t.Fatal(err)
				}
				if span.File != expectedFile {
					t.Fatalf("original source file changed: %q; want %q", span.File, expectedFile)
				}
				verifyCoordinateSpan(t, report.Diagnostics[0].Span)
			}
		}
		scenarios = append(scenarios, scenario)
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}

func TestLLVMMetadataV2HarnessControl(t *testing.T) {
	scenarios := []llvmRuntimeScenario{}
	for _, control := range []struct {
		name, source string
		number       int
	}{
		{"literals", metadataV2LiteralSource, 1},
		{"callback", metadataV2CallbackSource, 1},
		{"two-derivative-plans", metadataV2GradientSource, 1},
		{"recoverable-error", metadataV2RecoverableSource, 3},
	} {
		scenario := llvmRuntimeScenario{name: control.name, source: control.source, number: control.number, harness: "testdata/native-qualification/metadata-v2.c"}
		if control.number == 1 {
			reference := runExecutable(t, binary, "run", sourceFile(t, control.source), "--format", "json")
			if reference.status != 0 || reference.stderr != "" {
				t.Fatalf("control language result: %+v", reference)
			}
			scenario.expectedStdout = reference.stdout
		} else {
			scenario.verify = func(t *testing.T, got result) {
				var report struct{ Diagnostics []struct{ Code string } }
				if got.status != 1 || got.stdout != "" {
					t.Fatalf("control recoverable result: %+v", got)
				}
				if err := json.Unmarshal([]byte(got.stderr), &report); err != nil {
					t.Fatal(err)
				}
				if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "runtime.unhandled-error" {
					t.Fatal(got.stderr)
				}
			}
		}
		scenarios = append(scenarios, scenario)
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}
