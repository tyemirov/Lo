package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileObservationScopeLifetime(t *testing.T) {
	base := fileFixtureRoot(t)
	paths := make([]string, 160)
	arguments := []string{}
	for index := range paths {
		root, err := os.MkdirTemp(base, "directory-")
		if err != nil {
			t.Fatal(err)
		}
		paths[index] = fmt.Sprintf("File.Path.new(%q)", root)
		arguments = append(arguments, "--list-root", root)
	}
	text := fmt.Sprintf(`flow Observe(count: Int, path: File.Path) -> Int! { directory = File.directory(path); return Math.add(count, 1) }; return Flow.fold(List.new(%s), 0, Observe)`, strings.Join(paths, ","))
	arguments = append([]string{"run", sourceFile(t, text)}, arguments...)
	arguments = append(arguments, "--format", "json")
	valueJSON(t, fileOutput(t, runProgram(t, arguments[1], arguments[0], arguments[2:]...)), `"160"`)
}

func TestFileObservationEscapesThroughRemovalAndGuard(t *testing.T) {
	selected := fileFixtureRoot(t)
	contextRoot := fileFixtureRoot(t)
	fileFixture(t, selected, "selected", "content")
	paths := make([]string, 160)
	arguments := []string{"--list-root", selected, "--remove-root", selected, "--list-root", contextRoot}
	for index := range paths {
		root, err := os.MkdirTemp(fileFixtureRoot(t), "directory-")
		if err != nil {
			t.Fatal(err)
		}
		paths[index] = fmt.Sprintf("File.Path.new(%q)", root)
		arguments = append(arguments, "--list-root", root)
	}
	text := fmt.Sprintf(`flow Guard(directory: File.Directory!, original: File.Entry, current: File.Entry) -> Bool { return Math.eq(List.length(List.new(directory)), 1) }; flow Build() -> File.Remove! { scan = File.scan(File.Path.new(%q)); directory = File.directory(File.Path.new(%q)); return File.Remove.checked(scan.entries, directory, Guard) }; flow Observe(count: Int, path: File.Path) -> Int! { directory = File.directory(path); return Math.add(count, 1) }; flow Main() -> Int! { target = Build(); visited = Flow.fold(List.new(%s), 0, Observe); receipt = commit target; return receipt.completed_count }; return Main()`, selected, contextRoot, strings.Join(paths, ","))
	arguments = append([]string{"run", sourceFile(t, text)}, arguments...)
	arguments = append(arguments, "--format", "json")
	valueJSON(t, fileOutput(t, runProgram(t, arguments[1], arguments[0], arguments[2:]...)), `"1"`)
}

func TestFileObservationDynamicDirectoryTraversal(t *testing.T) {
	root := fileFixtureRoot(t)
	arguments := []string{"--list-root", root}
	parent := root
	for index := 1; index < 160; index++ {
		created, err := os.MkdirTemp(parent, "")
		if err != nil {
			t.Fatal(err)
		}
		child := filepath.Join(parent, "d")
		if err := os.Rename(created, child); err != nil {
			t.Fatal(err)
		}
		arguments = append(arguments, "--list-root", child)
		parent = child
	}
	text := fmt.Sprintf(`flow Path(entry: File.Entry) -> File.Path { return entry.path }; flow Step(state: Int, path: File.Path) -> Record{state: Int, items: List<File.Path>, stop: Bool}! { scan = File.scan(path); return Record.new(state: Math.add(state, 1), items: List.map(scan.entries, Path), stop: false) }; return Flow.worklist(List.new(File.Path.new(%q)), 0, Step)`, root)
	arguments = append([]string{"run", sourceFile(t, text)}, arguments...)
	arguments = append(arguments, "--format", "json")
	valueJSON(t, fileOutput(t, runProgram(t, arguments[1], arguments[0], arguments[2:]...)), `"160"`)
}
