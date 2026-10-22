package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These cases were characterized against the admitted-program runtime before
// location deserialization changed. Wrong shapes remain explicit admission
// errors; JSON syntax and unknown values must still be parsed completely.
func TestLLVMLocationMetadataAdmission(t *testing.T) {
	fixtures := locationMetadataCases()
	if len(fixtures) != 55 {
		t.Fatalf("characterization count: %d", len(fixtures))
	}
	runLocationMetadataAdmission(t, fixtures)
}

func runLocationMetadataAdmission(t *testing.T, fixtures []locationMetadataCase) {
	t.Helper()
	const source = `flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.mul(p.x,p.x) }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(2.0),List.new()); result=Autodiff.grad(Record.new(x:x),Loss); return result.gradients.x }; return Run()`
	harness := locationMetadataHarness(t, `
typedef __UINT64_TYPE__ U64;
typedef struct Program Program;
extern const Program *lo_program_new(void);
extern void lo_program_free(const Program *);
extern U64 lo_runtime_live_programs(void);
extern U64 lo_runtime_live_values(void);
extern int lo_cli_main(int,const char*const*,const char*const*);
int main(void) {
 const Program *program=lo_program_new();
 if ((program!=0)!=LO_CASE) return 10;
 lo_program_free(program);
 if (lo_runtime_live_programs() || lo_runtime_live_values()) return 11;
 if (LO_CASE) return 0;
 const char *args[]={"fixture","metadata",0};
 return lo_cli_main(2,args,0);
}
`)
	var scenarios []llvmRuntimeScenario
	for _, fixture := range fixtures {
		expected := fixture.message
		scenario := llvmRuntimeScenario{name: fixture.name, source: source, harness: harness, transform: func(t *testing.T, module []byte) []byte {
			raw := locationMetadataBytes(t, module)
			var root map[string]any
			if err := json.Unmarshal(raw, &root); err != nil {
				t.Fatal(err)
			}
			raw = fixture.edit(root)
			return replaceLocationMetadata(t, module, raw)
		}, verify: func(t *testing.T, got result) {
			if expected == "" {
				if got.status != 0 || got.stdout != "" || got.stderr != "" {
					t.Fatalf("valid metadata: %+v", got)
				}
				return
			}
			if got.status != 2 || got.stdout != "" {
				t.Fatalf("invalid metadata status: %+v", got)
			}
			want := "runtime.host-failure: " + expected
			if strings.HasPrefix(expected, "program metadata:") {
				if !strings.HasPrefix(got.stderr, want+" at line 1 column ") || !regexp.MustCompile(` column [1-9][0-9]*\n$`).MatchString(got.stderr) {
					t.Fatalf("JSON admission diagnostic: %+v, expected %q with position", got, want)
				}
			} else if got.stderr != want+"\n" {
				t.Fatalf("admission diagnostic: %+v, expected %q", got, want)
			}
		}}
		if expected == "" {
			scenario.number = 1
		}
		scenarios = append(scenarios, scenario)
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}

func TestLLVMLocationMetadataDiagnosticSpan(t *testing.T) {
	harness := locationMetadataHarness(t, `
typedef __UINT64_TYPE__ U64;
extern U64 lo_runtime_live_programs(void);
extern U64 lo_runtime_live_values(void);
extern int lo_cli_main(int,const char*const*,const char*const*);
int main(void) {
 const char *args[]={"fixture","run","--format","json",0};
 int status=lo_cli_main(4,args,0);
 if (lo_runtime_live_programs() || lo_runtime_live_values()) return 20;
 return status;
}
`)
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), []llvmRuntimeScenario{{name: "coordinate-extra-fields", source: `return Math.div(1,0)`, harness: harness, transform: func(t *testing.T, module []byte) []byte {
		return rewriteLLVMMetadata(t, module, func(metadata map[string]json.RawMessage) {
			var sites []map[string]any
			if err := json.Unmarshal(metadata["locations"], &sites); err != nil {
				t.Fatal(err)
			}
			for _, site := range sites {
				for _, key := range []string{"start", "end"} {
					site[key].(map[string]any)["extra"] = map[string]any{"nested": []any{1, nil, "retained"}}
				}
			}
			metadata["locations"] = json.RawMessage(locationMetadataJSON(sites))
		})
	}, verify: func(t *testing.T, got result) {
		if got.status != 1 || got.stdout != "" {
			t.Fatalf("unhandled error: %+v", got)
		}
		var envelope struct {
			Diagnostics []struct {
				Code string
				Span struct{ Start, End map[string]json.RawMessage }
			}
		}
		if err := json.Unmarshal([]byte(got.stderr), &envelope); err != nil {
			t.Fatal(err)
		}
		if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "runtime.unhandled-error" {
			t.Fatalf("error diagnostic: %+v", got)
		}
		for _, coordinate := range []map[string]json.RawMessage{envelope.Diagnostics[0].Span.Start, envelope.Diagnostics[0].Span.End} {
			if string(coordinate["extra"]) != `{"nested":[1,null,"retained"]}` {
				t.Fatalf("coordinate metadata lost: %s", got.stderr)
			}
		}
	}}})
}

