package integration_test

import (
	"path/filepath"
	"strings"

	"testing"
)

// These paths are inert descriptions; the tests create no executable, launcher,
// directory or data fixture, and parsing must not require any of them to exist.
func processPolicyPaths(t *testing.T) (root, command, launcher string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "_scratch", "adapter-implementation-x921", "process-policy-fixture"))
	if err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(root, "command,with spaces"), filepath.Join(root, "launcher,with spaces")
}

func processPolicyFlags(t *testing.T) []string {
	t.Helper()
	root, command, launcher := processPolicyPaths(t)
	return []string{"--process-allow", strings.Repeat("aB", 32) + "," + command, "--process-launcher", launcher, "--process-read-root", root}
}
