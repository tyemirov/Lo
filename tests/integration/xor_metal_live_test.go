//go:build metal && darwin && arm64 && cgo

package integration_test

import (
	"path/filepath"
	"testing"
)

func TestMetalLiveXOR(t *testing.T) {
	compiler := binary
	cpu := runProgram(t, filepath.Join(repository, "examples", "xor_cpu.lo"), "run", "--format", "json")
	assertXORResult(t, trainingResult(t, cpu))
	source := filepath.Join(repository, "examples", "xor_metal.lo")
	artifact := filepath.Join(t.TempDir(), "xor")
	built := runExecutable(t, compiler, nativeMetalBuildArgs(t, source, artifact, "metal")...)
	if built.status != 0 {
		t.Fatalf("XOR Metal build: %+v", built)
	}
	for repeat := 0; repeat < 3; repeat++ {
		actual := runExecutable(t, artifact, "run", "--allow-device", "metal:0", "--format", "json")
		assertXORResult(t, trainingResult(t, actual))
	}
}

func TestNativeMetalLiveXOR(t *testing.T) {
	compiler := binary
	source := filepath.Join(repository, "examples", "xor_metal.lo")
	artifact := filepath.Join(t.TempDir(), "xor")
	built := runExecutable(t, compiler, "build", source, "--device-backend", "metal", "--output", artifact, "--format", "json")
	if built.status != 0 {
		t.Fatalf("XOR native build: %+v", built)
	}
	for repeat := 0; repeat < 3; repeat++ {
		actual := runExecutable(t, artifact, "run", "--allow-device", "metal:0", "--format", "json")
		assertXORResult(t, trainingResult(t, actual))
	}
}
