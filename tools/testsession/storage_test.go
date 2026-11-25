package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionStorageUsesEffectiveCacheOverrides(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	unused := t.TempDir()
	t.Setenv("LO_NATIVE_PREPARATION", unused)
	values := isolatedCacheEnvironment()
	values["LO_UNUSED_PREPARATION"] = unused
	script := "import os,pathlib\n(pathlib.Path(os.environ['LO_UNUSED_PREPARATION'])/'other-owner-output').write_bytes(b'x'*(4<<20))\n"
	entry := map[string]any{"id": "effective-cache", "argv": []string{python, "-B", "-c", script}, "environment": values}
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{entry}}, "--max-bytes", "3145728")
	if err != nil || !strings.Contains(stdout, `"status":"pass"`) {
		t.Fatalf("unused inherited preparation was charged after explicit override: %v\n%s\n%s", err, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(unused, "other-owner-output")); err != nil {
		t.Fatalf("unselected preparation was removed: %v", err)
	}
}

func TestSessionStorageAccountsDeclaredPersistentCacheGrowth(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"preexisting", "growth", "work-hardlink"} {
		t.Run(variant, func(t *testing.T) {
			cache := t.TempDir()
			original := strings.Repeat("p", 1<<20)
			before := filepath.Join(cache, "preexisting")
			if err := os.WriteFile(before, []byte(original), os.ModePerm); err != nil {
				t.Fatal(err)
			}
			script := "import os,pathlib,sys\ncache=pathlib.Path(os.environ['GOCACHE'])\nwork=pathlib.Path(sys.argv[1])\n"
			if variant == "growth" {
				script += "(cache/'new-output').write_bytes(b'n'*(4<<20))\n"
			} else if variant == "work-hardlink" {
				script += "(cache/'new-output').write_bytes(b'n'*(2<<20))\nos.link(cache/'new-output',work/'retained')\n"
			}
			values := isolatedCacheEnvironment()
			values["GOCACHE"] = cache
			entry := map[string]any{"id": "cache-storage", "argv": []string{python, "-B", "-c", script, "{work}"}, "environment": values}
			stdout, stderr, err := invoke(t, map[string]any{"groups": []any{entry}}, "--max-bytes", "3145728")
			if variant == "growth" {
				if err == nil || !strings.Contains(stderr, "storage limit") || strings.Contains(stdout, `"status":"pass"`) {
					t.Fatalf("persistent cache growth escaped storage cap: %v\n%s\n%s", err, stdout, stderr)
				}
			} else if err != nil || !strings.Contains(stdout, `"status":"pass"`) || !strings.Contains(stdout, `"max_bytes":3145728`) {
				t.Fatalf("existing or hardlinked cache bytes charged incorrectly: %v\n%s\n%s", err, stdout, stderr)
			}
			actual, readErr := os.ReadFile(before)
			if readErr != nil || string(actual) != original {
				t.Fatalf("session changed preexisting cache: %v", readErr)
			}
			if variant != "preexisting" {
				if _, err := os.Stat(filepath.Join(cache, "new-output")); err != nil {
					t.Fatalf("session removed shared cache output: %v", err)
				}
			}
		})
	}
}

func TestSessionStorageChargesUniqueFiles(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []struct {
		name, operation string
		withinLimit     bool
	}{
		{name: "hardlinks", operation: "os.link(source, destination)", withinLimit: true},
		{name: "independent-copy", operation: "destination.write_bytes(source.read_bytes())"},
	} {
		t.Run(variant.name, func(t *testing.T) {
			script := "import os,pathlib,sys\nroot=pathlib.Path(sys.argv[1])\nsource=root/'source'\ndestination=root/'destination'\nsource.write_bytes(b'x'*(2<<20))\n" + variant.operation + "\n"
			entry := map[string]any{"id": "storage", "argv": []string{python, "-B", "-c", script, "{work}"}, "environment": isolatedCacheEnvironment()}
			stdout, stderr, err := invoke(t, map[string]any{"groups": []any{entry}}, "--max-bytes", "3145728")
			if variant.withinLimit {
				if err != nil || !strings.Contains(stdout, `"status":"pass"`) {
					t.Fatalf("hardlinked input charged more than once: %v\n%s\n%s", err, stdout, stderr)
				}
			} else if err == nil || !strings.Contains(stderr, "storage limit") || strings.Contains(stdout, `"status":"pass"`) {
				t.Fatalf("independent file bytes escaped storage cap: %v\n%s\n%s", err, stdout, stderr)
			}
		})
	}
}

func TestSessionStorageAccountsPreparedProducerGrowth(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	preparation := t.TempDir()
	original := strings.Repeat("p", 4<<20)
	pinned := filepath.Join(preparation, "pinned-input")
	if err := os.WriteFile(pinned, []byte(original), os.ModePerm); err != nil {
		t.Fatal(err)
	}
	script := "import os,pathlib\nroot=pathlib.Path(os.environ['LO_NATIVE_PREPARATION'])/'cargo-target'\nroot.mkdir()\n(root/'new-output').write_bytes(b'n'*(4<<20))\n"
	values := isolatedCacheEnvironment()
	values["LO_NATIVE_PREPARATION"] = preparation
	entry := map[string]any{"id": "producer-storage", "argv": []string{python, "-B", "-c", script}, "environment": values}
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{entry}}, "--max-bytes", "3145728")
	if err == nil || !strings.Contains(stderr, "storage limit") || strings.Contains(stdout, `"status":"pass"`) {
		t.Fatalf("native producer growth escaped storage cap: %v\n%s\n%s", err, stdout, stderr)
	}
	actual, readErr := os.ReadFile(pinned)
	if readErr != nil || string(actual) != original {
		t.Fatalf("session changed pinned preparation input: %v", readErr)
	}
	if _, err := os.Stat(filepath.Join(preparation, "cargo-target/new-output")); err != nil {
		t.Fatalf("session removed prepared producer state: %v", err)
	}
}
