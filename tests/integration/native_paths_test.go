package integration_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeBuildPaths(t *testing.T) {
	helper := filepath.Join(t.TempDir(), "go")
	command := exec.Command("go", "build", "-o", helper, "./testdata/native_toolchain")
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("controlled toolchain: %v %s", err, output)
	}
	state := t.TempDir()
	root, err := os.MkdirTemp(state, "build-root-")
	if err != nil {
		t.Fatal(err)
	}
	source := sourceFile(t, "return 1")
	output := filepath.Join(state, "artifact")
	command = exec.Command(binary, "build", source, "--output", output, "--format", "json")
	command.Env = append(os.Environ(), "PATH="+filepath.Dir(helper)+string(os.PathListSeparator)+os.Getenv("PATH"), "LO_BUILD_ROOT="+root, "LO_TEST_TOOL_STATE="+state, "LO_TEST_TOOL_ACTION=paths")
	data, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("controlled toolchain unexpectedly succeeded")
	}
	if !strings.Contains(string(data), "build.failed") {
		t.Fatalf("build diagnostic: %s", data)
	}
	recorded, err := os.ReadFile(filepath.Join(state, "paths.json"))
	if err != nil {
		t.Fatal(err)
	}
	var paths map[string]string
	if err := json.Unmarshal(recorded, &paths); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"work", "TMPDIR", "TMP", "TEMP", "GOCACHE", "GOMODCACHE", "GOPATH", "CLANG_MODULE_CACHE_PATH", "XDG_CACHE_HOME", "CCACHE_DIR"} {
		path := paths[key]
		relative, err := filepath.Rel(root, path)
		if err != nil || !filepath.IsAbs(path) || !filepath.IsLocal(relative) {
			t.Errorf("%s=%q is outside explicit build root %q", key, path, root)
		}
	}
	if paths["GOTELEMETRY"] != "off" || paths["GOTOOLCHAIN"] != "local" || paths["CI"] != "1" {
		t.Fatalf("unconfined tool state: %+v", paths)
	}
	if _, err := os.Stat(paths["work"]); !os.IsNotExist(err) {
		t.Fatalf("generated work remained: %v", err)
	}
}

func TestNativeBuildRootValidation(t *testing.T) {
	for _, scenario := range []string{"missing", "relative", "absent", "symlink-root", "symlink-cache"} {
		t.Run(scenario, func(t *testing.T) {
			state := t.TempDir()
			root := filepath.Join(state, "root")
			if err := os.Mkdir(root, os.ModePerm); err != nil {
				t.Fatal(err)
			}
			selected := root
			switch scenario {
			case "missing":
				selected = ""
			case "relative":
				selected = "relative-build-root"
			case "absent":
				selected = filepath.Join(state, "absent")
			case "symlink-root":
				selected = filepath.Join(state, "link")
				if err := os.Symlink(root, selected); err != nil {
					t.Fatal(err)
				}
			case "symlink-cache":
				other := filepath.Join(state, "other")
				if err := os.Mkdir(other, os.ModePerm); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, filepath.Join(root, "go-build")); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command(binary, "build", sourceFile(t, "return 1"), "--output", filepath.Join(state, "artifact"), "--format", "json")
			command.Env = append(os.Environ(), "LO_BUILD_ROOT="+selected)
			data, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(data), "build.output") || !strings.Contains(string(data), "LO_BUILD_ROOT") {
				t.Fatalf("invalid build root: %v %s", err, data)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			if scenario == "symlink-cache" {
				count = 1
			}
			if len(entries) != count {
				t.Fatalf("invalid root created build state: %v", entries)
			}
			if _, err := os.Stat(filepath.Join(state, "artifact")); !os.IsNotExist(err) {
				t.Fatalf("invalid root created artifact: %v", err)
			}
		})
	}
}
