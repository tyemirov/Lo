package integration_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLLVMLinuxDerivatives(t *testing.T) {
	t.Run("shared-edges", func(t *testing.T) {
		artifact := linuxLLVMArtifact(t, `flow Square(x: Tensor<FP32>) -> Tensor<FP32>! { return Tensor.mul(x, x) }
flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! {
 first=Square(p.x); second=Square(p.x); return Tensor.sum(Tensor.add(first,second),List.new(0))
}
flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(2.0,3.0),List.new(2)); result=Autodiff.grad(Record.new(x:x),Loss); return result.gradients.x }; return Run()`)
		value := trainingResult(t, runLinuxLLVMExecutable(t, artifact, "run", "--format", "json"))
		checkNumbers(t, tensorNumbers(t, value), []float64{8, 12}, 1e-6, 1e-5)
	})
	for _, fixture := range derivativeFixtures() {
		if fixture.name != "add-broadcast" && fixture.name != "matmul" {
			continue
		}
		t.Run(fixture.name, func(t *testing.T) {
			for _, field := range []string{"a", "b"} {
				t.Run(field, func(t *testing.T) {
					artifact := linuxLLVMArtifact(t, fixture.source("Autodiff.grad(p,Loss).gradients."+field, fixture.a, fixture.b))
					value := trainingResult(t, runLinuxLLVMExecutable(t, artifact, "run", "--format", "json"))
					expected := fixture.ga
					if field == "b" {
						expected = fixture.gb
					}
					checkNumbers(t, tensorNumbers(t, value), expected, 1e-6, 1e-5)
				})
			}
		})
	}
}

func TestLLVMLinuxCPUTraining(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(repository, "examples", "training_cpu.lo"))
	if err != nil {
		t.Fatal(err)
	}
	artifact := linuxLLVMArtifact(t, string(source))
	var previous string
	for repeat := 0; repeat < 2; repeat++ {
		got := runLinuxLLVMExecutable(t, artifact, "run", "--format", "json")
		value := trainingResult(t, got)
		if repeat > 0 && got.stdout != previous {
			t.Fatal("Linux CPU training is not bitwise repeatable")
		}
		previous = got.stdout
		valueJSON(t, recordField(t, value, "initial_loss"), `"2.46875"`)
		loss := trainingFloat(t, recordField(t, value, "loss"))
		checkNumbers(t, []float64{loss}, []float64{.00013215476064942777}, 1e-8, 1e-5)
		if loss >= .001 {
			t.Fatalf("Linux CPU training loss=%g", loss)
		}
		parameters := recordField(t, value, "parameters")
		for _, expected := range []struct {
			name   string
			values []float64
		}{
			{"w1", []float64{1.1966239213943481, -.045796893537044525, .08913600444793701, 1.388328194618225}},
			{"b1", []float64{.19662371277809143, .38832828402519226}},
			{"w2", []float64{.7451153993606567, 1.0836396217346191}},
			{"b2", []float64{.021668776869773865}},
		} {
			checkNumbers(t, tensorNumbers(t, recordField(t, parameters, expected.name)), expected.values, 1e-6, 1e-5)
		}
	}
}

func TestLLVMLinuxXOR(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(repository, "examples", "xor_cpu.lo"))
	if err != nil {
		t.Fatal(err)
	}
	artifact := linuxLLVMArtifact(t, string(source))
	var previous string
	for repeat := 0; repeat < 3; repeat++ {
		got := runLinuxLLVMExecutable(t, artifact, "run", "--format", "json")
		assertXORResult(t, trainingResult(t, got))
		if repeat > 0 && got.stdout != previous {
			t.Fatal("Linux XOR CPU results changed between runs")
		}
		previous = got.stdout
	}
}
