//go:build metal && darwin && arm64 && cgo

package integration_test

import (
	"path/filepath"
	"testing"
)

func checkpointLiveWorkflow(t *testing.T, prepare func(string, string) func() result) {
	t.Helper()
	directory := t.TempDir()
	baseline := trainingResult(t, runExecutable(t, binary, "run", filepath.Join(repository, "examples", "xor_cpu.lo"), "--format", "json"))
	stages := []string{"save", "resume", "infer"}
	executions := make([]func() result, len(stages))
	for index, stage := range stages {
		executions[index] = prepare(checkpointXORSource(t, stage, "metal", directory), directory)
	}
	for repeat := 0; repeat < 3; repeat++ {
		for index, stage := range stages {
			actual := trainingResult(t, executions[index]())
			switch stage {
			case "save":
				valueJSON(t, recordField(t, actual, "steps"), `"250"`)
			case "resume":
				assertXORResult(t, actual)
				if string(actual.Value) != string(baseline.Value) {
					t.Fatal("local Metal resumption differs from uninterrupted CPU result")
				}
			case "infer":
				if string(actual.Value) != string(recordField(t, baseline, "predictions").Value) {
					t.Fatal("local Metal inference differs")
				}
			}
		}
	}
	adam := prepare(checkpointAdamWResume, directory)
	for repeat := 0; repeat < 3; repeat++ {
		assertCheckpointAdamW(t, trainingResult(t, adam()))
	}
}

func TestMetalLiveCheckpoints(t *testing.T) {
	compiler := filepath.Join(repository, "bin", "lo-metal")
	checkpointLiveWorkflow(t, func(text, directory string) func() result {
		path := sourceFile(t, text)
		return func() result {
			return runExecutable(t, compiler, "run", path, "--allow-device", "metal:0", "--read-root", directory, "--write-root", directory, "--format", "json")
		}
	})
}

func TestNativeMetalCheckpoints(t *testing.T) {
	compiler := filepath.Join(repository, "bin", "lo-metal")
	checkpointLiveWorkflow(t, func(text, directory string) func() result {
		path := sourceFile(t, text)
		artifact := filepath.Join(t.TempDir(), "checkpoint")
		built := runExecutable(t, compiler, "build", path, "--device-backend", "metal", "--output", artifact, "--format", "json")
		if built.status != 0 {
			t.Fatalf("checkpoint native build: %+v", built)
		}
		return func() result {
			return runExecutable(t, artifact, "run", "--allow-device", "metal:0", "--read-root", directory, "--write-root", directory, "--format", "json")
		}
	})
}
