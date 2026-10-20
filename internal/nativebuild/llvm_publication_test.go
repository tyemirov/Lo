package nativebuild

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLLVMPublicationCleanupFailure(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "existing"}[existing], func(t *testing.T) {
			root := t.TempDir()
			staged, output := filepath.Join(root, "staged"), filepath.Join(root, "program")
			if err := writeFile(staged, []byte("replacement")); err != nil {
				t.Fatal(err)
			}
			if existing {
				if err := writeFile(output, []byte("previous")); err != nil {
					t.Fatal(err)
				}
			}
			injected := errors.New("injected work cleanup failure")
			err := publishLLVM(context.Background(), staged, output, filepath.Join(root, "work"), func(string) error { return injected })
			if !errors.Is(err, injected) {
				t.Fatalf("cleanup error lost: %v", err)
			}
			data, err := os.ReadFile(output)
			if existing && (err != nil || string(data) != "previous") {
				t.Fatalf("failure replaced prior artifact: %q, %v", data, err)
			}
			if !existing && !os.IsNotExist(err) {
				t.Fatalf("failure published an artifact: %q, %v", data, err)
			}
		})
	}
}

func TestLLVMPublicationSuccess(t *testing.T) {
	root := t.TempDir()
	staged, output, work := filepath.Join(root, "staged"), filepath.Join(root, "program"), filepath.Join(root, "work")
	if err := writeFile(filepath.Join(work, "input"), []byte("temporary")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(staged, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	if err := publishLLVM(context.Background(), staged, output, work, os.RemoveAll); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "replacement" {
		t.Fatalf("published artifact: %q, %v", data, err)
	}
	for _, path := range []string{work, staged} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("temporary input remains: %s, %v", path, err)
		}
	}
}

func TestLLVMPublicationRenameFailure(t *testing.T) {
	root := t.TempDir()
	staged, output := filepath.Join(root, "staged"), filepath.Join(root, "output")
	previous := filepath.Join(output, "program")
	if err := writeFile(previous, []byte("previous")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(staged, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	if err := publishLLVM(context.Background(), staged, output, filepath.Join(root, "work"), os.RemoveAll); err == nil {
		t.Fatal("rename over nonempty directory succeeded")
	}
	data, err := os.ReadFile(previous)
	if err != nil || string(data) != "previous" {
		t.Fatalf("prior artifact changed: %q, %v", data, err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("failed publication left staged file: %v", err)
	}
}
