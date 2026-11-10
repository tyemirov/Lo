package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestF036HostLimitArguments(t *testing.T) {
	artifact, built := buildForTest(t, sourceFile(t, "return 0"))
	if built.status != 0 {
		t.Fatalf("build descendant policy fixture: %+v", built)
	}
	for _, test := range []struct {
		name string
		args []string
	}{
		{"unused-defaults", []string{"--process-descendants"}},
		{"unused-explicit", []string{"--process-descendants", "--max-process-descendants", "8", "--max-process-tokens", "64", "--max-process-events", "2000", "--process-kernel-limit", "128"}},
		{"unused-finite-maxima", []string{"--process-descendants", "--max-process-descendants", "4294967295", "--max-process-tokens", "4294967295", "--max-process-events", "18446744073709551615", "--process-kernel-limit", "4294967295"}},
		{"selector-after-limit", []string{"--max-process-events", "1", "--process-descendants"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual := runExecutable(t, artifact, append([]string{"run", "--format", "json"}, test.args...)...)
			nativeValueCheck("Int", `"0"`)(t, actual)
		})
	}
	invalid := func(t *testing.T, args ...string) {
		t.Helper()
		actual := runExecutable(t, artifact, append([]string{"run", "--format", "json"}, args...)...)
		nativeFailure(t, actual, 2, "cli.invalid-invocation", "")
	}
	t.Run("duplicate-selector", func(t *testing.T) {
		invalid(t, "--process-descendants", "--process-descendants")
	})
	for _, flag := range []string{"--max-process-descendants", "--max-process-tokens", "--max-process-events", "--process-kernel-limit"} {
		t.Run(strings.TrimPrefix(flag, "--"), func(t *testing.T) {
			t.Run("requires-selector", func(t *testing.T) { invalid(t, flag, "1") })
			t.Run("duplicate", func(t *testing.T) { invalid(t, "--process-descendants", flag, "1", flag, "2") })
			t.Run("missing-value", func(t *testing.T) { invalid(t, "--process-descendants", flag) })
			overflow := "4294967296"
			if flag == "--max-process-events" {
				overflow = "18446744073709551616"
			}
			for _, test := range []struct{ name, value string }{
				{"zero", "0"}, {"negative", "-1"}, {"decimal-point", "1.0"},
				{"leading-plus", "+1"}, {"whitespace", " 1"}, {"nondecimal", "0x10"},
				{"empty", ""}, {"overflow", overflow}, {"unlimited", "unlimited"},
			} {
				t.Run(test.name, func(t *testing.T) { invalid(t, "--process-descendants", flag, test.value) })
			}
		})
	}
}

func f036UnavailableAfterLock(t *testing.T, actual result) {
	t.Helper()
	nativeFailure(t, actual, 2, "runtime.process-unavailable", "")
	if strings.Contains(actual.stderr, `"operation":"Process.run"`) {
		t.Fatalf("unavailable activation reported Process completion: %+v", actual)
	}
}

func f036RunPolicyFixture(t *testing.T, fixture f036Fixture, artifact string, extra ...string) result {
	t.Helper()
	return f036RunPolicyCommand(t, fixture, artifact, append(fixture.flags(t, artifact), extra...))
}

