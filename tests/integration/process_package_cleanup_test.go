package integration_test

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func packageCleanupContext(t *testing.T, fixture cleanupFixture, extra ...[2]string) string {
	t.Helper()
	updates := [][2]string{{"PATH", fixture.root}, {"CI", "1"}, {"HOMEBREW_NO_AUTO_UPDATE", "1"}, {"HOMEBREW_NO_ANALYTICS", "1"}, {"GOTOOLCHAIN", "local"}, {"MISE_AUTO_INSTALL", "0"}, {"UV_PYTHON_DOWNLOADS", "never"}, {"LO_CLEANUP_ROOT", fixture.writes}, {"LO_CLEANUP_DIRECTORY", fixture.root}}
	var fields []string
	for _, update := range append(updates, extra...) {
		fields = append(fields, fmt.Sprintf("Record.new(key: %q, value: %q)", update[0], update[1]))
	}
	return fmt.Sprintf(`inherited = Process.Context.current(); updated = Process.Context.override_environment(inherited, Map.new(%s)); context = Process.Context.with_directory(updated, File.Path.new(%q));`, strings.Join(fields, ","), fixture.root)
}

func TestProcessPackageCleanupCommands(t *testing.T) {
	t.Setenv("LO_CLEANUP_INHERITED", "retained")
	for _, test := range []struct {
		tool      string
		arguments []string
	}{
		{"npm", []string{"cache", "clean", "--force"}}, {"go", []string{"clean", "-cache", "-testcache"}},
		{"uv", []string{"cache", "clean", "--force"}}, {"pnpm", []string{"store", "prune", "--yes"}},
		{"python3", []string{"-m", "pip", "cache", "purge"}},
	} {
		t.Run(test.tool, func(t *testing.T) {
			fixture := newCleanupFixtureSource(t, "process-package-command.c")
			path := filepath.Join(fixture.root, test.tool)
			if err := os.Rename(fixture.executable, path); err != nil {
				t.Fatal(err)
			}
			fixture.grant = strings.TrimSuffix(fixture.grant, fixture.executable) + path
			fixture.executable = path
			cache := fileFixture(t, fixture.writes, test.tool+".cache", "disposable")
			preserved := fileFixture(t, fixture.root, "preserved", "retained")
			var arguments []string
			for _, argument := range test.arguments {
				arguments = append(arguments, processLiteral(t, argument))
			}
			flags := append(fixture.flags(), "--process-prune-root", cleanupScope(t, path, "root", fixture.writes))
			for _, failure := range []bool{true, false} {
				var extra [][2]string
				if failure {
					extra = append(extra, [2]string{"LO_CLEANUP_FAIL", "1"})
				}
				selected := packageCleanupContext(t, fixture, extra...) + fmt.Sprintf(` executable = Process.Executable.resolve(context, %q); command = Process.Command.new(context, executable, List.new(%s), Bytes.new("")); result = commit command; return result`, test.tool, strings.Join(arguments, ","))
				got := run(t, append([]string{"run", sourceFile(t, processContextProgram("Process.Result", selected)), "--format", "json"}, flags...)...)
				fields := processFields(t, fileOutput(t, got))
				termination := processFields(t, fields["termination"])
				code, stdout, stderr := "0", "cleaned\n", ""
				if failure {
					code, stdout, stderr = "7", "", "package fixture failure\n"
				}
				valueJSON(t, termination["exit_code"], processLiteral(t, code))
				valueJSON(t, fields["stdout"], processLiteral(t, base64.StdEncoding.EncodeToString([]byte(stdout))))
				valueJSON(t, fields["stderr"], processLiteral(t, base64.StdEncoding.EncodeToString([]byte(stderr))))
				if _, err := os.Stat(cache); failure && err != nil || !failure && !os.IsNotExist(err) {
					t.Fatalf("cache result after failure=%v: %v", failure, err)
				}
			}
			data, err := os.ReadFile(preserved)
			if err != nil || string(data) != "retained" {
				t.Fatalf("ungranted fixture changed: %q %v", data, err)
			}
		})
	}
}

