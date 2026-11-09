package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cleanupWorkflowCalls(t *testing.T, fixture cleanupWorkflowFixture) string {
	t.Helper()
	data, err := os.ReadFile(fixture.calls)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func cleanupWorkflowPreserved(t *testing.T, fixture cleanupWorkflowFixture) {
	t.Helper()
	for _, paths := range [][]string{fixture.preserved, fixture.direct, fixture.recursive, fixture.removedTrees} {
		for _, path := range paths {
			macCleanupPresent(t, path, true)
		}
	}
}

func TestMacCleanupCompleteDockerBoundary(t *testing.T) {
	const local = `{"Name":"desktop-linux","Endpoints":{"docker":{"Host":"unix:///fixture/docker.sock"}}}`
	const builder = "Driver: docker\nEndpoint: desktop-linux\n"
	cases := []struct{ name, variable, value, behavior string }{
		{"multiple-contexts", "FIXTURE_DOCKER_JSON", "[" + local + "," + local + "]", "success"},
		{"wrong-context-name", "FIXTURE_DOCKER_JSON", strings.Replace("["+local+"]", "desktop-linux", "other", 1), "success"},
		{"empty-endpoint", "FIXTURE_DOCKER_JSON", strings.Replace("["+local+"]", "unix:///fixture/docker.sock", "unix://", 1), "success"},
		{"relative-endpoint", "FIXTURE_DOCKER_JSON", strings.Replace("["+local+"]", "unix:///fixture/docker.sock", "unix://relative", 1), "success"},
		{"control-endpoint", "FIXTURE_DOCKER_JSON", strings.Replace("["+local+"]", "unix:///fixture/docker.sock", `unix:///fixture/\u000asock`, 1), "success"},
		{"remote-endpoint", "FIXTURE_DOCKER_JSON", strings.Replace("["+local+"]", "unix:///fixture/docker.sock", "tcp://remote:2375", 1), "success"},
		{"duplicate-json-key", "FIXTURE_DOCKER_JSON", strings.Replace("["+local+"]", `"Name":"desktop-linux"`, `"Name":"desktop-linux","Name":"desktop-linux"`, 1), "success"},
		{"wrong-shape", "FIXTURE_DOCKER_JSON", local, "success"},
		{"empty-contexts", "FIXTURE_DOCKER_JSON", "[]", "success"},
		{"wrong-endpoint-kind", "FIXTURE_DOCKER_JSON", strings.Replace("["+local+"]", `"unix:///fixture/docker.sock"`, `true`, 1), "success"},
		{"driver-duplicate", "FIXTURE_DOCKER_BUILDER", builder + "Driver: docker\n", "success"},
		{"driver-pending", "FIXTURE_DOCKER_BUILDER", builder + "Driver:\n", "success"},
		{"endpoint-pending", "FIXTURE_DOCKER_BUILDER", builder + "Endpoint:\n", "success"},
		{"continued-driver-extra-token", "FIXTURE_DOCKER_BUILDER", "Driver:\n docker remote\nEndpoint: desktop-linux\n", "success"},
		{"driver-extra-token", "FIXTURE_DOCKER_BUILDER", strings.Replace(builder, "Driver: docker", "Driver: docker remote", 1), "success"},
		{"driver-malformed-duplicate", "FIXTURE_DOCKER_BUILDER", builder + "Driver:docker\n", "success"},
		{"context-stderr", "FIXTURE_DOCKER_BUILDER", builder, "docker-context-stderr"},
		{"builder-stderr", "FIXTURE_DOCKER_BUILDER", builder, "docker-builder-stderr"},
		{"ambient-host", "DOCKER_HOST", "unix:///different.sock", "success"},
		{"ambient-context", "DOCKER_CONTEXT", "different", "success"},
		{"ambient-builder", "BUILDX_BUILDER", "different", "success"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCleanupWorkflowFixture(t)
			fixture.overrides = [][2]string{{test.variable, test.value}}
			scenario := fixture.scenario(t, "apply", test.behavior, false, func(t *testing.T, actual result) {
				calls := cleanupWorkflowCalls(t, fixture)
				if actual.status != 1 || !strings.Contains(actual.stderr, "Failed categories: Docker") {
					t.Fatalf("Docker rejection: calls=%q result=%+v", calls, actual)
				}
				for _, line := range strings.Split(calls, "\n") {
					if strings.HasPrefix(line, "docker\t") && (strings.Contains(line, "\tprune\t") || strings.Contains(line, "\tsystem\tdf")) {
						t.Fatalf("unvalidated Docker action: %q", line)
					}
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

func TestMacCleanupCompleteTemporaryBoundary(t *testing.T) {
	for _, name := range []string{"temp-failure", "temp-stderr", "empty", "wrong-prefix", "wrong-name", "multiple-lines", "temp-owner"} {
		t.Run(name, func(t *testing.T) {
			fixture := newCleanupWorkflowFixture(t)
			switch name {
			case "empty":
				fixture.overrides = [][2]string{{"FIXTURE_USER_TEMP", ""}}
			case "wrong-prefix":
				fixture.overrides = [][2]string{{"FIXTURE_USER_TEMP", fixture.shared + "/T"}}
			case "wrong-name":
				fixture.overrides = [][2]string{{"FIXTURE_USER_TEMP", fixture.temporaryPrefix + "/account/Other"}}
			case "multiple-lines":
				fixture.overrides = [][2]string{{"FIXTURE_USER_TEMP", fixture.temporary + "\n" + fixture.temporary}}
			}
			scenario := fixture.scenario(t, "apply", name, false, func(t *testing.T, actual result) {
				calls := cleanupWorkflowCalls(t, fixture)
				if actual.status != 1 || !strings.Contains(actual.stderr, "Failed categories: Temporary files") {
					t.Fatalf("temporary root rejection: calls=%q result=%+v", calls, actual)
				}
				for _, root := range []string{fixture.shared, fixture.temporary} {
					macCleanupPresent(t, filepath.Join(root, "old.tmp"), true)
				}
				if !strings.Contains(calls, "docker\t--context\tdesktop-linux\timage\tprune\t--all\t--force") {
					t.Fatalf("temporary failure stopped Docker: %q", calls)
				}
				macCleanupPresent(t, filepath.Join(fixture.writes, "Library/Caches/mprlab-gateway/deployments/deployment-123.log"), false)
			})
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
		})
	}
}

func TestMacCleanupCompleteReportFailure(t *testing.T) {
	for _, behavior := range []string{"inventory-failure", "inventory-stderr", "inventory-malformed", "inventory-empty", "inventory-partial", "inventory-duplicate", "inventory-unknown", "inventory-negative"} {
		t.Run(behavior, func(t *testing.T) {
			fixture := newCleanupWorkflowFixture(t)
			scenario := fixture.scenario(t, "report", behavior, false, func(t *testing.T, actual result) {
				calls := cleanupWorkflowCalls(t, fixture)
				if actual.status != 1 || !strings.Contains(actual.stderr, "FAILED Storage report:") || strings.Count(calls, "\n") != 1 || strings.Contains(actual.stdout, "Storage inventory (allocated size;") {
					t.Fatalf("report rejection: calls=%q result=%+v", calls, actual)
				}
				cleanupWorkflowPreserved(t, fixture)
			})
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
		})
	}
}

func TestMacCleanupCompleteTerminalStop(t *testing.T) {
	fixture := newCleanupWorkflowFixture(t)
	volumes := 1
	fixture.expectedVolumes = &volumes
	fixture.extraFlags = []string{"--max-process-output-bytes", "2"}
	scenario := fixture.scenario(t, "apply", "success", false, func(t *testing.T, actual result) {
		calls := cleanupWorkflowCalls(t, fixture)
		if actual.status != 2 || !strings.Contains(actual.stderr, "runtime.limit") || calls != "python3\t-m\tpip\t--version\n" {
			t.Fatalf("terminal output stop: calls=%q result=%+v", calls, actual)
		}
		if strings.Contains(actual.stdout, "Cleanup complete") || strings.Contains(actual.stderr, "Failed categories:") {
			t.Fatalf("terminal failure entered ordinary summary: %+v", actual)
		}
		cleanupWorkflowPreserved(t, fixture)
	})
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestMacCleanupCompleteBusyLock(t *testing.T) {
	fixture := newCleanupWorkflowFixture(t)
	volumes := 0
	fixture.expectedVolumes = &volumes
	fixture.busy = true
	scenario := fixture.scenario(t, "preview", "success", false, func(t *testing.T, actual result) {
		if actual.status != 1 || !strings.Contains(actual.stderr, "Another cleanup instance is running") || strings.Contains(actual.stdout, "Starting mac-cleanup") || cleanupWorkflowCalls(t, fixture) != "" {
			t.Fatalf("entry lock boundary: %+v", actual)
		}
		cleanupWorkflowPreserved(t, fixture)
	})
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestMacCleanupCompleteAbsentTools(t *testing.T) {
	fixture := newCleanupWorkflowFixture(t)
	for _, name := range []string{"npm", "python3", "docker"} {
		if err := os.Remove(fixture.tools[name]); err != nil {
			t.Fatal(err)
		}
		delete(fixture.tools, name)
	}
	scenario := fixture.scenario(t, "apply", "success", false, func(t *testing.T, actual result) {
		calls := cleanupWorkflowCalls(t, fixture)
		if actual.status != 0 || !strings.Contains(actual.stdout, "SKIP npm: not installed") || !strings.Contains(actual.stdout, "SKIP Docker: not installed") || strings.Contains("\n"+calls, "\npython3\t") || strings.Contains("\n"+calls, "\ndocker\t") || strings.Contains("\n"+calls, "\nnpm\t") {
			t.Fatalf("optional tool discovery: calls=%q result=%+v", calls, actual)
		}
		if !strings.HasPrefix(calls, "go\tclean\t-cache\t-testcache\nuv\tcache\tclean\t--force\npnpm\tstore\tprune\t--yes\n") {
			t.Fatalf("independent command continuation: %q", calls)
		}
	})
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestMacCleanupCompleteLiteralReportPaths(t *testing.T) {
	fixture := newCleanupWorkflowFixture(t)
	oldHome := fixture.writes
	fixture.writes = filepath.Join(fixture.root, "home $(touch injected) ;")
	if err := os.Rename(oldHome, fixture.writes); err != nil {
		t.Fatal(err)
	}
	fixture.derived = strings.Replace(fixture.derived, oldHome, fixture.writes, 1)
	for _, paths := range [][]string{fixture.direct, fixture.recursive, fixture.removedTrees, fixture.preserved} {
		for index, path := range paths {
			paths[index] = strings.Replace(path, oldHome, fixture.writes, 1)
		}
	}
	scenario := fixture.scenario(t, "report", "success", false, func(t *testing.T, actual result) {
		calls := cleanupWorkflowCalls(t, fixture)
		if actual.status != 0 || !strings.Contains(calls, fixture.writes+"/Library/Caches/staticcheck") || strings.Count(calls, "\n") != 1 || !strings.Contains(actual.stdout, fixture.writes) {
			t.Fatalf("literal report paths: calls=%q result=%+v", calls, actual)
		}
		macCleanupPresent(t, filepath.Join(fixture.root, "injected"), false)
		cleanupWorkflowPreserved(t, fixture)
	})
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestMacCleanupCompleteReportSymlink(t *testing.T) {
	fixture := newCleanupWorkflowFixture(t)
	path := filepath.Join(fixture.writes, ".ollama")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fixture.prunes, path); err != nil {
		t.Fatal(err)
	}
	scenario := fixture.scenario(t, "report", "success", false, func(t *testing.T, actual result) {
		calls := cleanupWorkflowCalls(t, fixture)
		if actual.status != 0 || strings.Contains(calls, "\t"+path) || strings.Count(calls, "\n") != 1 {
			t.Fatalf("report symlink exclusion: calls=%q result=%+v", calls, actual)
		}
		target, err := os.Readlink(path)
		if err != nil || target != fixture.prunes {
			t.Fatalf("report changed symlink: %q %v", target, err)
		}
		cleanupWorkflowPreserved(t, fixture)
	})
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
}
