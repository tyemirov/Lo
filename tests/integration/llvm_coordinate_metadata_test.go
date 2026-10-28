package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLLVMCoordinateMetadataAdmission(t *testing.T) {
	runLocationMetadataAdmission(t, coordinateMetadataCases())
}
func coordinateMetadataCases() []locationMetadataCase {
	var cases []locationMetadataCase
	first := func(root map[string]any) map[string]any { return root["spans"].([]any)[0].(map[string]any) }
	add := func(name, message string, edit func(map[string]any)) {
		cases = append(cases, locationMetadataCase{name, message, func(root map[string]any) []byte { edit(first(root)); return locationMetadataJSON(root) }})
	}
	raw := func(name, message string, edit func(map[string]any, map[string]any) []byte) {
		cases = append(cases, locationMetadataCase{name, message, func(root map[string]any) []byte { return edit(root, first(root)) }})
	}
	appendCoordinate := func(root, site map[string]any, key, value string) []byte {
		data := string(locationMetadataJSON(site))
		return locationMetadataFirstSpan(root, data[:len(data)-1]+fmt.Sprintf(",%q:%s}", key, value))
	}
	for _, key := range []string{"start", "end"} {
		add("missing-"+key, "invalid metadata offset", func(site map[string]any) { delete(site, key) })
		for _, shape := range []struct {
			name  string
			value any
		}{{"null", nil}, {"bool", false}, {"number", 1}, {"string", "bad"}, {"array", []any{1, 2}}, {"empty-object", map[string]any{}}} {
			add(key+"-"+shape.name, "invalid metadata offset", func(site map[string]any) { site[key] = shape.value })
		}
		for _, field := range []string{"offset", "line", "column"} {
			add(key+"-missing-"+field, "invalid metadata "+field, func(site map[string]any) { delete(site[key].(map[string]any), field) })
			for _, shape := range []struct {
				name  string
				value any
			}{{"null", nil}, {"string", "1"}, {"negative", -1}, {"fraction", 0.5}, {"array", []any{}}, {"out-of-range", json.RawMessage(`18446744073709551616`)}} {
				add(key+"-"+field+"-"+shape.name, "invalid metadata "+field, func(site map[string]any) { site[key].(map[string]any)[field] = shape.value })
			}
			if field != "offset" {
				add(key+"-zero-"+field, "invalid location coordinate", func(site map[string]any) { site[key].(map[string]any)[field] = 0 })
			}
		}
		raw(key+"-duplicate-valid-last", "", func(root, site map[string]any) []byte {
			data := string(locationMetadataJSON(site))
			return locationMetadataFirstSpan(root, fmt.Sprintf("{%q:null,", key)+data[1:])
		})
		raw(key+"-duplicate-invalid-last", "invalid metadata offset", func(root, site map[string]any) []byte { return appendCoordinate(root, site, key, "null") })
		raw(key+"-escaped-key", "", func(root, site map[string]any) []byte {
			data := string(locationMetadataJSON(site))
			spelling := `st\u0061rt`
			if key == "end" {
				spelling = `\u0065nd`
			}
			return locationMetadataFirstSpan(root, strings.Replace(data, fmt.Sprintf("%q:", key), `"`+spelling+`":`, 1))
		})
		for _, field := range []string{"offset", "line", "column"} {
			raw(key+"-"+field+"-duplicate-valid-last", "", func(root, site map[string]any) []byte {
				coordinate := string(locationMetadataJSON(site[key]))
				return appendCoordinate(root, site, key, fmt.Sprintf("{%q:null,", field)+coordinate[1:])
			})
			raw(key+"-"+field+"-duplicate-invalid-last", "invalid metadata "+field, func(root, site map[string]any) []byte {
				coordinate := string(locationMetadataJSON(site[key]))
				return appendCoordinate(root, site, key, coordinate[:len(coordinate)-1]+fmt.Sprintf(",%q:null}", field))
			})
		}
		raw(key+"-escaped-known-duplicate", "", func(root, site map[string]any) []byte {
			coordinate := string(locationMetadataJSON(site[key]))
			coordinate = strings.Replace(coordinate, `"column":`, `"col\u0075mn":`, 1)
			return appendCoordinate(root, site, key, `{"column":null,`+coordinate[1:])
		})
		for _, value := range []string{`1e999`, `[1e999]`, `{"bad":1e999}`} {
			raw(key+"-invalid-extra-number-"+value, "program metadata: number out of range", func(root, site map[string]any) []byte {
				coordinate := string(locationMetadataJSON(site[key]))
				return appendCoordinate(root, site, key, coordinate[:len(coordinate)-1]+`,"extra":`+value+"}")
			})
		}
	}
	add("reversed-offsets", "invalid source span", func(site map[string]any) { site["start"].(map[string]any)["offset"] = 1000000 })
	add("reversed-lines", "invalid source span", func(site map[string]any) { site["start"].(map[string]any)["line"] = 1000000 })
	add("reversed-columns-accepted", "", func(site map[string]any) { site["start"].(map[string]any)["column"] = 1000000 })
	add("all-usize-max-accepted", "", func(site map[string]any) {
		for _, key := range []string{"start", "end"} {
			for _, field := range []string{"offset", "line", "column"} {
				site[key].(map[string]any)[field] = json.RawMessage(`18446744073709551615`)
			}
		}
	})
	add("offset-before-line", "invalid metadata offset", func(site map[string]any) {
		site["start"].(map[string]any)["offset"] = "bad"
		site["start"].(map[string]any)["line"] = 0
	})
	add("line-before-column", "invalid location coordinate", func(site map[string]any) {
		site["start"].(map[string]any)["line"] = 0
		site["start"].(map[string]any)["column"] = nil
	})
	add("start-before-end", "invalid metadata column", func(site map[string]any) {
		site["start"].(map[string]any)["column"] = nil
		site["end"].(map[string]any)["offset"] = "bad"
	})
	add("end-field-before-span", "invalid metadata line", func(site map[string]any) {
		site["start"].(map[string]any)["offset"] = 1000000
		site["end"].(map[string]any)["line"] = "bad"
	})
	raw("json-before-semantic-error", "program metadata: number out of range", func(root, site map[string]any) []byte {
		site["file_index"] = "bad"
		coordinate := string(locationMetadataJSON(site["end"]))
		return appendCoordinate(root, site, "end", coordinate[:len(coordinate)-1]+`,"unknown":1e999}`)
	})
	raw("unknown-null-array-large-number", "", func(root, site map[string]any) []byte {
		coordinate := string(locationMetadataJSON(site["start"]))
		return appendCoordinate(root, site, "start", coordinate[:len(coordinate)-1]+`,"a_before":null,"d_between":[null,{"κ":"é界"}],"m_between":18446744073709551615,"z_after":{"nested":[true,false,1]}}`)
	})
	raw("unknown-duplicate-last", "", func(root, site map[string]any) []byte {
		coordinate := string(locationMetadataJSON(site["end"]))
		return appendCoordinate(root, site, "end", coordinate[:len(coordinate)-1]+`,"extra":false,"extr\u0061":null}`)
	})
	raw("coordinate-invalid-surrogate-key", "program metadata: unexpected end of hex escape", func(root, site map[string]any) []byte {
		coordinate := string(locationMetadataJSON(site["start"]))
		return appendCoordinate(root, site, "start", coordinate[:len(coordinate)-1]+`,"\ud800":1}`)
	})
	raw("coordinate-invalid-surrogate-value", "program metadata: unexpected end of hex escape", func(root, site map[string]any) []byte {
		coordinate := string(locationMetadataJSON(site["start"]))
		return appendCoordinate(root, site, "start", coordinate[:len(coordinate)-1]+`,"unknown":"\ud800"}`)
	})
	raw("coordinate-invalid-utf8", "program metadata: invalid unicode code point", func(root, site map[string]any) []byte {
		coordinate := string(locationMetadataJSON(site["start"]))
		return bytes.Replace(appendCoordinate(root, site, "start", coordinate[:len(coordinate)-1]+`,"unknown":"PLACEHOLDER"}`), []byte("PLACEHOLDER"), []byte{255}, 1)
	})
	raw("duplicate-coordinate-earlier-json-error", "program metadata: number out of range", func(root, site map[string]any) []byte {
		data := string(locationMetadataJSON(site))
		return locationMetadataFirstSpan(root, `{"start":{"extra":1e999},`+data[1:])
	})
	for depth := 122; depth <= 129; depth++ {
		nested := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
		message := ""
		if depth >= 124 {
			message = "program metadata: recursion limit exceeded"
		}
		raw(fmt.Sprintf("coordinate-extra-depth-%d", depth), message, func(root, site map[string]any) []byte {
			coordinate := string(locationMetadataJSON(site["start"]))
			return appendCoordinate(root, site, "start", coordinate[:len(coordinate)-1]+`,"extra":`+nested+"}")
		})
		message = "invalid metadata offset"
		if depth >= 125 {
			message = "program metadata: recursion limit exceeded"
		}
		raw(fmt.Sprintf("coordinate-array-depth-%d", depth), message, func(root, site map[string]any) []byte { return appendCoordinate(root, site, "end", nested) })
	}
	return cases
}

