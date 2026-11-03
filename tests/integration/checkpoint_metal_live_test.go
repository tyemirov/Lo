//go:build metal && darwin && arm64 && cgo

package integration_test

import (
	"path/filepath"
	"testing"
)

func checkpointLiveWorkflow(t *testing.T, prepare func(string, string) func() result) {
	t.Helper()
	directory := t.TempDir()
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
			case "infer":
				checkNativeTensor(t, actual, []string{"4", "1"}, []float64{0, 1, 1, 0}, .05, 0)
			}
		}
	}
	adam := prepare(checkpointAdamWResume, directory)
	for repeat := 0; repeat < 3; repeat++ {
		actual := trainingResult(t, adam())
		assertCheckpointAdamW(t, actual)
		state := nativeCheckpoint(t, recordField(t, actual, "actual"))
		valueJSON(t, recordField(t, state, "step"), `"2"`)
		checkNativeTensor(t, recordField(t, recordField(t, state, "parameters"), "x"), []string{"2"}, []float64{.9813104194054769, -1.98848901856369}, 1e-6, 1e-5)
		checkNativeTensor(t, recordField(t, recordField(t, state, "first_moment"), "x"), []string{"2"}, []float64{.009, .012}, 1e-7, 1e-5)
		checkNativeTensor(t, recordField(t, recordField(t, state, "second_moment"), "x"), []string{"2"}, []float64{.00000999, .00012996}, 1e-9, 1e-5)
	}
}

func TestNativeMetalCheckpoints(t *testing.T) {
	compiler := binary
	checkpointLiveWorkflow(t, func(text, directory string) func() result {
		path := sourceFile(t, text)
		artifact := filepath.Join(t.TempDir(), "checkpoint")
		built := runExecutable(t, compiler, nativeMetalBuildArgs(t, path, artifact, "metal")...)
		if built.status != 0 {
			t.Fatalf("checkpoint native build: %+v", built)
		}
		return func() result {
			return runExecutable(t, artifact, "run", "--allow-device", "metal:0", "--read-root", directory, "--write-root", directory, "--format", "json")
		}
	})
}