func f036RunPolicyCommand(t *testing.T, fixture f036Fixture, executable string, arguments []string) result {
	t.Helper()
	deadline, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	command := exec.CommandContext(deadline, executable, arguments...)
	command.Dir, command.Env = fixture.work, fixture.environment()
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if _, exited := err.(*exec.ExitError); !exited {
			t.Fatal(err)
		}
	}
	if deadline.Err() != nil {
		t.Fatalf("descendant policy fixture exceeded its deadline: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), status: command.ProcessState.ExitCode()}
}

func TestF036AdmittedHostLimits(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Darwin process and audit-token fixtures")
	}
	for _, test := range []struct {
		name                              string
		args                              []string
		processes, tokens, events, kernel int
	}{
		{name: "defaults", processes: 256, tokens: 1024, events: 1000000, kernel: 1024},
		{name: "explicit", args: []string{"--max-process-descendants", "8", "--max-process-tokens", "64", "--max-process-events", "2000"}, processes: 8, tokens: 64, events: 2000},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newF036Fixture(t)
			if test.name == "explicit" {
				test.kernel = int(fixture.kernelLimit)
				test.args = append(test.args, "--process-kernel-limit", fixture.kernelArgument())
			}
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "public-cli", source: strings.Replace(fixture.source("finish"), "Main()->Int onError Recover", "Main()->Int!", 1),
				harness: "testdata/native-runtime/process-ownership-provider.c", hostBootstrap: "lo_test_host_init", sdkHarness: true,
				harnessTransform: func(t *testing.T, source []byte) []byte {
					text := string(fixture.transform(t, source))
					const anchor = "    if(user!=36||!configuration||!emit)return LO_PROCESS_OWNER_FAILED;"
					if strings.Count(text, anchor) != 1 {
						t.Fatal("provider configuration assertion did not bind")
					}
					check := fmt.Sprintf("\n    if(configuration->max_processes!=%d || configuration->max_tokens!=%d || configuration->max_events!=%d || configuration->kernel_processes!=%d)_exit(81);\n    fixture_log(\"limits-ok\\n\");", test.processes, test.tokens, test.events, test.kernel)
					if test.name == "defaults" {
						check += "\n    return LO_PROCESS_OWNER_UNAVAILABLE;"
					}
					return []byte(strings.Replace(text, anchor, anchor+check, 1))
				},
				run: func(t *testing.T, artifact, _ string) {
					actual := f036RunPolicyFixture(t, fixture, artifact, test.args...)
					if test.name == "defaults" {
						f036UnavailableAfterLock(t, actual)
						if !fixture.logged("limits-ok\n") || fixture.logged("O\n") || fixture.lockBusy(t) {
							t.Fatal("default inspection launched or retained ownership")
						}
						macCleanupPresent(t, fixture.ready, false)
						return
					}
					nativeValueCheck("Int", `"0"`)(t, actual)
					if !fixture.logged("limits-ok\n") || !fixture.logged("C\n") || fixture.lockBusy(t) {
						t.Fatal("successful activation did not confirm selected limits and complete resource release")
					}
					macCleanupPresent(t, fixture.ready, true)
				}}})
		})
	}
}

func TestF036FixtureUsesInheritedKernelLimit(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Darwin process and audit-token fixtures")
	}
	fixture := newF036Fixture(t)
	parentLimit := fixture.kernelLimit
	bounded := min(fixture.kernelLimit, uint32(2048))
	limit := fmt.Sprint(bounded)
	fixture.kernelLimit = fixture.hostProcessLimit(t, "--host-process-limit-run", limit, fixture.command)
	if fixture.kernelLimit != bounded {
		t.Fatalf("child process-limit selection: got %d, want %d", fixture.kernelLimit, bounded)
	}
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "public-cli", source: strings.Replace(fixture.source("finish"), "Main()->Int onError Recover", "Main()->Int!", 1),
		harness: "testdata/native-runtime/process-ownership-provider.c", hostBootstrap: "lo_test_host_init", sdkHarness: true,
		harnessTransform: func(t *testing.T, source []byte) []byte {
			text := string(fixture.transform(t, source))
			const anchor = "    if(user!=36||!configuration||!emit)return LO_PROCESS_OWNER_FAILED;"
			if strings.Count(text, anchor) != 1 {
				t.Fatal("provider inherited-limit assertion did not bind")
			}
			check := fmt.Sprintf("\n    if(configuration->kernel_processes!=%du)_exit(81);\n    fixture_log(\"inherited-limit-ok\\n\");", bounded)
			return []byte(strings.Replace(text, anchor, anchor+check, 1))
		},
		run: func(t *testing.T, artifact, _ string) {
			arguments := append([]string{"--host-process-limit-run", limit, artifact}, fixture.flags(t, artifact)...)
			arguments = append(arguments, "--process-kernel-limit", fixture.kernelArgument())
			actual := f036RunPolicyCommand(t, fixture, fixture.command, arguments)
			nativeValueCheck("Int", `"0"`)(t, actual)
			if !fixture.logged("inherited-limit-ok\n") || !fixture.logged("C\n") || fixture.lockBusy(t) {
				t.Fatal("bounded inherited limit did not complete activation and release the lock")
			}
			macCleanupPresent(t, fixture.ready, true)
			if after := fixture.hostProcessLimit(t); after != parentLimit {
				t.Fatalf("child wrapper changed its parent limit: got %d, want %d", after, parentLimit)
			}
		}}})
}

