package integration_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func llvmDarwinSDKHarness(t *testing.T, work string) (string, []string) {
	t.Helper()
	query := func(argument string) string {
		t.Helper()
		arguments := []string{"--no-cache", "--sdk", "macosx", argument}
		if argument == "--find" {
			arguments = append(arguments, "clang")
		}
		command := exec.Command("/usr/bin/xcrun", arguments...)
		command.Dir = work
		command.Env = []string{"CI=1", "PATH=/usr/bin:/bin", "TMPDIR=" + work, "CLANG_MODULE_CACHE_PATH=" + work}
		output, err := command.Output()
		if err != nil {
			t.Fatalf("discover SDK harness tool: %v", err)
		}
		return strings.TrimSpace(string(output))
	}
	compiler, sdk := query("--find"), query("--show-sdk-path")
	if !filepath.IsAbs(compiler) || !filepath.IsAbs(sdk) {
		t.Fatal("SDK harness paths must be absolute")
	}
	flags := []string{"-target", "aarch64-apple-darwin", "-mmacosx-version-min=27.0", "-isysroot", sdk,
		"-O0", "-g", "-fPIC", "-ffp-contract=off", "-fno-modules", "-fblocks",
		"-iquote", filepath.Join(repository, "runtime/native/include"),
		"-iquote", filepath.Join(repository, "testdata/native-runtime")}
	return compiler, flags
}
