package integration_test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMacCleanupOwnedWorkflow(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Darwin process ownership fixture")
	}
	owner := newF036Fixture(t)
	fixture := newCleanupWorkflowFixture(t)
	compiler, flags := llvmDarwinSDKHarness(t, owner.work)
	ownedCommand := filepath.Join(fixture.root, "owned-command")
	command := exec.Command(compiler, append(flags, filepath.Join(repository, "testdata/native-runtime/mac-cleanup-owned-command.c"), "-o", ownedCommand)...)
	command.Dir, command.Env = owner.work, owner.environment()
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build owned cleanup command: %v\n%s", err, output)
	}
	for _, path := range fixture.tools {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(ownedCommand, path); err != nil {
			t.Fatal(err)
		}
	}
	childMarker := fileFixture(t, fixture.prunes, "owned-child", "")
	fixture.overrides = [][2]string{{"FIXTURE_OWNER_SOCKET", owner.socket}, {"FIXTURE_OWNER_CHILD", childMarker}}
	fixture.extraFlags = []string{"--process-descendants", "--max-process-descendants", "8", "--max-process-tokens", "64", "--max-process-events", "10000", "--process-kernel-limit", "4096", "--process-read-root", owner.root}
	for _, name := range []string{"npm", "go", "uv", "pnpm", "python3", "getconf", "du", "lsof", "docker"} {
		fixture.extraFlags = append(fixture.extraFlags, "--process-unix-endpoint", cleanupScope(t, fixture.tools[name], "endpoint", owner.socket))
	}
	scenario := fixture.scenario(t, "apply", "success", false, func(t *testing.T, actual result) {
		defer func() {
			if !t.Failed() {
				return
			}
			for _, path := range []string{owner.log, owner.tokens, fixture.calls, childMarker} {
				file, err := os.Open(path)
				if err != nil {
					t.Logf("owned workflow evidence %s: %v", filepath.Base(path), err)
					continue
				}
				size, seekErr := file.Seek(0, io.SeekEnd)
				offset := int64(0)
				if size > 32768 {
					offset = size - 32768
				}
				if seekErr == nil {
					_, seekErr = file.Seek(offset, io.SeekStart)
				}
				contents, readErr := io.ReadAll(io.LimitReader(file, 32768))
				closeErr := file.Close()
				t.Logf("owned workflow evidence %s (last at most 32768 bytes): %q; seek=%v read=%v close=%v", filepath.Base(path), contents, seekErr, readErr, closeErr)
			}
		}()
		if actual.status != 0 || actual.stderr != "" || !strings.Contains(actual.stdout, "Cleanup complete") || !strings.Contains(actual.stdout, "net volume change: 2.00 GiB") {
			t.Fatalf("owned complete workflow: %+v", actual)
		}
		calls := cleanupWorkflowCalls(t, fixture)
		if !strings.HasPrefix(calls, "python3\t-m\tpip\t--version\nnpm\tcache\tclean\t--force\n") || !strings.Contains(calls, "docker\t--context\tdesktop-linux\timage\tprune\t--all\t--force\n") {
			t.Fatalf("owned command order: %q", calls)
		}
		for _, paths := range [][]string{fixture.direct, fixture.recursive, fixture.removedTrees} {
			for _, path := range paths {
				macCleanupPresent(t, path, false)
			}
		}
		for _, path := range fixture.preserved {
			macCleanupPresent(t, path, true)
		}
		events, err := os.ReadFile(owner.log)
		if err != nil {
			t.Fatal(err)
		}
		commands := strings.Count(calls, "\n")
		if strings.Count(string(events), "O\n") != commands || strings.Count(string(events), "C\n") != commands {
			t.Fatalf("per-command ownership completion: commands=%d events=%q", commands, events)
		}
		records := owner.tokenRecords(t)
		child, err := os.ReadFile(childMarker)
		if err != nil || len(child) != 64 {
			t.Fatalf("missing confirmed child identity: %q, %v", child, err)
		}
		childObserved := false
		for _, record := range records {
			if record[0] == "command" && record[1] == string(child) {
				childObserved = true
			}
			if owner.alive(record[1]) {
				t.Fatalf("owned process still alive after application completion: %s", record[0])
			}
		}
		if !childObserved {
			t.Fatalf("controller did not observe confirmed child identity: %s", child)
		}
	})
	baseTransform := scenario.harnessTransform
	scenario.hostBootstrap = "lo_test_host_init"
	scenario.sdkHarness = true
	scenario.harnessTransform = func(t *testing.T, source []byte) []byte {
		app := string(baseTransform(t, source))
		const cli = "int status = lo_cli_main(sizeof(arguments) / sizeof(arguments[0]), arguments, 0);"
		if strings.Count(app, cli) != 1 {
			t.Fatal("missing canonical workflow CLI call")
		}
		app = strings.Replace(app, cli, `for (unsigned index = 0; index + 1 < sizeof(arguments) / sizeof(arguments[0]); ++index) {
        if (!strcmp(arguments[index], "--process-launcher")) arguments[index + 1] = argv[0];
    }
    `+cli, 1)
		provider, err := os.ReadFile(filepath.Join(repository, "testdata/native-runtime/process-ownership-provider.c"))
		if err != nil {
			t.Fatal(err)
		}
		provider = owner.transform(t, provider)
		const providerMain = "int main(int argc,char **argv,char **environment)"
		if strings.Count(string(provider), providerMain) != 1 {
			t.Fatal("missing provider fixture main")
		}
		provider = []byte(strings.Replace(string(provider), providerMain, "int lo_unused_provider_main(int argc,char **argv,char **environment)", 1))
		// SDK declarations must precede the application fixture's public ABI
		// declarations and calls, including the SDK's symbol labels.
		return []byte(string(provider) + "\n" + app)
	}
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
}