func coordinateMetadataExtras(t *testing.T, module []byte) []byte {
	t.Helper()
	return rewriteLLVMMetadata(t, module, func(metadata map[string]json.RawMessage) {
		var sites []map[string]json.RawMessage
		if err := json.Unmarshal(metadata["spans"], &sites); err != nil {
			t.Fatal(err)
		}
		for _, site := range sites {
			for _, key := range []string{"start", "end"} {
				var coordinate map[string]json.RawMessage
				if err := json.Unmarshal(site[key], &coordinate); err != nil {
					t.Fatal(err)
				}
				coordinate["a_before"] = json.RawMessage(`null`)
				coordinate["d_between"] = json.RawMessage(`[null,{"κ":"é界"}]`)
				coordinate["m_between"] = json.RawMessage(`18446744073709551615`)
				coordinate["z_after"] = json.RawMessage(`{"nested":[true,false,1]}`)
				encoded := string(locationMetadataJSON(coordinate))
				site[key] = json.RawMessage(`{"offset":null,"line":"bad","column":0,` + encoded[1:len(encoded)-1] + `,"a_before":true,"a_b\u0065fore":null}`)
			}
		}
		metadata["spans"] = json.RawMessage(locationMetadataJSON(sites))
	})
}
func verifyCoordinateSpan(t *testing.T, span json.RawMessage) {
	t.Helper()
	var parts struct{ Start, End json.RawMessage }
	if err := json.Unmarshal(span, &parts); err != nil {
		t.Fatal(err)
	}
	for _, coordinate := range []json.RawMessage{parts.Start, parts.End} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(coordinate, &fields); err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]string{"a_before": `null`, "d_between": `[null,{"κ":"é界"}]`, "m_between": `18446744073709551615`, "z_after": `{"nested":[true,false,1]}`} {
			if string(fields[key]) != want {
				t.Fatalf("coordinate %s lost: %s", key, coordinate)
			}
		}
		previous := -1
		for _, key := range []string{"offset", "line", "column", "a_before", "d_between", "m_between", "z_after"} {
			index := strings.Index(string(coordinate), fmt.Sprintf("%q:", key))
			if index <= previous {
				t.Fatalf("coordinate key order: %s", coordinate)
			}
			previous = index
		}
	}
}
func TestLLVMCoordinateDiagnosticSpan(t *testing.T) {
	harness := locationMetadataHarness(t, `extern int lo_cli_main(int,const char*const*,const char*const*);int main(void){const char*args[]={"coordinate","run","--format","json",0};return lo_cli_main(4,args,0);}`)
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), []llvmRuntimeScenario{{name: "ordered-arbitrary-coordinate-extras", source: `return Math.div(1,0)`, harness: harness, transform: coordinateMetadataExtras, verify: func(t *testing.T, got result) {
		if got.status != 1 || got.stdout != "" {
			t.Fatalf("diagnostic: %+v", got)
		}
		var report struct {
			Diagnostics []struct{ Span json.RawMessage }
		}
		if err := json.Unmarshal([]byte(got.stderr), &report); err != nil {
			t.Fatal(err)
		}
		if len(report.Diagnostics) != 1 {
			t.Fatal(got.stderr)
		}
		verifyCoordinateSpan(t, report.Diagnostics[0].Span)
	}}})
}
func TestLLVMCoordinateArtifactMetadata(t *testing.T) {
	harness := locationMetadataHarness(t, `extern int lo_cli_main(int,const char*const*,const char*const*);int main(void){const char*args[]={"coordinate","metadata",0};return lo_cli_main(2,args,0);}`)
	var expected string
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), []llvmRuntimeScenario{{name: "metadata-unchanged", source: `return 7`, harness: harness, transform: func(t *testing.T, module []byte) []byte {
		var metadata map[string]json.RawMessage
		if err := json.Unmarshal(locationMetadataBytes(t, module), &metadata); err != nil {
			t.Fatal(err)
		}
		var artifact any
		if err := json.Unmarshal(metadata["artifact"], &artifact); err != nil {
			t.Fatal(err)
		}
		expected = string(locationMetadataJSON(artifact)) + "\n"
		return coordinateMetadataExtras(t, module)
	}, verify: func(t *testing.T, got result) {
		if got.status != 0 || got.stderr != "" || got.stdout != expected {
			t.Fatalf("artifact metadata: %+v expected %s", got, expected)
		}
	}}})
}

