//go:build darwin && arm64

package integration_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestF036InjectedPrivateInvocationIsolation(t *testing.T) {
	first, second := newF036Fixture(t), newF036Fixture(t)
	programs := []f036InjectedProgram{first.injectedProgram("fork"), second.injectedProgram("fork")}
	if programs[0].source != programs[1].source {
		t.Fatal("private invocation paths require one shared compiled module")
	}
	t.Setenv("LO_TEST_SESSION_DIRECTORY", t.TempDir())
	for index, fixture := range []f036Fixture{first, second} {
		program := programs[index]
		runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "private-invocation", source: program.source, number: 0,
			harness: "testdata/native-runtime/process-ownership-provider.c", hostBootstrap: "lo_test_host_init", sdkHarness: true,
			harnessTransform: fixture.transform, run: func(t *testing.T, artifact, _ string) {
				arguments := append(fixture.flags(t, artifact), "--process-kernel-limit", fixture.kernelArgument())
				command := fixture.injectedCommand(artifact, program, arguments)
				output, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("private invocation: %v %s", err, output)
				}
				nativeValueCheck("Int", `"0"`)(t, result{stdout: string(output)})
				macCleanupPresent(t, fixture.ready, true)
				if index == 0 {
					macCleanupPresent(t, second.ready, false)
				}
				fixture.wait(t, "complete private invocation cleanup", func() bool { return fixture.logged("C\n") && !fixture.lockBusy(t) && !fixture.anyAlive(t) })
			}}})
		entries, err := os.ReadDir(filepath.Join(runtimeCacheRoot(t), "objects"))
		if err != nil {
			t.Fatal(err)
		}
		modules := 0
		for _, entry := range entries {
			if _, err := os.Stat(filepath.Join(runtimeCacheRoot(t), "objects", entry.Name(), "source.ll")); err == nil {
				modules++
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
		if modules != 1 {
			t.Fatalf("private invocations rebuilt the shared LLVM module: got %d modules after invocation %d", modules, index+1)
		}
	}
}
