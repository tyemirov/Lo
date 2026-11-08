package integration_test

import (
	"strings"
	"testing"
)

func TestLLVMF033DescendantObservations(t *testing.T) {
	runLLVMRuntimeScenarios(t, f033NativeScenarios())
}

func TestLLVMLinuxF033DescendantObservations(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", f033NativeScenarios())
}

func f033NativeScenarios() []llvmRuntimeScenario {
	const root = "ancestor/root"
	refresh := f033Program(root, "File.Directory", `return File.Directory.refresh(directory)`)
	child := f033Program(root, "File.Directory", `entry = File.Directory.entry(directory, "child"); return File.Directory.child(directory, entry)`)
	scan := f033Program(root, "Int", `scan = File.Directory.scan(directory); return List.length(scan.vanished_names)`)
	remove := f033Program(root, "File.RemoveReceipt", `entry = File.Directory.entry(directory, "child"); child = File.Directory.child(directory, entry); nested = File.Directory.entry(child, "inner"); inner = File.Directory.child(child, nested); scan = File.Directory.scan(inner); target = File.Remove.new(scan.entries); receipt = commit target; return receipt`)
	var scenarios []llvmRuntimeScenario
	for index, test := range []struct{ name, source, expected string }{
		{"replaced-root-ancestor-same-root", refresh, "File.DirectoryChanged"},
		{"replaced-child-ancestor-same-child", f033Program(root, "File.Scan", `entry = File.Directory.entry(directory, "child"); child = File.Directory.child(directory, entry); nested = File.Directory.entry(child, "inner"); inner = File.Directory.child(child, nested); return File.Directory.scan(inner)`), "File.DirectoryChanged"},
		{"replaced-child", child, "File.DirectoryChanged"},
		{"child-replaced-with-symlink", child, "File.DirectoryChanged"},
		{"foreign-session", refresh, "runtime.capability-denied"},
		{"released-observation", `flow Capture() -> Int! { observed = File.directory(File.Path.new("ancestor/root")); return observed.inode }; flow Main() -> File.Directory! { captured = Capture(); directory = File.directory(File.Path.new("ancestor/root")); return File.Directory.refresh(directory) }; return Main()`, "runtime.capability-denied"},
		{"snapshot-has-no-authority", f033Program(root, "File.Directory", `wrapped = Record.new(directory: directory); error = Error.new("Fixture.Snapshot", "snapshot", wrapped); snapshot = error.input; return File.Directory.refresh(wrapped.directory)`), "runtime.capability-denied"},
		{"retained-scan-vanished", scan, `"value":"1"`},
		{"retained-scan-io-failure", scan, "File.ListFailed"},
		{"fresh-directory-metadata", f033Program(root, "Int", `fresh = File.Directory.refresh(directory); return Math.sub(fresh.owner_uid, directory.owner_uid)`), `"value":"1"`},
		{"fresh-entry-metadata", f033Program(root, "Int", `entry = File.Directory.entry(directory, "entry"); observations = File.Entry.observe(entry); fresh = List.at(observations, 0); return fresh.size_bytes`), `"value":"99"`},
		{"cancelled-retained-observation", refresh, "runtime.cancelled"},
		{"retained-close-failure", refresh, "runtime.host-failure"},
		{"wide-handle-bound", f033WalkSource(root), `"value":"161"`},
		{"pending-ancestor-handle-bound", f033WalkSource(root), `"value":"321"`},
		{"retained-entry-io-failure", f033Program(root, "File.Entry", `return File.Directory.entry(directory, "child")`), "File.ListFailed"},
		{"retained-refresh-io-failure", refresh, "File.DirectoryFailed"},
		{"vanished-child-directory", child, "File.DirectoryAbsent"},
		{"ancestor-replaced-during-refresh", refresh, "File.DirectoryChanged"},
		{"exact-root-ancestor-replaced", refresh, "File.DirectoryChanged"},
		{"retained-observe-io-failure", f033Program(root, "List<File.Entry>", `entry = File.Directory.entry(directory, "child"); return File.Entry.observe(entry)`), "File.ListFailed"},
		{"replaced-parent-before-observe", f033Program(root, "List<File.Entry>", `entry = File.Directory.entry(directory, "child"); return File.Entry.observe(entry)`), "File.DirectoryChanged"},
		{"child-open-io-failure", child, "File.DirectoryFailed"},
		{"child-replaced-at-open", child, "File.DirectoryChanged"},
		{"retained-observe-parent-open-io", f033Program(root, "List<File.Entry>", `entry = File.Directory.entry(directory, "child"); return File.Entry.observe(entry)`), "File.ListFailed"},
		{"removal-parent-replaced-after-preflight", remove, "File.EntryChanged"},
		{"removal-parent-open-io", remove, "File.RemoveFailed"},
		{"refresh-failure-after-ancestor-replacement", refresh, "File.DirectoryChanged"},
		{"scan-failure-after-ancestor-replacement", scan, "File.DirectoryChanged"},
		{"child-failure-after-ancestor-replacement", child, "File.DirectoryChanged"},
		{"invalid-provider-after-ancestor-replacement", refresh, "runtime.host-failure"},
		{"cancelled-after-ancestor-replacement", refresh, "runtime.cancelled"},
		{"scan-replacement-restored-by-next-callback", scan, "File.DirectoryChanged"},
		{"scan-directory-replacement-restored-by-entry", scan, "File.DirectoryChanged"},
		{"scan-open-failure-retains-close-failure", scan, "File.ListFailed"},
	} {
		scenarios = append(scenarios, llvmRuntimeScenario{name: test.name, source: test.source, number: index, harness: "testdata/native-runtime/f033-descendants.c", transform: func(t *testing.T, module []byte) []byte {
			t.Helper()
			text := strings.Replace(string(module), "define void @lo_entry(", "define void @lo_f033_entry(", 1)
			for _, replacement := range [][2]string{
				{"lo_op_File_2Edirectory", "lo_f033_directory"},
				{"lo_op_File_2EDirectory_2Escan", "lo_f033_scan"},
				{"lo_op_File_2EDirectory_2Erefresh", "lo_f033_refresh"},
				{"lo_op_File_2EDirectory_2Echild", "lo_f033_child"},
				{"lo_op_File_2EDirectory_2Eentry", "lo_f033_named_entry"},
				{"lo_op_File_2EEntry_2Eobserve", "lo_f033_observe"},
				{"lo_project", "lo_f033_project"},
			} {
				text = strings.ReplaceAll(text, "@"+replacement[0]+"(", "@"+replacement[1]+"(")
			}
			return []byte(text)
		}, verify: func(t *testing.T, actual result) {
			if actual.status != 0 || actual.stderr != "" || !strings.Contains(actual.stdout, test.expected) {
				t.Fatalf("F033 %s: %+v; expected %s", test.name, actual, test.expected)
			}
			if (index == 25 || index == 26) && !strings.Contains(actual.stdout, `"completed_count":{"type":"Int","value":"0"}`) {
				t.Fatalf("F033 removal lost confirmed progress: %s", actual.stdout)
			}
			if index == 34 && !strings.Contains(actual.stdout, `"cleanup_errors"`) {
				t.Fatalf("F033 scan lost owned-descriptor cleanup error: %s", actual.stdout)
			}
		}})
	}
	return scenarios
}
