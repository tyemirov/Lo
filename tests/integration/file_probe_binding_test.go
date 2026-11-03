package integration_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileOpenPathsProtectsExecutable(t *testing.T) {
	for _, alias := range []string{"direct", "hardlink", "symlink"} {
		for _, operation := range []string{"Write", "Append", "Remove"} {
			t.Run(alias+"/"+operation, func(t *testing.T) {
				record := installFileProbeFixture(t)
				tool := filepath.Join(filepath.Dir(record), "lsof")
				original, err := os.ReadFile(tool)
				if err != nil {
					t.Fatal(err)
				}
				root := filepath.Dir(tool)
				if alias != "direct" {
					root = fileFixtureRoot(t)
					link := filepath.Join(root, "lsof")
					if alias == "hardlink" {
						err = os.Link(tool, link)
					} else {
						err = os.Symlink(tool, link)
					}
					if err != nil {
						t.Fatal(err)
					}
					if alias == "symlink" {
						t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
					} else {
						tool = link
					}
				}
				if alias == "symlink" && operation == "Remove" {
					root = filepath.Dir(record)
				}
				mutation := fmt.Sprintf(`target = File.%s.new(File.Path.new(%q), Bytes.new("replacement")); commit target`, operation, tool)
				if operation == "Remove" {
					mutation = fmt.Sprintf(`entries = File.list(File.Path.new(%q)); target = File.Remove.new(entries); commit target`, root)
				}
				program := sourceFile(t, fmt.Sprintf(`flow Main() -> List<File.Path>! {%s; return File.open_paths(File.Path.new(%q))}; return Main()`, mutation, root))
				args := []string{"run", program, "--write-root", root, "--list-root", root, "--remove-root", root, "--open-files-root", root, "--format", "json"}
				if alias == "symlink" && root != filepath.Dir(record) {
					args = append(args, "--write-root", filepath.Dir(record))
				}
				got := runProgram(t, args[1], args[0], args[2:]...)
				if got.status != 2 || !strings.Contains(got.stderr, "protected") {
					t.Fatalf("probe mutation was not denied: %+v", got)
				}
				remaining, err := os.ReadFile(tool)
				if err != nil || sha256.Sum256(remaining) != sha256.Sum256(original) {
					t.Fatalf("probe bytes changed: %v", err)
				}
				if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("probe ran after mutation: %v", err)
				}
			})
		}
	}
}

func TestFileOpenPathsParsingBudget(t *testing.T) {
	record := installFileProbeFixture(t)
	root := fileFixtureRoot(t)
	t.Setenv("LO_TEST_PROBE_STDOUT", strings.Repeat("p123\n", 100))
	t.Setenv("LO_TEST_PROBE_EXIT", "0")
	path := sourceFile(t, fmt.Sprintf(`flow Main()->List<File.Path>!{return File.open_paths(File.Path.new(%q))};return Main()`, root))
	good := fileOutput(t, runProgram(t, path, "run", "--open-files-root", root, "--max-steps", "1000", "--format", "json"))
	valueJSON(t, good, `[]`)
	if err := os.Remove(record); err != nil {
		t.Fatal(err)
	}
	got := runProgram(t, path, "run", "--open-files-root", root, "--max-steps", "60", "--format", "json")
	if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" {
		t.Fatalf("raw probe parsing bypassed logical steps: %+v", got)
	}
	if _, err := os.Stat(record); err != nil {
		t.Fatalf("limit occurred before raw probe completed: %v", err)
	}
}
