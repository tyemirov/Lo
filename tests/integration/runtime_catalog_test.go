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
	executablePath := repository + "/testdata/inert-process/executable"
	executableJSON := fmt.Sprintf(`{"type":"Process.Executable","value":{"path":{"type":"File.Path","value":%q}}}`, executablePath)
	want := map[string]string{
		catalog.HostInvocationCurrent:          `{"type":"Int","value":"0"}`,
		catalog.FileDirectory:                  `{"type":"Bool","value":true}`,
		catalog.FileScan:                       `{"type":"Int","value":"0"}`,
		catalog.FileRemoveChecked:              `{"type":"Int","value":"0"}`,
		catalog.ProcessContextCurrent:          `{"type":"Process.Context","value":{"origin":"inherited","redacted":true}}`,
		catalog.ProcessContextNew:              `{"type":"Process.Context","value":{"origin":"explicit","redacted":true}}`,
		catalog.ProcessContextOverride:         `{"type":"Process.Context","value":{"origin":"derived","redacted":true}}`,
		catalog.ProcessContextWithout:          `{"type":"Process.Context","value":{"origin":"derived","redacted":true}}`,
		catalog.ProcessContextDirectory:        `{"type":"Process.Context","value":{"origin":"derived","redacted":true}}`,
		catalog.ProcessContextEnvironmentValue: `{"type":"String","value":"value"}`,
		catalog.ProcessExecutableResolve:       fmt.Sprintf(`{"type":"Process.Executable","value":{"path":{"type":"File.Path","value":%q}}}`, binary),
		catalog.ProcessExecutableNew:           executableJSON,
		catalog.ProcessTerminationExitCode:     `"0"`,
		catalog.ProcessTerminationSignal:       `"0"`,
		catalog.ProcessCommandNew:              fmt.Sprintf(`{"type":"Process.Command","value":{"context":{"type":"Process.Context","value":{"origin":"explicit","redacted":true}},"executable":%s,"arguments":{"type":"List<String>","value":[{"type":"String","value":"argument"}]},"stdin":{"type":"Bytes","value":"c3RkaW4="}}}`, executableJSON),
		catalog.AutodiffGrad:                   `{"type":"Record{gradients: Record{x: Tensor<FP32>}, value: Tensor<FP32>}","value":{"gradients":{"type":"Record{x: Tensor<FP32>}","value":{"x":{"type":"Tensor<FP32>","value":{"dtype":"fp32","device":"cpu","shape":[],"values":["4"]}}}},"value":{"type":"Tensor<FP32>","value":{"dtype":"fp32","device":"cpu","shape":[],"values":["4"]}}}}`,
		catalog.FloatAdd:                       `{"type":"Float","value":"3.75"}`, catalog.FloatSub: `{"type":"Float","value":"-1.25"}`,
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
		catalog.URLNew:           `{"type":"HTTP.URL","value":"http://127.0.0.1:80/resource"}`,
		catalog.MethodNew:        `{"type":"HTTP.Method","value":"GET"}`,
		catalog.ClientRequestNew: `{"type":"HTTP.Client.Request","value":{"method":{"type":"HTTP.Method","value":"GET"},"url":{"type":"HTTP.URL","value":"http://127.0.0.1:80/resource"},"headers":{"type":"HTTP.Headers","value":{"entries":{"type":"List<Record{name: String, value: Bytes}>","value":[]}}},"body":{"type":"Bytes","value":""}}}`,
		catalog.HeadersNew:       `{"type":"HTTP.Headers","value":{"entries":{"type":"List<Record{name: String, value: Bytes}>","value":[]}}}`,
		catalog.HeaderValues:     `{"type":"List<Bytes>","value":[]}`,
		catalog.ResponseNew:      `{"type":"HTTP.Response","value":{"status":{"type":"Int","value":"200"},"headers":{"type":"HTTP.Headers","value":{"entries":{"type":"List<Record{name: String, value: Bytes}>","value":[]}}},"body":{"type":"Bytes","value":"b2s="}}}`,
		catalog.ResponseTextNew:  `{"status":{"type":"Int","value":"200"},"headers":{"type":"HTTP.Headers","value":{"entries":{"type":"List<Record{name: String, value: Bytes}>","value":[]}}},"body":{"type":"Bytes","value":"b2s="}}`,
		catalog.ResponseBytesNew: `{"status":{"type":"Int","value":"200"},"headers":{"type":"HTTP.Headers","value":{"entries":{"type":"List<Record{name: String, value: Bytes}>","value":[]}}},"body":{"type":"Bytes","value":"b2s="}}`,
		catalog.UTF8BytesNew:     `"b2s="`,
		catalog.UTF8New:          `{"type":"String","value":"ok"}`, catalog.FromScalarsNew: `"a"`, catalog.Concat: `"ab"`, catalog.Upper: `"A"`, catalog.Lower: `"a"`, catalog.StringLength: `"1"`, catalog.StringEqual: `false`, catalog.ScalarEqual: `false`, catalog.IntToString: `"1"`,
		catalog.MathAdd: `"3"`, catalog.MathSub: `"-1"`, catalog.MathMul: `"2"`, catalog.MathDiv: catalog.DivideByZero, catalog.MathMod: catalog.ModuloByZero,
		catalog.MathLT: `true`, catalog.MathLTE: `true`, catalog.MathGT: `false`, catalog.MathGTE: `false`, catalog.MathEqual: `false`, catalog.MathSum: `"3"`, catalog.MathProduct: `"1"`, catalog.MathMin: catalog.EmptyList, catalog.MathMax: catalog.EmptyList,
		catalog.Contains: `true`, catalog.IsEmpty: `false`, catalog.HasKey: `true`, catalog.Get: `{"type":"Int","value":"1"}`,
		catalog.Merge:   `{"a":{"type":"String","value":"a"}}`,
		catalog.ListMap: `[{"type":"String","value":"1"}]`, catalog.ListFilter: `[{"type":"Scalar","value":"a"},{"type":"Scalar","value":"b"},{"type":"Scalar","value":"c"}]`, catalog.ListFold: `"1"`,
		catalog.ListFlatMap: `{"type":"List<Int>","value":[{"type":"Int","value":"1"}]}`,
		catalog.ListUnfold:  `{"type":"List<String>","value":[{"type":"String","value":"0"},{"type":"String","value":"1"}]}`, catalog.Length: `"5"`, catalog.FileRead: `{"type":"Bytes","value":"eA=="}`,
		catalog.FileAppend:    `{"operation":"File.Append.new","file_path":{"type":"File.Path","value":"unwritten"},"file_content":{"type":"Bytes","value":"ZGF0YQ=="}}`,
		catalog.FileWrite:     `{"operation":"File.Write.new","file_path":{"type":"File.Path","value":"unwritten"},"file_content":{"type":"Bytes","value":"ZGF0YQ=="}}`,
		catalog.FilePathEqual: `true`, catalog.FlowFold: `"1"`, catalog.FlowWorklist: `"1"`,
		catalog.FileList:      `{"type":"List<File.Entry>","value":[]}`,
		catalog.FileOpenPaths: `{"type":"List<File.Path>","value":[]}`,
		catalog.FileRemove:    `{"type":"File.Remove","value":{"operation":"File.Remove.new","entries":{"type":"List<File.Entry>","value":[]}}}`,
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
	for name, element := range map[string]string{catalog.TensorTo: "4", catalog.TensorNew: "4", catalog.TensorAdd: "6", catalog.TensorSub: "2", catalog.TensorMul: "8", catalog.TensorDiv: "2", catalog.TensorSqrt: "2", catalog.TensorRelu: "4", catalog.TensorTranspose: "4", catalog.TensorSum: "4", catalog.TensorMean: "4", catalog.TensorMatmul: "8", catalog.TensorBroadcast: "4", catalog.TensorReshape: "4"} {
		shape := "[]"
		if name == catalog.TensorMatmul {
			shape = `["1","1"]`
		}
		if name == catalog.TensorBroadcast || name == catalog.TensorReshape {
			shape = `["1"]`
		}
		want[name] = fmt.Sprintf(`{"type":"Tensor<FP32>","value":{"dtype":"fp32","device":"cpu","shape":%s,"values":[%q]}}`, shape, element)
	}

	for _, tc := range []struct{ name, typ, element string }{
		{catalog.ParametersNew, "Parameters<Record{x: Tensor<FP32>}>", "2"},
		{catalog.ParametersValues, "Record{x: Tensor<FP32>}", "2"},
		{catalog.GradientsNew, "Gradients<Record{x: Tensor<FP32>}>", "2"},
		{catalog.GradientsValues, "Record{x: Tensor<FP32>}", "2"},
		{catalog.GradientsZero, "Gradients<Record{x: Tensor<FP32>}>", "0"},
		{catalog.GradientsAdd, "Gradients<Record{x: Tensor<FP32>}>", "4"},
		{catalog.GradientsScale, "Gradients<Record{x: Tensor<FP32>}>", "1"},
	} {
		want[tc.name] = fmt.Sprintf(`{"type":%q,"value":{"x":{"type":"Tensor<FP32>","value":{"dtype":"fp32","device":"cpu","shape":[],"values":[%q]}}}}`, tc.typ, tc.element)
	}
	want[catalog.AutodiffStop] = `{"type":"Tensor<FP32>","value":{"dtype":"fp32","device":"cpu","shape":[],"values":["2"]}}`
	for name, expected := range optimizerCatalogResults() {
		want[name] = expected
	}
	for name, expected := range checkpointCatalogResults() {
		want[name] = expected
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
			if tc.name == catalog.FileDirectory || tc.name == catalog.FileScan {
				root := fileFixtureRoot(t)
				if tc.name == catalog.FileDirectory {
					tc.source = fmt.Sprintf(`flow Main() -> Bool! { directory = File.directory(File.Path.new(%q)); return File.Path.eq(directory.path, File.Path.new(%q)) }; return Main()`, root, root)
				} else {
					tc.source = fmt.Sprintf(`flow Main() -> Int! { scan = File.scan(File.Path.new(%q)); return Math.add(List.length(scan.entries), List.length(scan.vanished_names)) }; return Main()`, root)
				}
				args = []string{"--list-root", root}
			}
			if tc.name == catalog.FileList || tc.name == catalog.FileOpenPaths {
				root := fileFixtureRoot(t)
				flag := "--list-root"
				if tc.name == catalog.FileOpenPaths {
					installFileProbeFixture(t)
					flag = "--open-files-root"
				}
				tc.source = fmt.Sprintf("return %s(File.Path.new(%q))", tc.name, root)
				args = []string{flag, root}
			}
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

func optimizerCatalogResults() map[string]string {
	tensor := func(n string) string {
		return fmt.Sprintf(`{"type":"Tensor<FP32>","value":{"dtype":"fp32","device":"cpu","shape":[],"values":[%q]}}`, n)
	}
	record := func(n string) string { return `{"type":"Record{x: Tensor<FP32>}","value":{"x":` + tensor(n) + `}}` }
	parameters := func(n string) string {
		return `{"type":"Parameters<Record{x: Tensor<FP32>}>","value":{"x":` + tensor(n) + `}}`
	}
	configs := map[string]string{
		"SGD":   `{"type":"Optimizer.SGD.Config","value":{"learning_rate":{"type":"Float","value":"0.25"}}}`,
		"AdamW": `{"type":"Optimizer.AdamW.Config","value":{"learning_rate":{"type":"Float","value":"0.25"},"beta1":{"type":"Float","value":"0.5"},"beta2":{"type":"Float","value":"0.5"},"epsilon":{"type":"Float","value":"0.5"},"weight_decay":{"type":"Float","value":"0.5"}}}`,
	}
	out := map[string]string{}
	for _, optimizer := range []string{"SGD", "AdamW"} {
		out["Optimizer."+optimizer+".Config.new"] = configs[optimizer]
		for step := 0; step <= 1; step++ {
			n := "2"
			op := "State.new"
			if step == 1 {
				op = "step"
				n = "1.5"
				if optimizer == "AdamW" {
					n = "1.55"
				}
			}
			fields := fmt.Sprintf(`"parameters":%s,"step":{"type":"Int","value":%q},"config":%s`, parameters(n), fmt.Sprint(step), configs[optimizer])
			if optimizer == "AdamW" {
				m, v, power := "0", "0", "1"
				if step == 1 {
					m, v, power = "1", "2", "0.5"
				}
				fields += fmt.Sprintf(`,"first_moment":%s,"second_moment":%s,"beta1_power":%s,"beta2_power":%s`, record(m), record(v), tensor(power), tensor(power))
			}
			out["Optimizer."+optimizer+"."+op] = fmt.Sprintf(`{"type":%q,"value":{%s}}`, "Optimizer."+optimizer+".State<Record{x: Tensor<FP32>}>", fields)
		}
	}
	return out
}
