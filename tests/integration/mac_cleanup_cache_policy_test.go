package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func macCleanupCachePolicyScenario(t *testing.T, fixture macCleanupFixture, name string, number int, source, stage, removed string, verify func(*testing.T, result)) llvmRuntimeScenario {
	t.Helper()
	arguments := []string{"fixture", "run"}
	if number != 0 {
		arguments = append(arguments, "--format", "json")
	}
	arguments = append(arguments, fixture.cacheFlags(t, true)...)
	quoted := make([]string, len(arguments))
	for index, argument := range arguments {
		quoted[index] = fmt.Sprintf("%q", argument)
	}
	return llvmRuntimeScenario{
		name: name, number: number, source: source, timeout: 45 * time.Second,
		harness: "testdata/native-runtime/mac-cleanup-cache-policy-observer.c",
		transform: func(t *testing.T, module []byte) []byte {
			t.Helper()
			const entry = "define void @lo_entry("
			if strings.Count(string(module), entry) != 1 {
				t.Fatal("missing unique cache policy entry")
			}
			return []byte(strings.Replace(string(module), entry, "define void @lo_cleanup_policy_entry(", 1))
		},
		harnessTransform: func(t *testing.T, data []byte) []byte {
			t.Helper()
			return []byte(strings.NewReplacer(
				"LO_FIXTURE_ROOT", fmt.Sprintf("%q", fixture.writes),
				"LO_FIXTURE_STAGE", fmt.Sprintf("%q", stage),
				"LO_FIXTURE_REMOVED", fmt.Sprintf("%q", removed),
				"LO_FIXTURE_ARGUMENTS", strings.Join(quoted, ", "),
			).Replace(string(data)))
		},
		verify: verify,
	}
}

