package nativebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLLVMSnapshotChild(t *testing.T) {
	if len(os.Args) > 1 && os.Args[len(os.Args)-1] == "lo-snapshot-child" {
		fmt.Fprintln(os.Stdout, "validated snapshot")
		os.Exit(0)
	}
}

func TestLLVMSnapshotPreservesConsumedExecutable(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the prepared LLVM distribution requires a Darwin host")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	sum := snapshotTestDigest(t, self)
	for _, mutation := range []string{"replace", "overwrite"} {
		t.Run(mutation, func(t *testing.T) {
			work := snapshotTestDirectory(t)
			original := filepath.Join(work, "original")
			if _, err := snapshotLLVMFile(context.Background(), self, original, sum); err != nil {
				t.Fatal(err)
			}
			consumed, err := snapshotLLVMFile(context.Background(), original, filepath.Join(work, "consumed"), sum)
			if err != nil {
				t.Fatal(err)
			}
			if mutation == "replace" {
				replacement, err := os.CreateTemp(work, "replacement-")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := replacement.WriteString("replaced source bytes\n"); err != nil {
					t.Fatal(err)
				}
				if err := replacement.Close(); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement.Name(), original); err != nil {
					t.Fatal(err)
				}
			} else {
				changed, err := os.Create(original)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := changed.WriteString("overwritten source bytes\n"); err != nil {
					t.Fatal(err)
				}
				if err := changed.Close(); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command(consumed, "-test.run=^TestLLVMSnapshotChild$", "--", "lo-snapshot-child")
			command.Dir, command.Stdin = work, nil
			output, err := command.CombinedOutput()
			if err != nil || string(output) != "validated snapshot\n" {
				t.Fatalf("consumed executable changed with source: %q %v", output, err)
			}
		})
	}
}

func TestLLVMSnapshotRejectsUnpinnedBytes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the prepared LLVM distribution requires a Darwin host")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(snapshotTestDirectory(t), "rejected")
	_, err = snapshotLLVMFile(context.Background(), self, destination, strings.Repeat("0", 64))
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("unpinned snapshot: %v", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("rejected snapshot remains: %v", err)
	}
}

func snapshotTestDigest(t *testing.T, path string) string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func snapshotTestDirectory(t *testing.T) string {
	t.Helper()
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(filepath.Dir(working))
	temporary, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(root, temporary)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatal("snapshot tests require a temporary directory inside Lo")
	}
	return t.TempDir()
}
