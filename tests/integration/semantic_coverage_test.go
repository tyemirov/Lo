package integration_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
)

func TestSemanticCoverageRunIdentity(t *testing.T) {
	var paths []string
	for _, directory := range []string{"cmd/lo", "internal", "tests/integration", "tools/semanticcoverage", "runtime", "tools/metalbuild", "tools/nativeruntime", "tools/lodistribution", "tools/loqualification", "testdata", "examples", "docs/examples"} {
		err := filepath.WalkDir(filepath.Join(repository, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if !slices.Contains([]string{".go", ".py", ".c", ".h", ".m", ".mm", ".metal", ".lo", ".json", ".txt", ".rs", ".toml", ".lock"}, filepath.Ext(path)) {
				return nil
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(repository, resolved)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return fmt.Errorf("source outside repository: %s", path)
			}
			paths = append(paths, path)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"go.mod", "go.sum", "Makefile", "docs/evidence/training-reference-results.json"} {
		paths = append(paths, filepath.Join(repository, filepath.FromSlash(name)))
	}
	slices.Sort(paths)
	hash := sha256.New()
	for _, path := range paths {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		relativeResolved, err := filepath.Rel(repository, resolved)
		if err != nil || relativeResolved == ".." || strings.HasPrefix(relativeResolved, ".."+string(filepath.Separator)) {
			t.Fatalf("source outside repository: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		relative, err := filepath.Rel(repository, path)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(hash, "%s\x00%x\n", filepath.ToSlash(relative), sha256.Sum256(data))
	}
	matrix := semanticMatrixBytes(t)
	compilerData, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	compilerPath, err := filepath.Rel(repository, binary)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{
		"implementation": "llvm-native", "target": runtime.GOOS + "/" + runtime.GOARCH,
		"profile": "software", "compiler": filepath.ToSlash(compilerPath),
		"compiler_sha256": fmt.Sprintf("%x", sha256.Sum256(compilerData)),
		"source_sha256":   fmt.Sprintf("%x", hash.Sum(nil)), "matrix_sha256": fmt.Sprintf("%x", sha256.Sum256(matrix)),
	}
	manifest := os.Getenv("LO_LLVM_TOOLCHAIN")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(repository, manifest)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatalf("toolchain outside repository: %s", manifest)
	}
	fields["toolchain"] = filepath.ToSlash(relative)
	fields["toolchain_sha256"] = fmt.Sprintf("%x", sha256.Sum256(data))
	type runtimeIdentity struct{ Path, SHA256 string }
	var distribution struct {
		ABI     string
		Targets map[string]struct{ Runtime runtimeIdentity }
		Metal   *struct{ Runtime runtimeIdentity }
	}
	if err := json.Unmarshal(data, &distribution); err != nil {
		t.Fatal(err)
	}
	if distribution.ABI != "lo-native-3" {
		t.Fatalf("runtime ABI mismatch: %s", distribution.ABI)
	}
	fields["abi"] = distribution.ABI
	verifyRuntime := func(selected runtimeIdentity) string {
		t.Helper()
		if !filepath.IsLocal(selected.Path) {
			t.Fatalf("runtime path must be local: %s", selected.Path)
		}
		path, err := filepath.EvalSymlinks(filepath.Join(filepath.Dir(manifest), selected.Path))
		if err != nil {
			t.Fatal(err)
		}
		relative, err := filepath.Rel(repository, path)
		if err != nil || !filepath.IsLocal(relative) {
			t.Fatalf("runtime outside repository: %s", path)
		}
		contents, err := os.ReadFile(path)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(contents)) != selected.SHA256 {
			t.Fatalf("runtime identity mismatch: %v", err)
		}
		return selected.SHA256
	}
	fields["runtime_sha256"] = verifyRuntime(distribution.Targets[fields["target"]].Runtime)
	if fields["target"] == "darwin/arm64" && distribution.Metal != nil {
		fields["metal_runtime_sha256"] = verifyRuntime(distribution.Metal.Runtime)
	}
	identity, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("LO_SEMANTIC_IDENTITY=%s", identity)
}

type semanticOperation struct {
	Operation             string   `json:"operation"`
	Capability            string   `json:"capability"`
	Errors                []string `json:"errors"`
	ActivationErrors      []string `json:"activation_errors"`
	NegativeApplicability string   `json:"negative_applicability"`
}
type semanticContract struct {
	ID        string   `json:"id"`
	Operation string   `json:"operation"`
	Axis      string   `json:"axis"`
	Boundary  string   `json:"boundary"`
	Targets   []string `json:"targets"`
	Profile   string   `json:"profile"`
	Tests     []string `json:"tests"`
}
type semanticMatrix struct {
	Operations []semanticOperation                                     `json:"operations"`
	Contracts  []semanticContract                                      `json:"contracts"`
	Gaps       []struct{ ID, Scope, Reason, Owner, Acceptance string } `json:"gaps"`
}

func semanticMatrixBytes(t *testing.T) []byte {
	t.Helper()
	path := os.Getenv("LO_SEMANTIC_MATRIX")
	if path == "" {
		path = filepath.Join("testdata", "semantic-migration", "matrix.json")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(repository, path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve semantic matrix %s: %v", path, err)
	}
	relative, err := filepath.Rel(repository, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("semantic matrix outside repository: %s", path)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		t.Fatalf("read semantic matrix %s: %v", path, err)
	}
	return data
}

func readSemanticMatrix(t *testing.T) semanticMatrix {
	t.Helper()
	data := semanticMatrixBytes(t)
	var matrix semanticMatrix
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&matrix); err != nil {
		t.Fatal(err)
	}
	return matrix
}

func TestSemanticCoverageInventory(t *testing.T) {
	matrix := readSemanticMatrix(t)
	files, err := filepath.Glob(filepath.Join(repository, "tests", "integration", "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]bool{}
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && strings.HasPrefix(function.Name.Name, "Test") {
				tests[function.Name.Name] = true
			}
		}
	}
	operations := map[string]catalog.Operation{}
	for _, operation := range catalog.All() {
		operations[operation.Name()] = operation
	}
	metadata := map[string]semanticOperation{}
	for _, row := range matrix.Operations {
		op, exists := operations[row.Operation]
		if !exists || metadata[row.Operation].Operation != "" {
			t.Errorf("unknown or repeated operation %q", row.Operation)
			continue
		}
		metadata[row.Operation] = row
		if row.Capability != op.Capability() || !slices.Equal(row.Errors, op.ErrorKinds()) || !slices.Equal(row.ActivationErrors, op.ActivationKinds()) {
			t.Errorf("%s: catalog metadata changed", row.Operation)
		}
		if !slices.Contains([]string{"catalog_failure_or_activation", "verifier_and_callback", "verifier_only_typed_total", "verifier_only_canonical_state_constructor"}, row.NegativeApplicability) {
			t.Errorf("%s: invalid negative applicability", row.Operation)
		}
		if row.NegativeApplicability == "verifier_only_canonical_state_constructor" && row.Operation != catalog.SGDStateNew {
			t.Errorf("%s: unreviewed constructor exemption", row.Operation)
		}
	}
	for name := range operations {
		if metadata[name].Operation == "" {
			t.Errorf("missing operation %s", name)
		}
	}
	ids, compilerCLI := map[string]bool{}, map[string]bool{}
	axes := map[string]map[string]bool{}
	identity := regexp.MustCompile(`^Test[A-Za-z0-9_]+(?:/[^\n]+)?$`)
	for _, row := range matrix.Contracts {
		if row.ID == "" || ids[row.ID] {
			t.Errorf("duplicate/empty contract %q", row.ID)
		}
		ids[row.ID] = true
		if !slices.Contains([]string{"compiler-command", "program-command", "native-abi"}, row.Boundary) {
			t.Errorf("%s: invalid boundary", row.ID)
		}
		if !slices.Contains([]string{"frontend", "software", "controlled-metal", "metal-artifact"}, row.Profile) {
			t.Errorf("%s: invalid profile", row.ID)
		}
		if (row.Profile == "frontend") != (row.Boundary == "compiler-command") || row.Profile == "controlled-metal" && row.Boundary != "native-abi" {
			t.Errorf("%s: boundary/profile mismatch", row.ID)
		}
		if row.Profile == "metal-artifact" && !slices.Equal(row.Targets, []string{"darwin/arm64"}) {
			t.Errorf("%s: invalid Metal artifact target", row.ID)
		}
		seenTargets := map[string]bool{}
		if len(row.Targets) == 0 {
			t.Errorf("%s: missing targets", row.ID)
		}
		for _, target := range row.Targets {
			if !slices.Contains([]string{"darwin/arm64", "linux/arm64"}, target) || seenTargets[target] {
				t.Errorf("%s: unknown/duplicate target %s", row.ID, target)
			}
			seenTargets[target] = true
		}
		seenRefs := map[string]bool{}
		if len(row.Tests) == 0 {
			t.Errorf("%s: missing test reference", row.ID)
		}
		for _, ref := range row.Tests {
			name, _, _ := strings.Cut(ref, "/")
			if !identity.MatchString(ref) || !tests[name] {
				t.Errorf("%s: absent/invalid test %s", row.ID, ref)
			}
			if seenRefs[ref] {
				t.Errorf("%s: duplicate test reference %s", row.ID, ref)
			}
			seenRefs[ref] = true
		}
		if row.Operation != "" {
			if metadata[row.Operation].Operation == "" {
				t.Errorf("%s: unknown operation %s", row.ID, row.Operation)
			}
			if !slices.Contains([]string{"positive", "negative", "runtime-negative"}, row.Axis) {
				t.Errorf("%s: invalid operation axis", row.ID)
			}
			if axes[row.Operation] == nil {
				axes[row.Operation] = map[string]bool{}
			}
			axes[row.Operation][row.Axis] = true
		} else if row.Axis != "contract" {
			t.Errorf("%s: missing operation for axis", row.ID)
		}
		if row.Boundary == "compiler-command" {
			compilerCLI[row.ID] = true
		}
	}
	for name, row := range metadata {
		for _, axis := range []string{"positive", "negative"} {
			if !axes[name][axis] {
				t.Errorf("%s: missing %s contract", name, axis)
			}
		}
		if len(row.Errors)+len(row.ActivationErrors) > 0 && row.NegativeApplicability != "verifier_only_canonical_state_constructor" && !axes[name]["runtime-negative"] {
			t.Errorf("%s: missing runtime-negative contract", name)
		}
	}
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repository, "internal", "compilercli", "command.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, specification := range group.Specs {
			value := specification.(*ast.ValueSpec)
			for index, name := range value.Names {
				if !strings.HasSuffix(name.Name, "Command") && !strings.HasSuffix(name.Name, "Option") {
					continue
				}
				literal, ok := value.Values[index].(*ast.BasicLit)
				if !ok {
					t.Fatalf("compiler CLI contract is not literal: %s", name)
				}
				command, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for id := range compilerCLI {
					if id == "cli:"+command || strings.HasPrefix(id, "cli:"+command+":") {
						found = true
					}
				}
				if !found {
					t.Errorf("missing compiler CLI contract %s", command)
				}
			}
		}
	}
}

func TestSemanticBuiltinRejections(t *testing.T) {
	for _, op := range catalog.All() {
		t.Run(op.Name(), func(t *testing.T) {
			text := "return " + op.Name() + "(migration_unknown_argument: null)"
			code := "check.unknown-argument"
			switch op.Name() {
			case catalog.RecordNew:
				text = "return Record.new(x: 1, x: 2)"
				code = "check.duplicate-argument"
			case catalog.ListNew:
				text = "return List.new(1, true)"
				code = "check.type-mismatch"
			case catalog.MapNew:
				text = "return Map.new(1)"
				code = "check.type-mismatch"
			}
			got := run(t, "check", sourceFile(t, text), "--format", "json")
			if got.status != 1 || got.stdout != "" {
				t.Fatalf("invalid call accepted: %+v", got)
			}
			out := diagnostics(t, got.stderr)
			if len(out.Diagnostics) != 1 || out.Diagnostics[0].Span == nil || out.Diagnostics[0].Code != code {
				t.Fatalf("expected located %s rejection: %+v", code, out)
			}
		})
	}
}
