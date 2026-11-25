package integration_test

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// One test worker uses the same Xcode tools across its fixture builds.
var darwinSDKHarnessTools struct {
	sync.Once
	compiler, sdk string
	err           error
}

func llvmDarwinSDKHarness(t *testing.T, work string) (string, []string) {
	t.Helper()
	darwinSDKHarnessTools.Do(func() {
		query := func(arguments ...string) (string, error) {
			command := exec.Command("/usr/bin/xcrun", append([]string{"--no-cache", "--sdk", "macosx"}, arguments...)...)
			command.Dir = work
			command.Env = []string{"CI=1", "PATH=/usr/bin:/bin", "TMPDIR=" + work, "CLANG_MODULE_CACHE_PATH=" + work}
			output, err := command.Output()
			if err != nil {
				return "", fmt.Errorf("discover SDK harness tool (%s): %w", arguments[0], err)
			}
			return strings.TrimSpace(string(output)), nil
		}
		darwinSDKHarnessTools.compiler, darwinSDKHarnessTools.err = query("--find", "clang")
		if darwinSDKHarnessTools.err == nil {
			darwinSDKHarnessTools.sdk, darwinSDKHarnessTools.err = query("--show-sdk-path")
		}
	})
	if darwinSDKHarnessTools.err != nil {
		t.Fatal(darwinSDKHarnessTools.err)
	}
	compiler, sdk := darwinSDKHarnessTools.compiler, darwinSDKHarnessTools.sdk
	if !filepath.IsAbs(compiler) || !filepath.IsAbs(sdk) {
		t.Fatal("SDK harness paths must be absolute")
	}
	flags := []string{"-target", "aarch64-apple-darwin", "-mmacosx-version-min=27.0", "-isysroot", sdk,
		"-O0", "-g", "-fPIC", "-ffp-contract=off", "-fno-modules", "-fblocks",
		"-iquote", filepath.Join(repository, "runtime/native/include"),
		"-iquote", filepath.Join(repository, "testdata/native-runtime")}
	return compiler, flags
}
