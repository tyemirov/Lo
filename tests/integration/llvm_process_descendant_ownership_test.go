package integration_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLLVMProcessDescendantAdmissionDoesNotLaunchWithoutProvider(t *testing.T) {
	fixture := newCleanupFixture(t)
	marker := fileFixture(t, fixture.writes, "admission-marker", "preserved")
	source := sourceFile(t, fixture.source(t, "write", marker))
	artifact, built := buildForTest(t, source)
	if built.status != 0 {
		t.Fatalf("build descendant admission fixture: %+v", built)
	}

	// These freshly linked test executables have no Endpoint Security entitlement.
	// Admission must finish before the registered command can change the marker.
	for _, launcher := range []struct{ name, path string }{
		{name: "external", path: filepath.Join(repository, "bin", "lo-process-launcher")},
		{name: "embedded", path: artifact},
	} {
		t.Run(launcher.name, func(t *testing.T) {
			fileFixture(t, fixture.writes, "admission-marker", "preserved")
			flags := []string{
				"run", "--format", "json",
				"--process-descendants",
				"--process-allow", fixture.grant,
				"--process-launcher", launcher.path,
				"--process-read-root", fixture.root,
				"--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes),
			}
			actual := runExecutable(t, artifact, flags...)
			data, err := os.ReadFile(marker)
			if err != nil || string(data) != "preserved" {
				t.Fatalf("unavailable descendant provider allowed command mutation: %q, %v; result: %+v", data, err, actual)
			}
			nativeFailure(t, actual, 2, "runtime.process-unavailable", "")
		})
	}
}

func TestLLVMProcessWithoutDescendantAuthorityRejectsCreation(t *testing.T) {
	fixture := newCleanupFixture(t)
	marker := fileFixture(t, fixture.writes, "default-profile-marker", "preserved")
	for _, operation := range []string{"fork", "spawn"} {
		t.Run(operation, func(t *testing.T) {
			arguments := []string{operation, fixture.root}
			if operation == "spawn" {
				arguments = []string{operation, fixture.executable, marker}
			}
			source := sourceFile(t, fixture.source(t, arguments...))
			artifact, built := buildForTest(t, source)
			if built.status != 0 {
				t.Fatalf("build default profile fixture: %+v", built)
			}
			for _, launcher := range []struct{ name, path string }{
				{name: "external", path: filepath.Join(repository, "bin", "lo-process-launcher")},
				{name: "embedded", path: artifact},
			} {
				t.Run(launcher.name, func(t *testing.T) {
					fileFixture(t, fixture.writes, "default-profile-marker", "preserved")
					flags := []string{
						"run", "--format", "json",
						"--process-allow", fixture.grant,
						"--process-launcher", launcher.path,
						"--process-read-root", fixture.root,
						"--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes),
					}
					actual := runExecutable(t, artifact, flags...)
					nativeValueCheck("Int", `"1"`)(t, actual)
					data, err := os.ReadFile(marker)
					if err != nil || string(data) != "preserved" {
						t.Fatalf("default profile allowed descendant mutation: %q, %v; result: %+v", data, err, actual)
					}
				})
			}
		})
	}
}
