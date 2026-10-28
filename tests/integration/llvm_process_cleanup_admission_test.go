package integration_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestLLVMProcessCleanupAdmissionParity(t *testing.T) {
	for _, scenario := range []string{"outside-alias", "endpoint-kind", "depth-limit", "scope-count", "endpoint-count"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			path := fileFixture(t, fixture.writes, "data", "preserved")
			flags := fixture.flags()
			code := "runtime.capability-denied"
			switch scenario {
			case "outside-alias":
				if err := os.Link(path, filepath.Join(fixture.root, "outside")); err != nil {
					t.Fatal(err)
				}
				flags = append(flags, "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes))
			case "endpoint-kind":
				endpoint := fileFixture(t, fixture.root, "not-a-socket", "preserved")
				flags = append(flags, "--process-unix-endpoint", cleanupScope(t, fixture.executable, "endpoint", endpoint))
			case "depth-limit":
				parent := fixture.writes
				for index := 0; index < 129; index++ {
					child, err := os.MkdirTemp(parent, "d")
					if err != nil {
						t.Fatal(err)
					}
					next := filepath.Join(parent, "d")
					if err := os.Rename(child, next); err != nil {
						t.Fatal(err)
					}
					parent = next
				}
				flags = append(flags, "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes))
				code = "runtime.limit"
			case "scope-count", "endpoint-count":
				for index := 0; index < 17; index++ {
					if scenario == "endpoint-count" {
						flags = append(flags, "--process-unix-endpoint", cleanupScope(t, fixture.executable, "endpoint", filepath.Join(fixture.root, "e"+strconv.Itoa(index))))
						continue
					}
					root, err := os.MkdirTemp(fixture.root, "prune-")
					if err != nil {
						t.Fatal(err)
					}
					flags = append(flags, "--process-prune-root", cleanupScope(t, fixture.executable, "root", root))
				}
				code = "runtime.host-failure"
			}
			program := fixture.source(t, "write", path)
			if scenario == "scope-count" || scenario == "endpoint-count" {
				program = "return 1"
			}
			source := sourceFile(t, program)
			artifact, built := buildForTest(t, source)
			if built.status != 0 {
				t.Fatalf("build admission fixture: %+v", built)
			}
			reference := runExecutable(t, binary, append([]string{"run", source, "--format", "json"}, flags...)...)
			native := runExecutable(t, artifact, append([]string{"run", "--format", "json"}, flags...)...)
			if reference.status != 2 || diagnostics(t, reference.stderr).Diagnostics[0].Code != code {
				t.Fatalf("interpreter admission class: %+v", reference)
			}
			if native != reference {
				t.Fatalf("admission differs: native=%+v interpreter=%+v", native, reference)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "preserved" {
				t.Fatalf("rejected admission launched a write: %q %v", data, err)
			}
		})
	}
}
