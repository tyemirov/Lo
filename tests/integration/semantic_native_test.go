package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func semanticNativeTarget() string {
	if target := os.Getenv("LO_TEST_TARGET"); target != "" {
		return target
	}
	return runtime.GOOS + "/" + runtime.GOARCH
}

func semanticNativeScenarios(t *testing.T, scenarios []llvmRuntimeScenario) {
	t.Helper()
	t.Setenv("LO_TEST_BACKEND", "llvm")
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}

func semanticNativeEnvelope(t *testing.T, result value.Value) string {
	t.Helper()
	serialized, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return `{"command":"run","status":"ok","result":` + string(serialized) + "}\n"
}

func semanticNativeObservation(source string, number int) llvmRuntimeScenario {
	return llvmRuntimeScenario{name: "native-runtime", source: source, number: number, captureOutput: number >= 2, harness: "testdata/native-qualification/semantic-effects.c", transform: func(t *testing.T, module []byte) []byte {
		t.Helper()
		contents := strings.ReplaceAll(string(module), "call void @lo_op_File_2Eread(", "call void @lo_semantic_read(")
		contents = strings.ReplaceAll(contents, "call void @lo_commit(", "call void @lo_semantic_commit(")
		contents = strings.Replace(contents, "define void @lo_entry(", "define void @lo_semantic_entry(", 1)
		return []byte(contents + "\ndeclare void @lo_semantic_read(ptr,ptr,i64,i64,ptr)\ndeclare void @lo_semantic_commit(ptr,ptr,i64,i64,ptr)\n")
	}}
}

