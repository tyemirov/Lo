package integration_test

import (
	"fmt"

	"os"

	"testing"
)

func registryByteFixture(t *testing.T, path string, content []byte) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write(content)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("create byte fixture: write=%v close=%v", writeErr, closeErr)
	}
}

func registryRemoveSource(root, name string) string {
	return fmt.Sprintf(`flow Select(name: String, entry: File.Entry) -> Bool { return String.eq(name, entry.name) }; flow Main() -> File.RemoveReceipt! { entries = File.list(File.Path.new(%q)); selected = List.filter(entries, %q, Select); target = File.Remove.new(selected); receipt = commit target; return receipt }; return Main()`, root, name)
}

// Both unresolved registries intentionally remain until test-process exit.

// No recovery API can prove cleanup after this trusted-host violation.
// The unresolved registry intentionally survives until this test process exits.

// Replace the trusted namespace before the operation, not concurrently with it.