func TestProcessPackageCleanupPipProbe(t *testing.T) {
	t.Setenv("LO_CLEANUP_INHERITED", "retained")
	for _, absent := range []bool{false, true} {
		t.Run(fmt.Sprint(absent), func(t *testing.T) {
			fixture := newCleanupFixtureSource(t, "process-package-command.c")
			path := filepath.Join(fixture.root, "python3")
			if err := os.Rename(fixture.executable, path); err != nil {
				t.Fatal(err)
			}
			fixture.grant = strings.TrimSuffix(fixture.grant, fixture.executable) + path
			fixture.executable = path
			cache := fileFixture(t, fixture.writes, "python3.cache", "disposable")
			var extra [][2]string
			if absent {
				extra = append(extra, [2]string{"LO_CLEANUP_PIP_ABSENT", "1"})
			}
			body := packageCleanupContext(t, fixture, extra...) + ` executable = Process.Executable.resolve(context, "python3"); probe = Process.Command.new(context, executable, List.new("-m", "pip", "--version"), Bytes.new("")); checked = commit probe; code = Process.Termination.exit_code(checked.termination); clean = Process.Command.new(context, executable, List.new("-m", "pip", "cache", "purge"), Bytes.new("")); selected = Match.new(code, 0: List.new(clean), _: EmptyCommands()); return Flow.fold(selected, code, Purge)`
			purge := `flow EmptyCommands() -> List<Process.Command> { return List.new() }; flow Purge(previous: Int, command: Process.Command) -> Int! { cleaned = commit command; return Process.Termination.exit_code(cleaned.termination) }; `
			flags := append(fixture.flags(), "--process-prune-root", cleanupScope(t, path, "root", fixture.writes))
			got := run(t, append([]string{"run", sourceFile(t, purge+processContextProgram("Int", body)), "--format", "json"}, flags...)...)
			code := "0"
			if absent {
				code = "1"
			}
			valueJSON(t, fileOutput(t, got), processLiteral(t, code))
			if _, err := os.Stat(cache); absent && err != nil || !absent && !os.IsNotExist(err) {
				t.Fatalf("pip probe did not control cleanup: %v", err)
			}
		})
	}
}

func TestProcessPackageCleanupExample(t *testing.T) {
	t.Setenv("LO_CLEANUP_INHERITED", "retained")
	data, err := os.ReadFile(filepath.Join(repository, "docs/examples/package-cache-cleanup.lo"))
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"available", "absent", "nonzero"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newCleanupFixtureSource(t, "process-package-command.c")
			path := filepath.Join(fixture.root, "npm")
			if scenario != "absent" {
				if err := os.Rename(fixture.executable, path); err != nil {
					t.Fatal(err)
				}
				fixture.grant = strings.TrimSuffix(fixture.grant, fixture.executable) + path
				fixture.executable = path
			}
			cache := fileFixture(t, fixture.writes, "npm.cache", "disposable")
			var extra [][2]string
			if scenario == "nonzero" {
				extra = append(extra, [2]string{"LO_CLEANUP_FAIL", "1"})
			}
			source := strings.Replace(string(data), "context = CleanupContext()", packageCleanupContext(t, fixture, extra...), 1)
			flags := append(fixture.flags(), "--process-prune-root", cleanupScope(t, fixture.executable, "root", fixture.writes))
			got := run(t, append([]string{"run", sourceFile(t, source), "--format", "json"}, flags...)...)
			fields := processFields(t, fileOutput(t, got))
			available, code := "true", "0"
			if scenario == "absent" {
				available = "false"
			}
			if scenario == "nonzero" {
				code = "7"
			}
			valueJSON(t, fields["available"], available)
			valueJSON(t, fields["exit_code"], processLiteral(t, code))
			if _, err := os.Stat(cache); scenario == "available" && !os.IsNotExist(err) || scenario != "available" && err != nil {
				t.Fatalf("example cache state for %s: %v", scenario, err)
			}
		})
	}
}

func TestProcessPackageCleanupRegisteredInterpreter(t *testing.T) {
	t.Setenv("LO_CLEANUP_INHERITED", "retained")
	for _, scenario := range []string{"authorized", "unregistered-interpreter", "no-cleanup-scope"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newCleanupFixtureSource(t, "process-package-command.c")
			worker := filepath.Join(fixture.root, "package-worker")
			if err := os.Rename(fixture.executable, worker); err != nil {
				t.Fatal(err)
			}
			workerGrant := strings.TrimSuffix(fixture.grant, fixture.executable) + worker
			wrapper := filepath.Join(fixture.root, "npm")
			command := exec.Command("cc", filepath.Join(repository, "testdata/native-runtime/process-package-wrapper.c"), "-o", wrapper)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("compile package wrapper: %v: %s", err, output)
			}
			data, err := os.ReadFile(wrapper)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(data)
			fixture.executable, fixture.grant = wrapper, fmt.Sprintf("%x,%s", digest, wrapper)
			cache := fileFixture(t, fixture.writes, "npm.cache", "disposable")
			body := packageCleanupContext(t, fixture, [2]string{"LO_CLEANUP_WORKER", worker}) + ` executable = Process.Executable.resolve(context, "npm"); command = Process.Command.new(context, executable, List.new("cache", "clean", "--force"), Bytes.new("")); result = commit command; return Process.Termination.exit_code(result.termination)`
			flags := fixture.flags()
			if scenario != "unregistered-interpreter" {
				flags = append(flags, "--process-allow", workerGrant)
			}
			if scenario != "no-cleanup-scope" {
				flags = append(flags, "--process-prune-root", cleanupScope(t, wrapper, "root", fixture.writes))
			}
			got := run(t, append([]string{"run", sourceFile(t, processContextProgram("Int", body)), "--format", "json"}, flags...)...)
			code := "1"
			if scenario == "authorized" {
				code = "0"
			}
			valueJSON(t, fileOutput(t, got), processLiteral(t, code))
			if _, err := os.Stat(cache); scenario == "authorized" && !os.IsNotExist(err) || scenario != "authorized" && err != nil {
				t.Fatalf("interpreter cleanup authority for %s: %v", scenario, err)
			}
		})
	}
}
