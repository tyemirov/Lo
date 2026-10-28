package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestF025PublicContracts(t *testing.T) {
	t.Run("invocation", func(t *testing.T) {
		source := `flow Main() -> Int! { initial = Host.Invocation.current(); return List.length(initial.arguments) }; return Main()`
		valueJSON(t, fileOutput(t, run(t, "run", sourceFile(t, source), "--format", "json", "--", "literal", "--flag", "")), `"3"`)
	})
	t.Run("metadata", func(t *testing.T) {
		root := fileFixtureRoot(t)
		fileFixture(t, root, "entry", "payload")
		source := fmt.Sprintf(`flow Entry(entry: File.Entry) -> Int { return entry.owner_uid }; flow Main() -> List<Int>! { entries = File.list(File.Path.new(%q)); return List.map(entries, Entry) }; return Main()`, root)
		valueJSON(t, fileOutput(t, run(t, "run", sourceFile(t, source), "--list-root", root, "--format", "json")), fmt.Sprintf(`[{"type":"Int","value":"%d"}]`, os.Getuid()))
	})
	t.Run("directory-and-scan", func(t *testing.T) {
		root := fileFixtureRoot(t)
		fileFixture(t, root, "entry", "payload")
		source := fmt.Sprintf(`flow Main() -> Int! { directory = File.directory(File.Path.new(%q)); scan = File.scan(directory.path); return Math.add(List.length(scan.entries), List.length(scan.vanished_names)) }; return Main()`, root)
		valueJSON(t, fileOutput(t, run(t, "run", sourceFile(t, source), "--list-root", root, "--format", "json")), `"1"`)
	})
	t.Run("checked-removal", func(t *testing.T) {
		root := fileFixtureRoot(t)
		fileFixture(t, root, "entry", "payload")
		source := fmt.Sprintf(`flow Eligible(original: File.Entry, current: File.Entry) -> Bool { return Math.eq(original.owner_uid, current.owner_uid) }; flow Main() -> Int! { entries = File.list(File.Path.new(%q)); target = File.Remove.checked(entries, Eligible); receipt = commit target; return receipt.completed_count }; return Main()`, root)
		valueJSON(t, fileOutput(t, run(t, "run", sourceFile(t, source), "--list-root", root, "--remove-root", root, "--format", "json")), `"1"`)
		if _, err := os.Lstat(filepath.Join(root, "entry")); !os.IsNotExist(err) {
			t.Fatalf("checked removal did not remove the selected entry: %v", err)
		}
	})
}
