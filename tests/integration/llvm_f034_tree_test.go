package integration_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func TestLLVMF034TreeLifecycle(t *testing.T) {
	runLLVMRuntimeScenarios(t, f034NativeScenarios(runtime.GOOS))
}

func TestLLVMLinuxF034TreeLifecycle(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", f034NativeScenarios("linux"))
}

func f034NativeScenarios(targetOS string) []llvmRuntimeScenario {
	var scenarios []llvmRuntimeScenario
	for number, fixture := range []struct {
		name, expected, stage string
		completed, index      int
		relative              string
	}{
		{"postorder-all-guards-before-first-unlink", `"completed_count":{"type":"Int","value":"7"}`, "", 7, -2, ""},
		{"unlink-failure-prefix", "File.RemoveFailed", "remove", 1, 3, "a-dir/b"},
		{"cancel-after-unlink", "runtime.cancelled", "remove", 1, -2, ""},
		{"close-after-root-unlink", "runtime.host-failure", "finalize", 7, -1, ""},
		{"recoverable-unlink-and-close", "File.RemoveFailed", "remove", 1, 3, "a-dir/b"},
		{"parent-owner-after-own-unlink", "File.EntryChanged", "postcheck", 1, -2, ""},
		{"unexpected-name-after-own-unlink", "File.EntryChanged", "postcheck", 1, -2, ""},
		{"alias-link-count-after-own-unlink", "File.EntryChanged", "postcheck", 1, -2, ""},
		{"allocation-preflight-change", "File.EntryChanged", "preflight", 0, -2, ""},
		{"file-access-preflight-change", "File.EntryChanged", "preflight", 0, -2, ""},
		{"directory-access-change-is-observation-safe", `"completed_count":{"type":"Int","value":"7"}`, "", 7, -2, ""},
		{"alias-size-after-own-unlink", "File.EntryChanged", "postcheck", 1, -2, ""},
		{"alias-allocation-after-own-unlink", "File.EntryChanged", "postcheck", 1, -2, ""},
		{"alias-modified-after-own-unlink", "File.EntryChanged", "postcheck", 1, -2, ""},
		{"alias-access-after-own-unlink", "File.EntryChanged", "postcheck", 1, -2, ""},
		{"alias-owner-after-own-unlink", "File.EntryChanged", "postcheck", 1, -2, ""},
		{"snapshot-cancellation", "runtime.cancelled", "", 0, -2, ""},
		{"snapshot-io-failure", "File.ListFailed", "", 0, -2, ""},
		{"snapshot-vanished-name", "File.ListFailed", "", 0, -2, ""},
		{"snapshot-absent-root", "File.DirectoryAbsent", "", 0, -2, ""},
		{"snapshot-replaced-root", "File.DirectoryChanged", "", 0, -2, ""},
		{"foreign-target", "runtime.capability-denied", "", 0, -2, ""},
		{"released-target", "runtime.capability-denied", "", 0, -2, ""},
		{"diagnostic-tree-lacks-authority", "File.InvalidSelection", "", 0, -2, ""},
		{"last-row-preflight-rejection", "File.EntryChanged", "preflight", 0, 6, "c-dir/a"},
		{"cancel-after-root-unlink", "runtime.cancelled", "remove", 7, -2, ""},
		{"invalid-snapshot-provider", "runtime.host-failure", "", 0, -2, ""},
		{"wide-bounded-descriptors", `"value":"161"`, "", 0, -2, ""},
		{"pending-ancestor-bounded-descriptors", `"value":"321"`, "", 0, -2, ""},
		{"protected-lock-preflight", "runtime.capability-denied", "preflight", 0, 4, "b-file"},
		{"total-row-budget", "runtime.limit", "", 0, -2, ""},
		{"eligibility-work-budget", "runtime.limit", "preflight", 0, -2, ""},
		{"cross-parent-hardlink-aliases", `"completed_count":{"type":"Int","value":"7"}`, "", 7, -2, ""},
		{"wide-bounded-removal", `"completed_count":{"type":"Int","value":"161"}`, "", 161, -2, ""},
		{"pending-ancestor-bounded-removal", `"completed_count":{"type":"Int","value":"321"}`, "", 321, -2, ""},
		{"escaping-tree-lease", `"completed_count":{"type":"Int","value":"7"}`, "", 7, -2, ""},
		{"escaping-target-lease", `"completed_count":{"type":"Int","value":"7"}`, "", 7, -2, ""},
	} {
		source := f034RemoveSource("parent", "", "")
		if number == 22 {
			source = f034Predicates + `flow Capture() -> Bool! { parent = File.directory(File.Path.new("parent")); root = File.Directory.entry(parent, "tree"); tree = File.Tree.snapshot(parent, root, Descend); target = File.Tree.Remove.checked(tree, Eligible); return true }; ` + f034Program("parent", "File.RemoveReceipt", "captured = Capture()", `target = File.Tree.Remove.checked(tree, Eligible); receipt = commit target; return receipt`)
		} else if number == 23 {
			source = f034Predicates + f034Program("parent", "File.Tree.Remove", "", `wrapped = Record.new(tree: tree); error = Error.new("Fixture.Snapshot", "snapshot", wrapped); diagnostic = error.input; return File.Tree.Remove.checked(wrapped.tree, Eligible)`)
		} else if number == 24 {
			source = strings.Replace(source, `return true }
flow Main`, `return Match.new(String.eq(row.relative, "c-dir/a"), true: false, _: true) }
flow Main`, 1)
		} else if number == 27 || number == 28 {
			source = f034Predicates + f034Program("parent", "Int", "", "return List.length(tree.entries)")
		} else if number == 29 {
			source = f034RemoveSource("parent", `invocation = Host.Invocation.current(); lock_name = List.at(invocation.arguments, 0); lock = Lock.File.new(File.Path.new(lock_name)); lease = commit lock`, "")
		} else if number == 31 {
			source = strings.Replace(source, `flow Eligible(row: Record{relative: String, entry: File.Entry}) -> Bool { return true }`, `flow Count(index: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: index, next_state: Math.add(index, 1)) }
flow Eligible(row: Record{relative: String, entry: File.Entry}) -> Bool { items = List.unfold(0, 10000, Count); return Math.eq(List.length(items), 10000) }`, 1)
		} else if number == 35 {
			source = f034EscapingSource("parent", "tree")
		} else if number == 36 {
			source = f034EscapingSource("parent", "target")
		}
		scenario := llvmRuntimeScenario{name: fixture.name, source: source, number: number, harness: "testdata/native-runtime/f034-tree.c", transform: f034NativeTransform}
		scenario.verify = func(t *testing.T, got result) {
			if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, fixture.expected) {
				t.Fatalf("F034 %s: %+v; expected %s", fixture.name, got, fixture.expected)
			}
			if fixture.stage != "" {
				kind := ""
				if strings.HasPrefix(fixture.expected, "File.") {
					kind = fixture.expected
				}
				fields := f034Progress(t, got.stdout, fixture.completed, 7, fixture.index, fixture.relative, fixture.stage, kind)
				if fixture.completed == 0 {
					valueJSON(t, fields["completed_bytes"], `"0"`)
				} else if fixture.completed == 1 {
					valueJSON(t, fields["completed_bytes"], `"3"`)
				}
			}
			if number == 4 && !strings.Contains(got.stdout, `"cleanup_errors"`) {
				t.Fatalf("recoverable primary lost cleanup failure: %s", got.stdout)
			}
			if number == 34 && targetOS == "linux" {
				const marker = "F034 descriptor measurement: "
				_, evidence, found := strings.Cut(got.stdout, marker)
				if !found || strings.Count(got.stdout, marker) != 1 || len(strings.Fields(evidence)) != 7 {
					t.Fatalf("missing descriptor measurement evidence: %s", got.stdout)
				}
				var observations, measurements, entries, probes, highFD, matches, sparseChecks uint64
				count, err := fmt.Sscanf(strings.TrimSpace(evidence), "observations=%d measurements=%d entries=%d reference_probes=%d high_fd=%d reference_matches=%d sparse_checks=%d", &observations, &measurements, &entries, &probes, &highFD, &matches, &sparseChecks)
				if err != nil || count != 7 || observations < 321 || measurements != observations+5 || entries < measurements || entries >= 512*measurements || probes != 1024 || highFD < 384 || highFD >= 512 || matches != 2 || sparseChecks != 2 {
					t.Fatalf("descriptor measurement contract: %s (%v)", evidence, err)
				}
				t.Logf("descriptor measurement: %s", strings.TrimSpace(evidence))
			}
		}
		scenarios = append(scenarios, scenario)
	}
	return scenarios
}

func f034NativeTransform(t *testing.T, module []byte) []byte {
	t.Helper()
	text := strings.Replace(string(module), "define void @lo_entry(", "define void @lo_f034_entry(", 1)
	// Descend is the first declared flow. Eligible is second, except the work-budget case.
	if !strings.Contains(text, "define void @lo_flow_1(") {
		t.Fatal("missing eligible callback definition")
	}
	text = strings.Replace(text, "define void @lo_flow_1(", "define void @lo_f034_guard(", 1)
	text += "\ndeclare void @lo_flow_1(ptr, ptr, i64, i64, ptr)\n"
	for _, replacement := range [][2]string{
		{"lo_op_File_2ETree_2Esnapshot", "lo_f034_snapshot"},
		{"lo_op_File_2ETree_2ERemove_2Echecked", "lo_f034_checked"},
		{"lo_commit", "lo_f034_commit"},
		{"lo_project", "lo_f034_project"},
	} {
		text = strings.ReplaceAll(text, "@"+replacement[0]+"(", "@"+replacement[1]+"(")
	}
	return []byte(text)
}
