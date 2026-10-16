//go:build metal && darwin && arm64 && cgo

package integration_test

import (
	"path/filepath"
	"testing"
)

func TestMetalLiveXOR(t *testing.T) {
	compiler := filepath.Join(repository, "bin", "lo-metal")
	cpu := runExecutable(t, binary, "run", filepath.Join(repository, "examples", "xor_cpu.lo"), "--format", "json")
	assertXORResult(t, trainingResult(t, cpu))
	for repeat := 0; repeat < 3; repeat++ {
		actual := runExecutable(t, compiler, "run", filepath.Join(repository, "examples", "xor_metal.lo"), "--allow-device", "metal:0", "--format", "json")
		assertXORResult(t, trainingResult(t, actual))
		if actual != cpu {
			t.Fatalf("XOR Metal/CPU mismatch: Metal=%+v CPU=%+v", actual, cpu)
		}
	}
}

func TestNativeMetalLiveXOR(t *testing.T) {
	compiler := filepath.Join(repository, "bin", "lo-metal")
	source := filepath.Join(repository, "examples", "xor_metal.lo")
	artifact := filepath.Join(t.TempDir(), "xor")
	built := runExecutable(t, compiler, "build", source, "--device-backend", "metal", "--output", artifact, "--format", "json")
	if built.status != 0 {
		t.Fatalf("XOR native build: %+v", built)
	}
	expected := runExecutable(t, compiler, "run", source, "--allow-device", "metal:0", "--format", "json")
	assertXORResult(t, trainingResult(t, expected))
	for repeat := 0; repeat < 3; repeat++ {
		actual := runExecutable(t, artifact, "run", "--allow-device", "metal:0", "--format", "json")
		assertXORResult(t, trainingResult(t, actual))
		if actual != expected {
			t.Fatalf("XOR native/interpreter mismatch: native=%+v interpreter=%+v", actual, expected)
		}
	}
}
