package integration_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
)

func TestProcessCleanupPolicyDiagnostics(t *testing.T) {
	root, executable, launcher := processPolicyPaths(t)
	source := sourceFile(t, "return 1")
	artifact, built := buildForTest(t, source)
	if built.status != 0 {
		t.Fatalf("build policy fixture: %+v", built)
	}
	write := cleanupScope(t, executable, "root", filepath.Join(root, "cache"))
	for _, test := range []struct {
		name    string
		flags   []string
		message string
	}{
		{"missing-value", []string{"--process-write-root"}, "--process-write-root requires a value with run or serve"},
		{"unknown", []string{"--process-write-root", `{"executable":"x","root":"y","extra":"z"}`}, "--process-write-root: unknown Process scope field"},
		{"duplicate-field", []string{"--process-prune-root", `{"executable":"x","root":"y","root":"z"}`}, "--process-prune-root: duplicate Process scope field"},
		{"missing-field", []string{"--process-unix-endpoint", `{"executable":"x"}`}, "--process-unix-endpoint: Process scope requires executable and endpoint"},
		{"null-field", []string{"--process-write-root", `{"executable":"x","root":null}`}, "--process-write-root: Process scope field must contain text"},
		{"number-field", []string{"--process-write-root", `{"executable":"x","root":12}`}, "--process-write-root: Process scope field must contain text"},
		{"array", []string{"--process-write-root", `[]`}, "--process-write-root: expected one JSON object with executable and root"},
		{"trailing", []string{"--process-write-root", write + ` {}`}, "--process-write-root: Process scope must contain one JSON object"},
		{"malformed", []string{"--process-write-root", `{"executable":`}, "--process-write-root: malformed Process scope JSON"},
		{"unpaired-high-surrogate", []string{"--process-write-root", `{"executable":"x","root":"\ud800"}`}, "--process-write-root: malformed Process scope JSON"},
		{"unpaired-low-surrogate", []string{"--process-write-root", `{"executable":"x","root":"\udc00"}`}, "--process-write-root: malformed Process scope JSON"},
		{"ungranted", []string{"--process-write-root", cleanupScope(t, filepath.Join(root, "ungranted"), "root", root)}, "Process write root requires an exact granted executable"},
		{"relative", []string{"--process-write-root", cleanupScope(t, executable, "root", "relative")}, "Process write root requires a clean absolute NUL-free path"},
		{"duplicate-root", []string{"--process-write-root", write, "--process-write-root", write}, "duplicate Process write root"},
		{"overlap", []string{"--process-write-root", write, "--process-prune-root", cleanupScope(t, executable, "root", filepath.Join(root, "cache", "nested"))}, "Process mutation roots overlap"},
		{"image", []string{"--process-write-root", cleanupScope(t, executable, "root", executable)}, "Process mutation root contains a registered executable"},
		{"launcher", []string{"--process-prune-root", cleanupScope(t, executable, "root", launcher)}, "Process mutation root contains the launcher"},
		{"unreadable", []string{"--process-write-root", cleanupScope(t, executable, "root", filepath.Join(filepath.Dir(root), "other"))}, "Process mutation root requires a containing child read root"},
		{"endpoint-overlap", []string{"--process-write-root", write, "--process-unix-endpoint", cleanupScope(t, executable, "endpoint", filepath.Join(root, "cache", "socket"))}, "Process Unix endpoint overlaps a mutation root"},
	} {
		t.Run(test.name, func(t *testing.T) {
			flags := append(processPolicyFlags(t), test.flags...)
			reference := runExecutable(t, binary, append([]string{"run", source, "--format", "json"}, flags...)...)
			native := runExecutable(t, artifact, append([]string{"run", "--format", "json"}, flags...)...)
			for name, got := range map[string]result{"interpreter": reference, "native": native} {
				if got.status != 2 || got.stdout != "" {
					t.Fatalf("%s accepted invalid scope: %+v", name, got)
				}
				diagnostic := diagnostics(t, got.stderr).Diagnostics[0]
				if diagnostic.Code != "cli.invalid-invocation" || !strings.Contains(diagnostic.Message, test.message) {
					t.Fatalf("%s scope diagnostic: %+v; want %q", name, diagnostic, test.message)
				}
			}
		})
	}
}

func TestProcessCleanupPolicyCopiesScopes(t *testing.T) {
	root, executable, launcher := processPolicyPaths(t)
	writes := []adapter.ProcessPathScopeInput{{Executable: executable, Path: filepath.Join(root, "cache")}}
	policy, err := adapter.NewProcessPolicy(adapter.ProcessPolicyInput{Grants: []string{strings.Repeat("a", 64) + "," + executable}, Launcher: launcher, ReadRoots: []string{root}, Writes: writes})
	if err != nil {
		t.Fatal(err)
	}
	writes[0].Path = "changed"
	scope, exists := policy.Scope(executable)
	if !exists || scope.Kind() != adapter.ProcessCleanupScope {
		t.Fatal("missing cleanup scope")
	}
	paths := scope.WriteRoots()
	if len(paths) != 1 || paths[0] != filepath.Join(root, "cache") {
		t.Fatal("scope retained mutable input")
	}
	paths[0] = "changed"
	if scope.WriteRoots()[0] != filepath.Join(root, "cache") {
		t.Fatal("scope exposed mutable storage")
	}
	if _, exists := policy.Scope(filepath.Join(root, "ungranted")); exists {
		t.Fatal("ungranted executable acquired a scope")
	}
}