func TestF036KernelLimitAboveInheritedHardLimit(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Darwin process and audit-token fixtures")
	}
	fixture := newF036Fixture(t)
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "public-cli", source: fixture.source("finish"),
		harness: "testdata/native-runtime/process-ownership-provider.c", hostBootstrap: "lo_test_host_init", sdkHarness: true,
		harnessTransform: func(t *testing.T, source []byte) []byte {
			text := string(fixture.transform(t, source))
			const anchor = "int lo_test_host_init("
			if strings.Count(text, anchor) != 1 {
				t.Fatal("supervisor limit injection did not bind")
			}
			text = strings.Replace(text, anchor, "int lo_original_host_init(", 1)
			return []byte(text + `
#include <sys/resource.h>
#include <dlfcn.h>
static int selected_supervisor;
int lo_test_host_init(int argc,char **argv,char **environment) {
    selected_supervisor=argc==2&&!strcmp(argv[1],"--lo-process-supervisor");
    return lo_original_host_init(argc,argv,environment);
}
int getrlimit(int resource,struct rlimit *limits) {
    if(selected_supervisor&&resource==RLIMIT_NPROC) {
        fixture_log("kernel-hard-limit\n");
        limits->rlim_cur=1;limits->rlim_max=1;return 0;
    }
    int (*original)(int,struct rlimit *)=dlsym(RTLD_NEXT,"getrlimit");
    if(!original)_exit(82);
    return original(resource,limits);
}
int setrlimit(int resource,const struct rlimit *limits) {
    if(selected_supervisor&&resource==RLIMIT_NPROC) {
        fixture_log("unexpected-kernel-change\n");errno=EINVAL;return -1;
    }
    int (*original)(int,const struct rlimit *)=dlsym(RTLD_NEXT,"setrlimit");
    if(!original)_exit(83);
    return original(resource,limits);
}
`)
		},
		run: func(t *testing.T, artifact, _ string) {
			actual := f036RunPolicyFixture(t, fixture, artifact, "--process-kernel-limit", "2")
			nativeFailure(t, actual, 2, "runtime.process-unavailable", "")
			if !fixture.logged("kernel-hard-limit\n") || fixture.logged("unexpected-kernel-change\n") || fixture.logged("O\n") {
				t.Fatal("kernel limit rejection did not precede mutation and provider creation")
			}
			macCleanupPresent(t, fixture.ready, false)
			if fixture.lockBusy(t) {
				t.Fatal("unavailable activation retained the caller lock")
			}
		}}})
}

func TestF036LinuxProviderCannotEnableDescendants(t *testing.T) {
	root := fileFixtureRoot(t)
	source := processContextProgram("Int", processGateSource(root)+`outcome=commit target;return 99`)
	command, launcher := filepath.Join(root, "absent-command"), filepath.Join(root, "absent-launcher")
	harness := fmt.Sprintf(`
#include "lo_process_ownership.h"
extern int lo_cli_main(int,const char *const *,const char *const *);
static int opened;
static uint32_t open_provider(uintptr_t user,const LoProcessOwnershipConfiguration *configuration,
    LoProcessOwnershipEmit emit,void *destination,LoProcessOwnershipClient *client) {
    (void)user;(void)configuration;(void)emit;(void)destination;(void)client;
    ++opened;return LO_PROCESS_OWNER_READY;
}
int lo_test_host_init(int argc,char **argv,char **environment) {
    (void)argc;(void)argv;(void)environment;
    return lo_process_ownership_provider(open_provider,36)?0:1;
}
int main(void) {
    const char *arguments[]={"fixture","run","--format","json","--process-descendants",
        "--process-allow",%q,"--process-launcher",%q,"--process-read-root",%q};
    int result=lo_cli_main(sizeof(arguments)/sizeof(arguments[0]),arguments,0);
    return opened?84:result;
}
`, strings.Repeat("0", 64)+","+command, launcher, root)
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", []llvmRuntimeScenario{{name: "public-cli", source: source,
		hostBootstrap: "lo_test_host_init", harnessTransform: func(_ *testing.T, _ []byte) []byte { return []byte(harness) },
		verify: func(t *testing.T, actual result) { f036UnavailableAfterLock(t, actual) }}})
}
