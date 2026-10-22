package integration_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestLLVMOptimizerWorkCharges(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the LLVM runtime lane")
	}
	for _, fixture := range []struct {
		name, config string
		delta        uint64
	}{
		{"SGD", "Optimizer.SGD.Config.new(0.1)", 36},
		{"AdamW", "Optimizer.AdamW.Config.new(0.01,0.9,0.999,1e-8,0.1)", 180},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			program := func(size int) string {
				return fmt.Sprintf(`flow Run() -> Optimizer.%s.State<Record{x:Tensor<FP32>}>! { one=Tensor.new(List.new(1.0),List.new()); x=Tensor.broadcast(one,List.new(%d)); p=Parameters.new(Record.new(x:x)); state=Optimizer.%s.State.new(p,%s); return Optimizer.%s.step(state,Gradients.zero(p)) }; return Run()`, fixture.name, size, fixture.name, fixture.config, fixture.name)
			}
			small := minimumLLVMWorkSteps(t, program(1))
			large := minimumLLVMWorkSteps(t, program(10))
			if large < small || large-small != fixture.delta {
				t.Fatalf("optimizer charge delta: small=%d large=%d expected=%d", small, large, fixture.delta)
			}
		})
	}
}

func TestLLVMCheckpointBitsAndWork(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the LLVM runtime lane")
	}
	program := func(size int) string {
		return fmt.Sprintf(`flow Run()->Parameters<Record{x:Tensor<FP32>}>!{x=Tensor.broadcast(Tensor.new(List.new(1.0),List.new()),List.new(%d));p=Parameters.new(Record.new(x:x));data=Checkpoint.encode(p);return Checkpoint.decode(data,p)};return Run()`, size)
	}
	small := minimumLLVMWorkSteps(t, program(1))
	large := minimumLLVMWorkSteps(t, program(100))
	if large <= small+100 {
		t.Fatalf("checkpoint work not charged: small=%d large=%d", small, large)
	}
	var scenarios []llvmRuntimeScenario
	for number, fixture := range []struct{ name, elements, shape string }{{"fp32-bits", "1.25,-0.0,1e-45,-2.5", "4"}, {"empty", "", "2,0,3"}} {
		text := fmt.Sprintf(`flow Run()->Record{before:Bytes,after:Bytes}!{x=Tensor.new(List.new(%s),List.new(%s));p=Parameters.new(Record.new(x:x));before=Checkpoint.encode(p);loaded=Checkpoint.decode(before,p);moved=Parameters.to(loaded,Device.new("metal:0"));returned=Parameters.to(moved,Device.new("cpu"));after=Checkpoint.encode(returned);return Record.new(before:before,after:after)};return Run()`, fixture.elements, fixture.shape)
		value, _, expected := metalResult(t, text, metalComputeFactory())
		if string(recordField(t, value, "before").Value) != string(recordField(t, value, "after").Value) {
			t.Fatal("checkpoint FP32 bits changed")
		}
		scenarios = append(scenarios, llvmRuntimeScenario{name: fixture.name, source: text, expectedStdout: strings.Repeat(expected, 3), harness: "testdata/native-qualification/checkpoint-bits.c", number: number})
	}
	runLLVMRuntimeScenarios(t, scenarios)
}