func TestSemanticNativeReadOrder(t *testing.T) {
	const text = `flow Read(path: String) -> String! { bytes=File.read(File.Path.new(path)); return UTF8.String.new(bytes) }
flow Join(a: String,b: String) -> String { return String.concat(a,b) }
flow Main() -> String! { unused=Read("unused");first=Read("repeat");second=Read("repeat");ordered=Join(b:Read("b"),a:Read("a"));return String.concat(first,second,ordered) };return Main()`
	boundary := &observedBoundary{read: func(path string, count int) (value.Bytes, error) {
		return value.NewBytes([]byte(fmt.Sprintf("%s:%d;", path, count))), nil
	}}
	reference, err := evaluate(t, text, boundary)
	if err != nil {
		t.Fatal(err)
	}
	if value.TextResult(reference) != "repeat:2;repeat:3;a:5;b:4;" || !reflect.DeepEqual(boundary.reads, []string{"unused", "repeat", "repeat", "b", "a"}) || boundary.commits != 0 {
		t.Fatalf("independent read order: %v, %v", reference, boundary)
	}
	scenario := semanticNativeObservation(text, 0)
	scenario.expectedStdout = semanticNativeEnvelope(t, reference)
	semanticNativeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestSemanticNativeMatchRegions(t *testing.T) {
	const text = `flow Handle(error: Error) -> String { return "failed" }
flow Read(path: String) -> String onError Handle { bytes=File.read(File.Path.new(path)); return UTF8.String.new(bytes) }
return Match.new(Read("selector"),"miss":Read("unselected"),"selector":Read("selected"),"unreached":Read("unreached-body"),_:Read("default"))`
	boundary := &observedBoundary{read: func(path string, _ int) (value.Bytes, error) { return value.NewBytes([]byte(path)), nil }}
	reference, err := evaluate(t, text, boundary)
	if err != nil {
		t.Fatal(err)
	}
	if value.TextResult(reference) != "selected" || !reflect.DeepEqual(boundary.reads, []string{"selector", "selected"}) || boundary.commits != 0 {
		t.Fatalf("independent match regions: %v, %v", reference, boundary)
	}
	scenario := semanticNativeObservation(text, 1)
	scenario.expectedStdout = semanticNativeEnvelope(t, reference)
	semanticNativeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestSemanticNativeErrorPrecedence(t *testing.T) {
	const text = `flow Handle(error: Error) -> Error { return error }
flow Main() -> Error onError Handle { first=File.read(File.Path.new("first"));later=File.read(File.Path.new("later"));return Error.new("missed","",Record.new()) };return Main()`
	boundary := &observedBoundary{read: func(path string, _ int) (value.Bytes, error) {
		return nil, fmt.Errorf("read %s: injected failure", path)
	}}
	reference, err := evaluate(t, text, boundary)
	if err != nil {
		t.Fatal(err)
	}
	failure, ok := reference.(value.Error)
	if !ok || failure.Kind() != "File.ReadFailed" || len(failure.Trace()) != 1 || !reflect.DeepEqual(boundary.reads, []string{"first"}) || boundary.commits != 0 {
		t.Fatalf("independent first failure: %v, %v", reference, boundary)
	}
	if failure.Input().Field("file_path").(value.Text).Text() != "first" {
		t.Fatal("wrong failed read input")
	}
	semanticNativeScenarios(t, []llvmRuntimeScenario{semanticNativeObservation(text, 2)})
}

func TestSemanticNativeCaptureLargeOutput(t *testing.T) {
	// The fixture must consume the complete output, including the final marker.
	text := fmt.Sprintf("return %q", strings.Repeat("A", 2<<20)+"CAPTURE_TRAILER_B005")
	scenario := semanticNativeObservation(text, 4)
	scenario.expectedStdout = "capture-complete\n"
	scenario.timeout = 5 * time.Second
	semanticNativeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestSemanticNativeCancellation(t *testing.T) {
	// Both implementations must reject cancellation before any host observation.
	TestEvaluatorCancellation(t)
	const text = `flow Handle(error: Error)->Int{return 99}
flow Main()->Int onError Handle { bytes=File.read(File.Path.new("unread"));target=File.Write.new(File.Path.new("unwritten"),bytes);commit target;return 1 };return Main()`
	semanticNativeScenarios(t, []llvmRuntimeScenario{semanticNativeObservation(text, 3)})
}

func TestSemanticNativeMetalProfile(t *testing.T) {
	const text = `flow Main()->Record{arithmetic:Tensor<FP32>,layout:Tensor<FP32>,reduction:Tensor<FP32>}!{
 metal=Device.new("metal:0");cpu=Device.new("cpu")
 a=Tensor.to(Tensor.new(List.new(-0.0,1.401298464324817e-45,-1.401298464324817e-45),List.new(3)),metal)
 b=Tensor.to(Tensor.new(List.new(0.0),List.new()),metal)
 arithmetic=Tensor.add(a,b);layout=Tensor.transpose(a,List.new(0))
 values=Tensor.to(Tensor.new(List.new(16777216.0,1.0,-16777216.0),List.new(3)),metal);reduction=Tensor.sum(values,List.new(0))
 return Record.new(arithmetic:Tensor.to(arithmetic,cpu),layout:Tensor.to(layout,cpu),reduction:Tensor.to(reduction,cpu)) };return Main()`
	result, receipts, expected := metalResult(t, text, metalComputeFactory())
	for field, bits := range map[string][]uint32{"arithmetic": {0, 0, 0}, "layout": {0x80000000, 1, 0x80000001}, "reduction": {0}} {
		numbers := tensorNumbers(t, recordField(t, result, field))
		if len(numbers) != len(bits) {
			t.Fatalf("%s length", field)
		}
		for index, number := range numbers {
			if math.Float32bits(float32(number)) != bits[index] {
				t.Fatalf("%s[%d] bits", field, index)
			}
		}
	}
	semanticTransferCount(t, receipts, 6)
	semanticNativeScenarios(t, []llvmRuntimeScenario{{name: "profile", source: text, expectedStdout: expected, harness: "testdata/native-qualification/semantic-metal.c", number: 0}})
}

func semanticTransferCount(t *testing.T, receipts []device.Receipt, expected int) {
	t.Helper()
	transfers := 0
	for _, receipt := range receipts {
		if receipt.Event == device.EventTransfer {
			transfers++
		}
	}
	if transfers != expected {
		t.Fatalf("explicit transfers %d want %d", transfers, expected)
	}
	assertReleased(t, receipts)
}

func TestSemanticNativeMetalCheckpointResume(t *testing.T) {
	for index, fixture := range []struct {
		optimizer, config, first, second string
		transfers                        int
	}{
		{"SGD", `Optimizer.SGD.Config.new(0.25)`, semanticOptimizerCheckpoint("SGD", "1.5", "1", "", "", ""), semanticOptimizerCheckpoint("SGD", "1", "2", "", "", ""), 5},
		{"AdamW", `Optimizer.AdamW.Config.new(0.25,0.5,0.5,2.0,0.5)`, semanticOptimizerCheckpoint("AdamW", "1.625", "1", "1", "2", "0.5"), semanticOptimizerCheckpoint("AdamW", "1.296875", "2", "1.5", "3", "0.25"), 13},
	} {
		t.Run(fixture.optimizer, func(t *testing.T) {
			text := fmt.Sprintf(`flow Main()->Bytes!{
 metal=Device.new("metal:0");cpu=Device.new("cpu");x=Tensor.new(List.new(2.0),List.new());p=Parameters.new(Record.new(x:x))
 mp=Parameters.to(p,metal);cp=Parameters.to(mp,cpu);template=Optimizer.%s.State.new(cp,%s)
 restored=Checkpoint.decode(Bytes.new(%q),template);moved=Optimizer.%s.State.to(restored,metal)
 gradient=Tensor.to(x,metal);g=Gradients.new(moved.parameters,Record.new(x:gradient));resumed=Optimizer.%s.step(moved,g)
 host=Optimizer.%s.State.to(resumed,cpu);return Checkpoint.encode(host) };return Main()`, fixture.optimizer, fixture.config, fixture.first, fixture.optimizer, fixture.optimizer, fixture.optimizer)
			result, receipts, expected := metalResult(t, text, metalComputeFactory())
			assertSemanticCheckpoint(t, result, fixture.second)
			semanticTransferCount(t, receipts, fixture.transfers)
			semanticNativeScenarios(t, []llvmRuntimeScenario{{name: "resume", source: text, expectedStdout: expected, harness: "testdata/native-qualification/semantic-metal.c", number: index + 1}})
		})
	}
}

func TestSemanticNativeMetalFailureCleanup(t *testing.T) {
	const text = `flow Handle(e:Error)->String{return e.kind};flow Main()->String onError Handle {a=Tensor.to(Tensor.new(List.new(-1.0),List.new()),Device.new("metal:0"));unused=Tensor.sqrt(a);target=File.Write.new(File.Path.new("later"),Bytes.new("bad"));commit target;return "unexpected"};return Main()`
	result, _, expected := metalResult(t, text, metalComputeFactory())
	valueJSON(t, result, `"Numeric.Domain"`)
	semanticNativeScenarios(t, []llvmRuntimeScenario{{name: "numeric-error", source: text, expectedStdout: strings.Repeat(expected, 3), harness: "testdata/native-runtime/metal-executor.c"}})
	// The native model cancels the real context during compute and checks that
	// cancellation wins over a concurrent device error and releases all buffers.
	semanticNativeScenarios(t, llvmMetalCancellationScenarios())
	ctx, cancel := context.WithCancel(context.Background())
	model := &metalModel{before: func(device.Operation) error { cancel(); return ctx.Err() }}
	got, receipts := runWithDevices(t, ctx, text, model, "--allow-device", "metal:0")
	if got.status != 2 || !strings.Contains(got.stderr, "runtime.cancelled") || strings.Contains(got.stderr, "Numeric.Domain") {
		t.Fatalf("interpreter device cancellation: %+v", got)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("reference context not cancelled")
	}
	assertReleased(t, receipts)
}
