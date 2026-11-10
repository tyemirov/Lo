package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacCleanupCompleteMissingHome(t *testing.T) {
	fixture := newCleanupWorkflowFixture(t)
	volumes := 0
	fixture.expectedVolumes = &volumes
	scenario := fixture.scenario(t, "preview", "missing-home", false, func(t *testing.T, actual result) {
		if actual.status != 1 || !strings.Contains(actual.stderr, "capture host home: missing HOME") || strings.Contains(actual.stdout, "Starting mac-cleanup") || cleanupWorkflowCalls(t, fixture) != "" {
			t.Fatalf("missing invocation input: %+v", actual)
		}
		cleanupWorkflowPreserved(t, fixture)
	})
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestMacCleanupCompleteMissingTemporaryRoot(t *testing.T) {
	fixture := newCleanupWorkflowFixture(t)
	saved := fixture.temporary + "-preserved"
	if err := os.Rename(fixture.temporary, saved); err != nil {
		t.Fatal(err)
	}
	scenario := fixture.scenario(t, "apply", "success", false, func(t *testing.T, actual result) {
		calls := cleanupWorkflowCalls(t, fixture)
		if actual.status != 1 || !strings.Contains(actual.stderr, "Failed categories: Temporary files") || !strings.Contains(actual.stderr, "File.DirectoryAbsent") {
			t.Fatalf("missing temporary root: calls=%q result=%+v", calls, actual)
		}
		macCleanupPresent(t, filepath.Join(fixture.shared, "old.tmp"), true)
		macCleanupPresent(t, filepath.Join(saved, "old.tmp"), true)
		if !strings.Contains(calls, "docker\t--context\tdesktop-linux\timage\tprune\t--all\t--force\n") {
			t.Fatalf("missing temporary root stopped independent Docker category: %q", calls)
		}
		macCleanupPresent(t, filepath.Join(fixture.writes, "Library/Caches/mprlab-gateway/deployments/deployment-123.log"), false)
	})
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestMacCleanupCompleteOrderedFailures(t *testing.T) {
	fixture := newCleanupWorkflowFixture(t)
	scenario := fixture.scenario(t, "apply", "multiple-failures", false, func(t *testing.T, actual result) {
		if actual.status != 1 || !strings.Contains(actual.stderr, "Failed categories: npm, uv\n") || !strings.Contains(actual.stdout, "net volume change: 2.00 GiB") {
			t.Fatalf("ordered ordinary failures: %+v", actual)
		}
		calls := cleanupWorkflowCalls(t, fixture)
		if !strings.HasPrefix(calls, "python3\t-m\tpip\t--version\nnpm\tcache\tclean\t--force\ngo\tclean\t-cache\t-testcache\nuv\tcache\tclean\t--force\npnpm\tstore\tprune\t--yes\n") {
			t.Fatalf("failed command continuation: %q", calls)
		}
		for _, paths := range [][]string{fixture.direct, fixture.recursive, fixture.removedTrees} {
			for _, path := range paths {
				macCleanupPresent(t, path, false)
			}
		}
		for _, path := range fixture.preserved {
			macCleanupPresent(t, path, true)
		}
	})
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestMacCleanupCompleteDockerFailureProgress(t *testing.T) {
	expected := []string{
		"docker\t--context\tdesktop-linux\tcontext\tinspect\tdesktop-linux",
		"docker\t--context\tdesktop-linux\tbuildx\tinspect\tdesktop-linux",
		"docker\t--context\tdesktop-linux\tsystem\tdf",
		"docker\t--context\tdesktop-linux\tbuildx\tprune\t--builder\tdesktop-linux\t--all\t--force",
		"docker\t--context\tdesktop-linux\timage\tprune\t--all\t--force",
	}
	for index, behavior := range []string{"docker-context-failure", "docker-builder-failure", "docker-usage-failure", "docker-build-failure", "docker-image-failure"} {
		t.Run(behavior, func(t *testing.T) {
			fixture := newCleanupWorkflowFixture(t)
			scenario := fixture.scenario(t, "apply", behavior, false, func(t *testing.T, actual result) {
				if actual.status != 1 || !strings.Contains(actual.stderr, "Failed categories: Docker") {
					t.Fatalf("Docker failure status: %+v", actual)
				}
				calls := cleanupWorkflowCalls(t, fixture)
				var docker []string
				for _, line := range strings.Split(calls, "\n") {
					if strings.HasPrefix(line, "docker\t") {
						docker = append(docker, line)
					}
				}
				if strings.Join(docker, "\n") != strings.Join(expected[:index+1], "\n") {
					t.Fatalf("confirmed Docker request prefix: %q, want %q", docker, expected[:index+1])
				}
				macCleanupPresent(t, filepath.Join(fixture.writes, "Library/Caches/mprlab-gateway/deployments/deployment-123.log"), false)
				for _, path := range fixture.preserved {
					macCleanupPresent(t, path, true)
				}
			})
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
		})
	}
}

func TestLLVMMacCleanupMetalArguments(t *testing.T) {
	manifest, _ := llvmMetalManifest(t)
	artifact := filepath.Join(t.TempDir(), "mac-cleanup-metal")
	source := sourceFile(t, cleanupApplication(t, "Main()"))
	built := runExecutable(t, binary, "build", source, "--toolchain", manifest, "--device-backend", "metal", "--output", artifact, "--format", "json")
	if built.status != 0 || built.stderr != "" {
		t.Fatalf("build complete Metal-linked application: %+v", built)
	}
	actual := runExecutable(t, artifact, "run", "--", "--help")
	if actual.status != 0 || actual.stderr != "" || !strings.Contains(actual.stdout, "mac-cleanup") || strings.Contains(actual.stdout, "Starting mac-cleanup") {
		t.Fatalf("Metal-linked application argument boundary: %+v", actual)
	}
}
