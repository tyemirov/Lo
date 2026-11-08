package integration_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestMacCleanupCanonicalFilePolicies(t *testing.T) {
	runLLVMRuntimeScenarios(t, macCleanupPolicyScenarios(t))
}

func TestMacCleanupLinuxCanonicalFilePolicies(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", macCleanupPolicyScenarios(t))
}

const macCleanupPolicyWrites = `first=File.Write.new(File.Path.new("a"),Bytes.new("first"));commit first;second=File.Write.new(File.Path.new("b"),Bytes.new("second"));commit second;directory=File.directory(File.Path.new("."));`
const macCleanupPolicySelection = `flow PolicyEntry(entry:File.Entry)->Bool{return String.eq(entry.name,"a")};`

func macCleanupPolicyScenarios(t *testing.T) []llvmRuntimeScenario {
	t.Helper()
	flows := cleanupFlows(t, "StaleCandidate", "StaleGuard", "GatewayGuard") + macCleanupPolicySelection
	candidate := flows + `flow Main()->Bool!{` + macCleanupPolicyWrites + `entries=List.filter(File.list(File.Path.new(".")),PolicyEntry);entry=List.at(entries,0);return StaleCandidate(1001,10000000000,List.new(Glob.new("*")),entry)};return Main()`
	guards := flows + `flow Main()->Record{stale:Bool,gateway:Bool}!{` + macCleanupPolicyWrites + `before=List.at(List.filter(File.list(File.Path.new(".")),PolicyEntry),0);after=List.at(List.filter(File.list(File.Path.new(".")),PolicyEntry),0);return Record.new(stale:StaleGuard(1001,10000000000,List.new(Glob.new("*")),before,after),gateway:GatewayGuard(before,after))};return Main()`
	var scenarios []llvmRuntimeScenario
	for _, test := range []struct {
		name, change string
		eligible     bool
	}{
		{"old-by-one-nanosecond", "", true},
		{"different-owner", "metadata->owner_uid=1002;", false},
		{"multiple-links", "metadata->link_count=2;", false},
		{"access-at-cutoff", "metadata->accessed_sec=10;metadata->accessed_nsec=0;", false},
		{"access-after-cutoff", "metadata->accessed_sec=10;metadata->accessed_nsec=1;", false},
		{"modification-at-cutoff", "metadata->modified_sec=10;metadata->modified_nsec=0;", false},
		{"modification-after-cutoff", "metadata->modified_sec=10;metadata->modified_nsec=1;", false},
		{"change-at-cutoff", "metadata->changed_sec=10;metadata->changed_nsec=0;", false},
		{"change-after-cutoff", "metadata->changed_sec=10;metadata->changed_nsec=1;", false},
	} {
		scenarios = append(scenarios, macCleanupMetadataScenario(test.name, candidate, test.change, fmt.Sprint(test.eligible)))
	}
	for _, test := range []struct {
		name, change   string
		stale, gateway bool
	}{
		{"unchanged-guards", "", true, true},
		{"owner-revalidation", "metadata->owner_uid=1002;", false, true},
		{"link-revalidation", "metadata->link_count=2;", false, true},
		{"access-revalidation", "metadata->accessed_nsec=999999998;", false, true},
		{"change-revalidation", "metadata->changed_nsec=999999998;", false, true},
		{"modification-revalidation", "metadata->modified_nsec=999999998;", false, false},
		{"size-revalidation", "++metadata->size_bytes;", false, false},
	} {
		change := "if(discovery[index]>0){" + test.change + "}"
		want := fmt.Sprintf(`{"stale":{"type":"Bool","value":%t},"gateway":{"type":"Bool","value":%t}}`, test.stale, test.gateway)
		scenarios = append(scenarios, macCleanupMetadataScenario(test.name, guards, change, want))
	}
	return scenarios
}

func macCleanupMetadataScenario(name, program, change, want string) llvmRuntimeScenario {
	return llvmRuntimeScenario{
		name: name, source: program, harness: "testdata/native-runtime/file-metadata-observer.c",
		transform: func(t *testing.T, module []byte) []byte {
			t.Helper()
			const entry = "define void @lo_entry("
			if strings.Count(string(module), entry) != 1 {
				t.Fatal("missing unique policy entry for metadata observer installation")
			}
			return []byte(strings.Replace(string(module), entry, "define void @lo_qualified_entry(", 1))
		},
		harnessTransform: func(t *testing.T, data []byte) []byte {
			t.Helper()
			const original = `    metadata->owner_uid = 1001;
    metadata->accessed_sec = -1;
    metadata->accessed_nsec = 123456789;
    metadata->changed_sec = -1;
    metadata->changed_nsec = 0;`
			if strings.Count(string(data), original) != 1 {
				t.Fatal("metadata observer fixture no longer has its unique controlled values")
			}
			injected := `metadata->owner_uid=1001;metadata->link_count=1;
metadata->accessed_sec=9;metadata->accessed_nsec=999999999;
metadata->modified_sec=9;metadata->modified_nsec=999999999;
metadata->changed_sec=9;metadata->changed_nsec=999999999;` + change
			return []byte(strings.Replace(string(data), original, injected, 1))
		},
		verify: func(t *testing.T, actual result) {
			t.Helper()
			valueJSON(t, fileOutput(t, actual), want)
		},
	}
}
