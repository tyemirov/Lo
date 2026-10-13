package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
)

func TestRunBuiltinCatalog(t *testing.T) {
	want := map[string]string{
		catalog.FloatAdd: `{"type":"Float","value":"3.75"}`, catalog.FloatSub: `{"type":"Float","value":"-1.25"}`,
		catalog.FloatMul: `{"type":"Float","value":"2.5"}`, catalog.FloatDiv: `{"type":"Float","value":"1.5"}`,
		catalog.FloatSqrt: `{"type":"Float","value":"1.5"}`, catalog.IntToFloat: `{"type":"Float","value":"7"}`,
		catalog.FloatEqual: `true`, catalog.FloatLT: `true`, catalog.FloatLTE: `true`, catalog.FloatGT: `false`, catalog.FloatGTE: `true`,
		catalog.FloatToInt: `"-1"`, catalog.FloatToString: `"-0"`,
		catalog.IntNew: `"123456789012345678901234567890"`, catalog.StringNew: `"héllo"`, catalog.BoolNew: `false`, catalog.FloatNew: `"-1250"`, catalog.BytesNew: `"Ynl0ZXM="`, catalog.ScalarNew: `"界"`,
		catalog.ListNew:   `[{"type":"Null","value":null},{"type":"Null","value":null}]`,
		catalog.MapNew:    `[{"key":{"type":"Bool","value":true},"value":{"type":"Int","value":"1"}}]`,
		catalog.RecordNew: `{"x":{"type":"Int","value":"1"},"y":{"type":"String","value":"a"}}`,
		catalog.RangeNew:  `{"start":{"type":"String","value":"a"},"end":{"type":"String","value":"z"},"include_start":true,"include_end":false}`,
		catalog.ErrorNew:  "", catalog.PathNew: `"unread"`, catalog.ServerNew: "",
		catalog.ResponseTextNew:  `{"status":{"type":"Int","value":"200"},"headers":{"type":"Map<String, String>","value":[]},"body":{"type":"Bytes","value":"b2s="}}`,
		catalog.ResponseBytesNew: `{"status":{"type":"Int","value":"200"},"headers":{"type":"Map<String, String>","value":[]},"body":{"type":"Bytes","value":"b2s="}}`,
		catalog.UTF8New:          `{"type":"String","value":"ok"}`, catalog.FromScalarsNew: `"a"`, catalog.Concat: `"ab"`, catalog.Upper: `"A"`, catalog.Lower: `"a"`, catalog.StringLength: `"1"`, catalog.StringEqual: `false`, catalog.ScalarEqual: `false`, catalog.IntToString: `"1"`,
		catalog.MathAdd: `"3"`, catalog.MathSub: `"-1"`, catalog.MathMul: `"2"`, catalog.MathDiv: catalog.DivideByZero, catalog.MathMod: catalog.ModuloByZero,
		catalog.MathLT: `true`, catalog.MathLTE: `true`, catalog.MathGT: `false`, catalog.MathGTE: `false`, catalog.MathEqual: `false`, catalog.MathSum: `"3"`, catalog.MathProduct: `"1"`, catalog.MathMin: catalog.EmptyList, catalog.MathMax: catalog.EmptyList,
		catalog.Contains: `true`, catalog.IsEmpty: `false`, catalog.HasKey: `true`, catalog.Get: `{"type":"Int","value":"1"}`,
		catalog.Merge:   `{"a":{"type":"String","value":"a"}}`,
		catalog.ListMap: `[{"type":"String","value":"1"}]`, catalog.ListFilter: `[{"type":"Scalar","value":"a"},{"type":"Scalar","value":"b"},{"type":"Scalar","value":"c"}]`, catalog.ListFold: `"1"`,
		catalog.ListFlatMap: `{"type":"List<Int>","value":[{"type":"Int","value":"1"}]}`,
		catalog.ListUnfold:  `{"type":"List<String>","value":[{"type":"String","value":"0"},{"type":"String","value":"1"}]}`, catalog.Length: `"5"`, catalog.FileRead: `{"type":"Bytes","value":"eA=="}`,
		catalog.FileAppend: `{"operation":"File.append","file_path":{"type":"File.Path","value":"unwritten"},"file_content":{"type":"String","value":"data"}}`,
		catalog.FileWrite:  `{"operation":"File.write","file_path":{"type":"File.Path","value":"unwritten"},"file_content":{"type":"Bytes","value":"ZGF0YQ=="}}`,
	}
	for name, expected := range map[string]string{
		catalog.DeviceNew:     `{"type":"Device","value":"cpu"}`,
		catalog.TensorShape:   `{"type":"List<Int>","value":[]}`,
		catalog.TensorRank:    `{"type":"Int","value":"0"}`,
		catalog.TensorDtype:   `{"type":"String","value":"fp32"}`,
		catalog.TensorDevice:  `{"type":"Device","value":"cpu"}`,
		catalog.TensorToFloat: `{"type":"Float","value":"4"}`,
		catalog.TensorValues:  `{"type":"List<Float>","value":[{"type":"Float","value":"4"}]}`,
	} {
		want[name] = expected
	}
	for name, element := range map[string]string{catalog.TensorNew: "4", catalog.TensorAdd: "6", catalog.TensorSub: "2", catalog.TensorMul: "8", catalog.TensorDiv: "2", catalog.TensorSqrt: "2", catalog.TensorRelu: "4", catalog.TensorTranspose: "4", catalog.TensorSum: "4", catalog.TensorMean: "4", catalog.TensorMatmul: "8", catalog.TensorBroadcast: "4", catalog.TensorReshape: "4"} {
		shape := "[]"
		if name == catalog.TensorMatmul {
			shape = `["1","1"]`
		}
		if name == catalog.TensorBroadcast || name == catalog.TensorReshape {
			shape = `["1"]`
		}
		want[name] = fmt.Sprintf(`{"type":"Tensor<FP32>","value":{"dtype":"fp32","device":"cpu","shape":%s,"values":[%q]}}`, shape, element)
	}

	covered := map[string]bool{}
	for _, tc := range builtinExamples() {
		if _, ok := want[tc.name]; ok {
			covered[tc.name] = true
		}
		t.Run(tc.name, func(t *testing.T) {
			expected, ok := want[tc.name]
			if !ok {
				t.Fatalf("missing runtime assertion for %s", tc.name)
			}
			if tc.name == catalog.MathDiv || tc.name == catalog.MathMod || tc.name == catalog.MathMin || tc.name == catalog.MathMax {
				got := run(t, "run", sourceFile(t, tc.source), "--format", "json")
				var out struct {
					Diagnostics []struct{ Error runtimeValue }
				}
				if err := json.Unmarshal([]byte(got.stderr), &out); err != nil {
					t.Fatal(err)
				}
				if got.status != 1 || len(out.Diagnostics) != 1 || out.Diagnostics[0].Error.Kind != expected {
					t.Fatalf("builtin error: %+v", got)
				}
				return
			}
			var args []string
			if tc.name == catalog.FileRead {
				dir := t.TempDir()
				file, err := os.Create(filepath.Join(dir, "data"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.WriteString("x"); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				tc.source = fmt.Sprintf(`return File.read(File.Path.new(%q))`, file.Name())
				args = []string{"--read-root", dir}
			}
			v := runValue(t, tc.source, args...)
			if v.Type != tc.kind {
				t.Fatalf("type %s, want %s", v.Type, tc.kind)
			}
			switch tc.name {
			case catalog.ErrorNew:
				if v.Kind != "Custom" || v.Message != "message" || v.Input == nil || len(v.Trace) != 0 {
					t.Fatalf("Error.new: %+v", v)
				}
				valueJSON(t, *v.Input, `{}`)
			case catalog.ServerNew:
				var server struct {
					Port             runtimeValue
					Handler, OnError string
				}
				if err := json.Unmarshal(v.Value, &server); err != nil {
					t.Fatal(err)
				}
				valueJSON(t, server.Port, `"8080"`)
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(v.Value, &fields); err != nil {
					t.Fatal(err)
				}
				if len(fields["handler"]) == 0 || len(fields["on_error"]) == 0 {
					t.Fatal("absent server handlers")
				}
			default:
				valueJSON(t, v, expected)
			}
		})
	}
	for _, op := range catalog.All() {
		if !covered[op.Name()] {
			t.Errorf("missing runtime case for %s", op.Name())
		}
	}
}