func TestMacCleanupCacheForeignSubtree(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	foreign := f033Directory(t, fixture.writes, "a-foreign")
	allowed := f033Directory(t, fixture.writes, "z-owned")
	preserved := fileFixture(t, foreign, "old.cache", "foreign subtree")
	deleted := fileFixture(t, allowed, "old.cache", "owned subtree")
	scenario := macCleanupCachePolicyScenario(t, fixture, "foreign-subtree", 0,
		fixture.cacheSource(t, true, "scoped", "", ""), foreign, "", func(t *testing.T, actual result) {
			t.Helper()
			if actual.status != 0 || actual.stderr != "" || !strings.Contains(actual.stdout, "1 stale files") || !strings.Contains(actual.stdout, "deleted 1") {
				t.Fatalf("foreign cache subtree: %+v", actual)
			}
			macCleanupPresent(t, preserved, true)
			macCleanupPresent(t, deleted, false)
			macCleanupPresent(t, foreign, true)
			macCleanupPresent(t, allowed, true)
			calls, err := os.ReadFile(fixture.calls)
			if err != nil || string(calls) != "-F0n\n--\n"+deleted+"\n" {
				t.Fatalf("foreign subtree was probed: %q, %v", calls, err)
			}
		})
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestMacCleanupCacheCancellationProgress(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	firstChild := f033Directory(t, fixture.writes, "a")
	laterChild := f033Directory(t, fixture.writes, "z")
	removed := fileFixture(t, fixture.writes, "root.cache", "confirmed deletion")
	firstPending := fileFixture(t, firstChild, "first.cache", "preserved")
	laterPending := fileFixture(t, laterChild, "later.cache", "preserved")
	scenario := macCleanupCachePolicyScenario(t, fixture, "cancel-after-root-removal", 1,
		fixture.cacheSource(t, true, "scoped", "", ""), firstChild, removed, func(t *testing.T, actual result) {
			t.Helper()
			if actual.status != 0 || actual.stderr != "" || !strings.Contains(actual.stdout, "runtime.cancelled") || strings.Contains(actual.stdout, "stale files") || strings.Contains(actual.stdout, "PLAN only") {
				t.Fatalf("cache cancellation published a normal summary: %+v", actual)
			}
			macCleanupPresent(t, removed, false)
			macCleanupPresent(t, firstPending, true)
			macCleanupPresent(t, laterPending, true)
			calls, err := os.ReadFile(fixture.calls)
			if err != nil || string(calls) != "-F0n\n--\n"+removed+"\n" {
				t.Fatalf("cache cancellation continued probes: %q, %v", calls, err)
			}
			var report struct {
				Diagnostics []struct {
					Code     string         `json:"code"`
					Progress []runtimeValue `json:"flow_progress"`
				} `json:"diagnostics"`
			}
			if err := json.Unmarshal([]byte(actual.stdout), &report); err != nil {
				t.Fatal(err)
			}
			if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "runtime.cancelled" {
				t.Fatalf("missing terminal cache diagnostic: %s", actual.stdout)
			}
			found := false
			for _, progress := range report.Diagnostics[0].Progress {
				fields := foldProgressFields(t, progress)
				if string(fields["operation"].Value) != `"Flow.worklist"` {
					continue
				}
				found = true
				valueJSON(t, fields["completed_count"], `"1"`)
				valueJSON(t, fields["pending_count"], `"2"`)
				var state map[string]runtimeValue
				if err := json.Unmarshal(fields["state"].Value, &state); err != nil {
					t.Fatal(err)
				}
				valueJSON(t, state["count"], `"1"`)
				valueJSON(t, state["deleted"], `"1"`)
				valueJSON(t, state["skipped"], `"0"`)
			}
			if !found {
				t.Fatalf("cache cancellation lost worklist progress: %s", actual.stdout)
			}
		})
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestMacCleanupCacheAllocationCounters(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	removed := fileFixture(t, fixture.writes, "a.cache", "one")
	opened := fileFixture(t, fixture.writes, "open.cache", "three")
	excluded := fileFixture(t, fixture.writes, "unsafe\n.cache", "seven!!")
	source := cleanupFlows(t, "CacheWalk") + fmt.Sprintf(`
flow AllocationFixture() -> Record{count: Int, eligible_bytes: Int, deleted: Int, deleted_bytes: Int, skipped: Int, error_count: Int}! {
    lock = Lock.File.new(Program.path())
    lease = commit lock
    environment = Map.new(Record.new(key: "PATH", value: %q), Record.new(key: "FIXTURE_CALLS", value: %q), Record.new(key: "FIXTURE_BEHAVIOR", value: "scoped"), Record.new(key: "FIXTURE_OPENED", value: %q), Record.new(key: "FIXTURE_CHANGED", value: ""))
    process = Process.Context.new(environment, File.Path.new(%q))
    context = Record.new(apply: true, home: File.Path.new(%q), uid: %d, process: process)
    directory = File.directory(File.Path.new(%q))
    state = CacheWalk(context, directory.device, Clock.now_unix_ns(), directory, CacheState(0, 0, 0, 0, 0, List.new()))
    return Record.new(count: state.count, eligible_bytes: state.eligible_bytes, deleted: state.deleted, deleted_bytes: state.deleted_bytes, skipped: state.skipped, error_count: List.length(state.errors))
}
return AllocationFixture()`, fixture.root, fixture.calls, opened, fixture.root, fixture.root, os.Getuid(), fixture.writes)
	scenario := macCleanupCachePolicyScenario(t, fixture, "allocated-counter-scope", 2, source, "", "", func(t *testing.T, actual result) {
		t.Helper()
		valueJSON(t, fileOutput(t, actual), `{"count":{"type":"Int","value":"3"},"eligible_bytes":{"type":"Int","value":"15360"},"deleted":{"type":"Int","value":"1"},"deleted_bytes":{"type":"Int","value":"3072"},"skipped":{"type":"Int","value":"2"},"error_count":{"type":"Int","value":"0"}}`)
		macCleanupPresent(t, removed, false)
		macCleanupPresent(t, opened, true)
		macCleanupPresent(t, excluded, true)
		calls, err := os.ReadFile(fixture.calls)
		if err != nil || string(calls) != "-F0n\n--\n"+removed+"\n"+opened+"\n" {
			t.Fatalf("allocation fixture probe scope: %q, %v", calls, err)
		}
	})
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestMacCleanupCacheScanOwnerInvariant(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	source := cleanupFlows(t, "CacheObserve") + fmt.Sprintf(`
flow ScanOwnerFixture() -> Bool! {
    directory = File.directory(File.Path.new(%q))
    scan = CacheObserve(%d, directory.device, directory)
    return Math.eq(scan.directory.owner_uid, %d)
}
return ScanOwnerFixture()`, fixture.writes, os.Getuid(), os.Getuid())
	scenario := macCleanupCachePolicyScenario(t, fixture, "returned-scan-owner", 3, source, "", "", func(t *testing.T, actual result) {
		t.Helper()
		valueJSON(t, fileOutput(t, actual), `true`)
		calls, err := os.ReadFile(fixture.calls)
		if err != nil || len(calls) != 0 {
			t.Fatalf("scan owner observation started a probe: %q, %v", calls, err)
		}
	})
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
}

func TestMacCleanupCanonicalCachePolicies(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	fileFixture(t, fixture.writes, "entry", "candidate")
	f033Directory(t, fixture.writes, "child")
	source := cleanupFlows(t, "CacheCandidate", "CacheChild") + fmt.Sprintf(`
flow CachePolicyFixture() -> Record{candidate: Bool, foreign_file: Bool, other_file_device: Bool, child: Bool, foreign_child: Bool, other_child_device: Bool, file_as_child: Bool, child_as_file: Bool}! {
    directory = File.directory(File.Path.new(%q))
    entry = File.Directory.entry(directory, "entry")
    child = File.Directory.entry(directory, "child")
    cutoff = Clock.now_unix_ns()
    return Record.new(candidate: CacheCandidate(entry.owner_uid, entry.device, cutoff, entry), foreign_file: CacheCandidate(Math.add(entry.owner_uid, 1), entry.device, cutoff, entry), other_file_device: CacheCandidate(entry.owner_uid, Math.add(entry.device, 1), cutoff, entry), child: CacheChild(child.owner_uid, child.device, child), foreign_child: CacheChild(Math.add(child.owner_uid, 1), child.device, child), other_child_device: CacheChild(child.owner_uid, Math.add(child.device, 1), child), file_as_child: CacheChild(entry.owner_uid, entry.device, entry), child_as_file: CacheCandidate(child.owner_uid, child.device, cutoff, child))
}
return CachePolicyFixture()`, fixture.writes)
	actual := runProgram(t, sourceFile(t, source), "run", "--format", "json", "--observe-tree-root", fixture.writes)
	valueJSON(t, fileOutput(t, actual), `{"candidate":{"type":"Bool","value":true},"foreign_file":{"type":"Bool","value":false},"other_file_device":{"type":"Bool","value":false},"child":{"type":"Bool","value":true},"foreign_child":{"type":"Bool","value":false},"other_child_device":{"type":"Bool","value":false},"file_as_child":{"type":"Bool","value":false},"child_as_file":{"type":"Bool","value":false}}`)
}
