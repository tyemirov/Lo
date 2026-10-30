package integration_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestFileObservationDirectoryCapacity(t *testing.T) {
	for _, count := range []int{128, 129} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			base := fileFixtureRoot(t)
			paths := make([]string, count)
			arguments := []string{}
			var first string
			for index := range paths {
				root, err := os.MkdirTemp(base, "directory-")
				if err != nil {
					t.Fatal(err)
				}
				if index == 0 {
					first = root
				}
				paths[index] = fmt.Sprintf("File.Path.new(%q)", root)
				arguments = append(arguments, "--list-root", root)
			}
			text := fmt.Sprintf(`flow Keep(directories: List<File.Directory>) -> List<File.Directory> { return directories }; flow Observe(directories: List<File.Directory>, path: File.Path) -> List<File.Directory>! { directory = File.directory(path); return List.flat_map(List.new(directories, List.new(directory)), Keep) }; flow Main() -> Int! { directories = Flow.fold(List.new(%s), List.new(), Observe); repeated = File.directory(File.Path.new(%q)); return List.length(directories) }; return Main()`, strings.Join(paths, ","), first)
			arguments = append([]string{"run", sourceFile(t, text)}, arguments...)
			arguments = append(arguments, "--format", "json")
			got := run(t, arguments...)
			if count == 128 {
				valueJSON(t, fileOutput(t, got), `"128"`)
				return
			}
			if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" {
				t.Fatalf("129th live directory must fail at the configured host capacity boundary: %+v", got)
			}
		})
	}
}