func TestLLVMCoordinateReceiptSpans(t *testing.T) {
	var scenarios []llvmRuntimeScenario
	add := func(name, source, operation string, number int, arguments []string) {
		var constants []string
		for _, argument := range append([]string{"coordinate", "run", "--format", "json"}, arguments...) {
			constants = append(constants, fmt.Sprintf("%q", argument))
		}
		harness := locationMetadataHarness(t, fmt.Sprintf("#include %q\nconst char *lo_coordinate_arguments[]={%s};\nconst int lo_coordinate_argument_count=%d;\n", filepath.Join(repository, "testdata/native-qualification/coordinate-receipts.c"), strings.Join(constants, ","), len(constants)))
		scenarios = append(scenarios, llvmRuntimeScenario{name: name, source: source, number: number, harness: harness, transform: coordinateMetadataExtras, verify: func(t *testing.T, got result) {
			if got.status != 1 || got.stdout != "" {
				t.Fatalf("receipt %s: %+v", operation, got)
			}
			var report struct {
				Diagnostics []struct {
					Completed struct {
						Operation string
						Span      json.RawMessage
					} `json:"last_completed_activation"`
				}
			}
			if err := json.Unmarshal([]byte(got.stderr), &report); err != nil {
				t.Fatal(err)
			}
			if len(report.Diagnostics) != 1 || report.Diagnostics[0].Completed.Operation != operation {
				t.Fatalf("receipt missing: %+v", got)
			}
			verifyCoordinateSpan(t, report.Diagnostics[0].Completed.Span)
		}})
	}
	add("file-write", `flow Main()->Int!{target=File.Write.new(File.Path.new("coordinate-written"),Bytes.new("abc"));commit target;return Math.div(1,0)};return Main()`, "File.write", 0, []string{"--write-root", "."})
	add("file-remove", `flow Keep(entry:File.Entry)->Bool{return String.eq(entry.name,"coordinate-entry")};flow Main()->Int!{write=File.Write.new(File.Path.new("coordinate-entry"),Bytes.new("abc"));commit write;entries=File.list(File.Path.new("."));selected=List.filter(entries,Keep);target=File.Remove.new(selected);commit target;return Math.div(1,0)};return Main()`, "File.remove", 1, []string{"--write-root", ".", "--list-root", ".", "--remove-root", "."})
	add("http-listen", `flow H(r:HTTP.Request)->HTTP.Response{return HTTP.Response.Text.new(200,"ok")};flow E(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)};flow Main()->Int!{target=HTTP.Server.new(0,H,E);commit target;return Math.div(1,0)};return Main()`, "HTTP.listen", 2, []string{"--listen", "0"})
	// Listen activation requires serve; the fixture changes this command only for case 2.
	add("http-request", `flow Main()->Int!{url=HTTP.URL.new("http://127.0.0.1:34510/");target=HTTP.Client.Request.new(HTTP.Method.new("GET"),url,HTTP.Headers.new(List.new()),Bytes.new(""));response=commit target;return Math.div(1,0)};return Main()`, "HTTP.request", 3, []string{"--http-allow", "GET,http://127.0.0.1:34510,127.0.0.1"})
	if runtime.GOOS == "darwin" && semanticNativeTarget() == "darwin/arm64" {
		executable := filepath.Join(repository, "bin/lo-process-launcher")
		data, err := os.ReadFile(executable)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		add("process", fmt.Sprintf(`flow Main()->Int!{executable=Process.Executable.new(File.Path.new(%q));target=Process.Command.new(executable: executable, arguments: List.new("coordinate-child"), context: Process.Context.new(Map.new(), File.Path.new(%q)), stdin: Bytes.new(""));result=commit target;return Math.div(1,0)};return Main()`, executable, repository), "Process.run", 4, []string{"--process-allow", fmt.Sprintf("%x,%s", digest, executable), "--process-read-root", repository})
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}