type locationMetadataCase struct {
	name, message string
	edit          func(map[string]any) []byte
}

func locationMetadataCases() []locationMetadataCase {
	var cases []locationMetadataCase
	add := func(name, message string, edit func(map[string]any)) {
		cases = append(cases, locationMetadataCase{name, message, func(root map[string]any) []byte { edit(root); return locationMetadataJSON(root) }})
	}
	first := func(root map[string]any) map[string]any { return root["locations"].([]any)[0].(map[string]any) }
	coordinate := func(root map[string]any) map[string]any { return first(root)["start"].(map[string]any) }
	add("valid", "", func(map[string]any) {})
	for _, key := range []string{"id", "action", "text"} {
		add("missing-"+key, "invalid metadata "+key, func(root map[string]any) { delete(first(root), key) })
		add("wrongtype-"+key, "invalid metadata "+key, func(root map[string]any) { first(root)[key] = []any{} })
	}
	add("unknown-action", "unknown native action unknown", func(root map[string]any) { first(root)["action"] = "unknown" })
	add("wrongtype-location", "invalid metadata id", func(root map[string]any) { root["locations"].([]any)[0] = 7 })
	add("wrongtype-locations", "absent metadata locations", func(root map[string]any) { root["locations"] = map[string]any{} })
	add("zero-column", "invalid location coordinate", func(root map[string]any) { coordinate(root)["column"] = 0 })
	add("negative-offset", "invalid metadata offset", func(root map[string]any) { coordinate(root)["offset"] = -1 })
	add("coordinate-extra", "", func(root map[string]any) { coordinate(root)["extra"] = map[string]any{"a": []any{1, nil, true}} })
	add("unknown-field", "", func(root map[string]any) { first(root)["extra"] = map[string]any{"a": []any{1, nil, true}} })
	add("id-before-action", "invalid metadata id", func(root map[string]any) { first(root)["id"] = "bad"; first(root)["action"] = false })
	add("action-before-coordinate", "invalid metadata action", func(root map[string]any) { first(root)["action"] = false; coordinate(root)["column"] = 0 })
	add("text-before-coordinate", "invalid metadata text", func(root map[string]any) { first(root)["text"] = []any{}; coordinate(root)["column"] = 0 })
	add("derivative-result-reference", "invalid derivative result node", func(root map[string]any) { root["derivatives"].([]any)[0].(map[string]any)["result_node"] = 999999 })
	raw := func(name, message string, edit func(map[string]any, string) []byte) {
		cases = append(cases, locationMetadataCase{name, message, func(root map[string]any) []byte { return edit(root, string(locationMetadataJSON(first(root)))) }})
	}
	appendField := func(root map[string]any, site, field string) []byte {
		return locationMetadataFirst(root, site[:len(site)-1]+","+field+"}")
	}
	raw("duplicate-action-valid-last", "", func(root map[string]any, site string) []byte {
		return locationMetadataFirst(root, `{"action":7,`+site[1:])
	})
	raw("duplicate-action-invalid-last", "invalid metadata action", func(root map[string]any, site string) []byte { return appendField(root, site, `"action":7`) })
	raw("duplicate-coordinate-valid-last", "", func(root map[string]any, site string) []byte {
		return appendField(root, site, `"start":`+string(locationMetadataJSON(coordinate(root))))
	})
	raw("unknown-huge-number", "program metadata: number out of range", func(root map[string]any, site string) []byte { return appendField(root, site, `"extra":1e999`) })
	raw("unknown-number-beyond-u64", "", func(root map[string]any, site string) []byte {
		return appendField(root, site, `"extra":18446744073709551616`)
	})
	raw("trailing-json", "program metadata: trailing characters", func(root map[string]any, _ string) []byte {
		return append(locationMetadataJSON(root), []byte(" {}")...)
	})
	raw("truncated-json", "program metadata: EOF while parsing an object", func(root map[string]any, _ string) []byte {
		data := locationMetadataJSON(root)
		return data[:len(data)-1]
	})
	raw("wrongtype-root", "program ABI identity mismatch", func(map[string]any, string) []byte { return []byte("[]") })
	raw("escaped-known-key", "", func(root map[string]any, site string) []byte {
		return locationMetadataFirst(root, strings.Replace(site, `"action":`, `"act\u0069on":`, 1))
	})
	raw("escaped-unknown-key", "", func(root map[string]any, site string) []byte { return appendField(root, site, `"unknown\u0061":1`) })
	raw("invalid-surrogate-known-key", "program metadata: unexpected end of hex escape", func(root map[string]any, site string) []byte { return appendField(root, site, `"\ud800":1`) })
	raw("invalid-surrogate-value", "program metadata: unexpected end of hex escape", func(root map[string]any, site string) []byte { return appendField(root, site, `"unknown":"\ud800"`) })
	raw("invalid-utf8-unknown-value", "program metadata: invalid unicode code point", func(root map[string]any, site string) []byte {
		return bytes.Replace(appendField(root, site, `"unknown":"PLACEHOLDER"`), []byte("PLACEHOLDER"), []byte{255}, 1)
	})
	raw("unknown-array-huge-number", "program metadata: number out of range", func(root map[string]any, site string) []byte { return appendField(root, site, `"unknown":[1e999]`) })
	raw("unknown-object-huge-number", "program metadata: number out of range", func(root map[string]any, site string) []byte { return appendField(root, site, `"unknown":{"n":1e999}`) })
	raw("duplicate-root-locations-valid-last", "", func(root map[string]any, _ string) []byte {
		return locationMetadataRoot(root, `"locations":null,"locations":`+string(locationMetadataJSON(root["locations"])))
	})
	raw("duplicate-root-locations-invalid-last", "absent metadata locations", func(root map[string]any, _ string) []byte {
		return locationMetadataRoot(root, `"locations":`+string(locationMetadataJSON(root["locations"]))+`,"locations":null`)
	})
	raw("locations-object-huge-number", "program metadata: number out of range", func(root map[string]any, _ string) []byte {
		return locationMetadataRoot(root, `"locations":{"n":1e999}`)
	})
	raw("location-array-huge-number", "program metadata: number out of range", func(root map[string]any, _ string) []byte { return locationMetadataFirst(root, `[1e999]`) })
	raw("root-array-huge-number", "program metadata: number out of range", func(map[string]any, string) []byte { return []byte("[1e999]") })
	add("locations-missing", "absent metadata locations", func(root map[string]any) { delete(root, "locations") })
	add("id-null", "invalid metadata id", func(root map[string]any) { first(root)["id"] = nil })
	add("id-fraction", "invalid metadata id", func(root map[string]any) { first(root)["id"] = 0.5 })
	raw("unknown-duplicate-last", "", func(root map[string]any, site string) []byte {
		return appendField(root, site, `"unknown":1,"unknown":2`)
	})
	raw("coordinate-unknown-huge-number", "program metadata: number out of range", func(root map[string]any, site string) []byte {
		return locationMetadataFirst(root, strings.Replace(site, `"column":1`, `"unknown":1e999,"column":1`, 1))
	})
	for depth := 124; depth <= 129; depth++ {
		nested := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
		unknownMessage := "program metadata: recursion limit exceeded"
		if depth == 124 {
			unknownMessage = ""
		}
		raw(fmt.Sprintf("unknown-depth-%d", depth), unknownMessage, func(root map[string]any, site string) []byte { return appendField(root, site, `"unknown":`+nested) })
		rootMessage := "program ABI identity mismatch"
		if depth >= 128 {
			rootMessage = "program metadata: recursion limit exceeded"
		}
		raw(fmt.Sprintf("root-array-depth-%d", depth), rootMessage, func(map[string]any, string) []byte { return []byte(nested) })
	}
	return cases
}

func locationMetadataJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
func locationMetadataRoot(root map[string]any, fields string) []byte {
	other := map[string]any{}
	for key, value := range root {
		if key != "locations" {
			other[key] = value
		}
	}
	data := locationMetadataJSON(other)
	return append(append(data[:len(data)-1], ','), []byte(fields+"}")...)
}
func locationMetadataFirst(root map[string]any, first string) []byte {
	var sites []string
	for index, site := range root["locations"].([]any) {
		if index == 0 {
			sites = append(sites, first)
		} else {
			sites = append(sites, string(locationMetadataJSON(site)))
		}
	}
	return locationMetadataRoot(root, `"locations":[`+strings.Join(sites, ",")+"]")
}

var locationMetadataDeclaration = regexp.MustCompile(`(?m)^@lo_program_metadata = constant \[[0-9]+ x i8\] c"((?:\\[0-9A-Fa-f]{2}|[^"\\])*)"`)

func locationMetadataBytes(t *testing.T, module []byte) []byte {
	t.Helper()
	match := locationMetadataDeclaration.FindSubmatch(module)
	if match == nil {
		t.Fatal("missing emitted metadata")
	}
	data, err := decodeLLVMBytes(match[1])
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func replaceLocationMetadata(t *testing.T, module, data []byte) []byte {
	t.Helper()
	match := locationMetadataDeclaration.FindSubmatch(module)
	if match == nil {
		t.Fatal("missing emitted metadata")
	}
	var encoded strings.Builder
	for _, b := range data {
		fmt.Fprintf(&encoded, `\%02X`, b)
	}
	replacement := []byte(fmt.Sprintf(`@lo_program_metadata = constant [%d x i8] c"%s"`, len(data), encoded.String()))
	module = bytes.Replace(module, match[0], replacement, 1)
	return regexp.MustCompile(`@lo_program_metadata_size = constant i64 [0-9]+`).ReplaceAll(module, []byte(fmt.Sprintf("@lo_program_metadata_size = constant i64 %d", len(data))))
}
func locationMetadataHarness(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "metadata.c")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(source); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(repository, path)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatal("metadata harness leaves repository")
	}
	return relative
}
