package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func qualificationFixture(t *testing.T, name string) string {
	t.Helper()
	root := os.Getenv("LO_TEST_PREBUILT_FIXTURES")
	if root == "" {
		return ""
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(repository, root)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatal("prebuilt qualification fixtures leave repository")
	}
	manifest, err := os.ReadFile(filepath.Join(root, "fixtures.json"))
	if err != nil {
		t.Fatal(err)
	}
	var identities map[string]struct{ Path, SHA256 string }
	decoder := json.NewDecoder(strings.NewReader(string(manifest)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&identities); err != nil {
		t.Fatal(err)
	}
	identity, exists := identities[name]
	if !exists || !filepath.IsLocal(identity.Path) {
		t.Fatalf("missing prebuilt qualification fixture %s", name)
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, identity.Path))
	if err != nil {
		t.Fatal(err)
	}
	relative, err = filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatal("prebuilt qualification fixture leaves input directory")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != identity.SHA256 {
		t.Fatalf("prebuilt qualification fixture digest differs: %s", name)
	}
	return path
}
