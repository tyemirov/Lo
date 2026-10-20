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
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
)

func TestSemanticCoverageRunIdentity(t *testing.T) {
	backend := os.Getenv("LO_TEST_BACKEND")
	if backend == "" {
		backend = "interpreter"
	}
	if backend != "interpreter" && backend != "native" {
		t.Fatalf("unknown semantic backend %q", backend)
	}
	var paths []string
	for _, directory := range []string{"cmd/lo", "internal", "tests/integration", "tools/semanticcoverage", "testdata", "examples", "docs/examples"} {
		err := filepath.WalkDir(filepath.Join(repository, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if !slices.Contains([]string{".go", ".py", ".c", ".h", ".m", ".mm", ".metal", ".lo", ".json", ".txt"}, filepath.Ext(path)) {
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
	identity, err := json.Marshal(map[string]string{"backend": backend, "source_sha256": fmt.Sprintf("%x", hash.Sum(nil)), "matrix_sha256": fmt.Sprintf("%x", sha256.Sum256(matrix))})
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
	Positive              []string `json:"positive"`
	Negative              []string `json:"negative"`
	RuntimeNegative       []string `json:"runtime_negative"`
	NegativeApplicability string   `json:"negative_applicability"`
	Oracle                string   `json:"oracle"`
	Boundary              string   `json:"boundary"`
}
type semanticContract struct {
	ID       string   `json:"id"`
	Tests    []string `json:"tests"`
	Oracle   string   `json:"oracle"`
	Boundary string   `json:"boundary"`
}
type semanticMatrix struct {
	Operations []semanticOperation                                     `json:"operations"`
	CLI        []semanticContract                                      `json:"cli"`
	Semantics  []semanticContract                                      `json:"semantics"`
	Profiles   []semanticContract                                      `json:"profiles"`
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
			if fn, ok := declaration.(*ast.FuncDecl); ok && strings.HasPrefix(fn.Name.Name, "Test") {
				tests[fn.Name.Name] = true
			}
		}
	}
	checkRefs := func(id string, refs []string) {
		if len(refs) == 0 {
			t.Errorf("%s: missing test reference", id)
		}
		for _, ref := range refs {
			name, _, _ := strings.Cut(ref, "/")
			if !tests[name] {
				t.Errorf("%s: absent test %s", id, ref)
			}
		}
	}
	operations := map[string]catalog.Operation{}
	for _, op := range catalog.All() {
		operations[op.Name()] = op
	}
	seen := map[string]bool{}
	for _, row := range matrix.Operations {
		op, exists := operations[row.Operation]
		if !exists || seen[row.Operation] {
			t.Errorf("unknown or repeated operation %q", row.Operation)
			continue
		}
		seen[row.Operation] = true
		if row.Capability != op.Capability() || !slices.Equal(row.Errors, op.ErrorKinds()) || !slices.Equal(row.ActivationErrors, op.ActivationKinds()) {
			t.Errorf("%s: catalog contract changed; review migration coverage", row.Operation)
		}
		checkRefs(row.Operation, row.Positive)
		checkRefs(row.Operation, row.Negative)
		if len(row.Errors)+len(row.ActivationErrors) > 0 && row.NegativeApplicability != "verifier_only_canonical_state_constructor" {
			checkRefs(row.Operation, row.RuntimeNegative)
		}
		if row.NegativeApplicability == "verifier_only_canonical_state_constructor" && row.Operation != catalog.SGDStateNew {
			t.Errorf("%s: unreviewed runtime negative exemption", row.Operation)
		}
		if row.Oracle == "" || row.Boundary == "" || row.NegativeApplicability == "" {
			t.Errorf("%s: missing oracle, execution boundary, or negative applicability", row.Operation)
		}
	}
	for name := range operations {
		if !seen[name] {
			t.Errorf("missing operation %s", name)
		}
	}
	cli := map[string]bool{}
	for _, rows := range [][]semanticContract{matrix.CLI, matrix.Semantics, matrix.Profiles} {
		ids := map[string]bool{}
		for _, row := range rows {
			if ids[row.ID] || row.ID == "" {
				t.Errorf("duplicate/empty contract %q", row.ID)
			}
			ids[row.ID] = true
			checkRefs(row.ID, row.Tests)
			if row.Oracle == "" || row.Boundary == "" {
				t.Errorf("%s: missing oracle or boundary", row.ID)
			}
		}
	}
	for _, row := range matrix.CLI {
		cli[row.ID] = true
	}
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repository, "internal", "runner", "command.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value := spec.(*ast.ValueSpec)
			for i, name := range value.Names {
				if !strings.HasSuffix(name.Name, "Command") && !strings.HasSuffix(name.Name, "Option") {
					continue
				}
				literal, ok := value.Values[i].(*ast.BasicLit)
				if !ok {
					t.Fatalf("CLI contract is not a literal: %s", name)
				}
				id, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				if !cli[id] {
					t.Errorf("missing CLI contract %s", id)
				}
				delete(cli, id)
			}
		}
	}
	// Native executables add metadata and omit the compiler's source operand.
	delete(cli, "metadata")
	delete(cli, "exit-status")
	delete(cli, "artifact-invocation")
	for id := range cli {
		t.Errorf("obsolete CLI contract %s", id)
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
