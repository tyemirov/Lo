package integration_test

import (
	"fmt"
	"testing"
)

func TestF031LocalEndpointMetadata(t *testing.T) {
	for _, fixture := range []struct{ name, document, want string }{
		{"local", `[{"Endpoints":{"docker":{"Host":"unix:///fixture/socket"}},"Unused":true}]`, "unix:///fixture/socket"},
		{"empty", `[]`, "JSON.InvalidIndex"},
		{"missing", `[{"Endpoints":{}}]`, "JSON.FieldAbsent"},
		{"wrong-host-type", `[{"Endpoints":{"docker":{"Host":42}}}]`, "JSON.TypeMismatch"},
		{"duplicate-host", `[{"Endpoints":{"docker":{"Host":"unix:///safe","Host":"tcp://wrong"}}}]`, "JSON.InvalidData"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source := `flow Failed(error:Error)->String{return error.kind};flow Main()->String onError Failed{contexts=` + f031Decode(fixture.document) + `;selected=JSON.at(contexts,0);return JSON.get(selected,"Endpoints") |> JSON.get("docker") |> JSON.get("Host") |> JSON.string()};return Main()`
			valueJSON(t, fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--format", "json")), fmt.Sprintf("%q", fixture.want))
		})
	}
}

func TestF031CanonicalCleanupFormatting(t *testing.T) {
	flows := cleanupFlows(t, "HumanSize", "ParseStorageLine")
	source := flows + `
flow ReportFields(size:String,count:Int,path:String)->Record{size:String,count:Int,path:String}{return Record.new(size:size,count:count,path:path)}
flow Main()->String!{
row=ParseStorageLine("1572864\tfixture path\twith tab")
return String.format("{size:>12} | {count:,} | {path}",ReportFields(HumanSize(row.bytes),1234567,row.path))
};return Main()`
	valueJSON(t, fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--format", "json")), `"    1.50 GiB | 1,234,567 | fixture path\twith tab"`)
}

func TestF031CanonicalCleanupReportRow(t *testing.T) {
	source := cleanupFlows(t, "PrintStorageRow") + `return PrintStorageRow(List.new(),0,Record.new(bytes:1610612736,path:"fixture path"))`
	got := runProgram(t, sourceFile(t, source), "run")
	const want = "    1.50 GiB  [report only] fixture path\n1\n"
	if got.status != 0 || got.stderr != "" || got.stdout != want {
		t.Fatalf("canonical report row: %+v; want %q", got, want)
	}
}

func TestF031CheckpointJSONComposition(t *testing.T) {
	source := `flow Main()->String!{tensor=Tensor.new(List.new(1.0),List.new(1));parameters=Parameters.new(Record.new(x:tensor));encoded=Checkpoint.encode(parameters);document=JSON.decode(encoded);return JSON.string(JSON.get(document,"type"))};return Main()`
	valueJSON(t, fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--format", "json")), `"Parameters<Record{x: Tensor<FP32>}>"`)
}
