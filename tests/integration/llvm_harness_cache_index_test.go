package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLLVMRuntimeObjectCacheAccountedIndex(t *testing.T) {
	t.Setenv("LO_TEST_SESSION_DIRECTORY", t.TempDir())
	tool, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	const count = 32
	builds := 0
	var first []byte
	for index := range count + 1 {
		value := index % count
		source := sourceFile(t, fmt.Sprintf("package fixture\nconst Value = %d\n", value))
		output := filepath.Join(t.TempDir(), "package.a")
		runtimeCachedObject(t, tool, "index-fixture", source, output, []string{"tool", "compile", "-o", output, source}, func(directory string, arguments []string) {
			builds++
			command := exec.Command(tool, arguments...)
			command.Dir = directory
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("compile indexed cache fixture: %v %s", err, output)
			}
		})
		data, err := os.ReadFile(output)
		if err != nil || len(data) == 0 {
			t.Fatalf("indexed cache lost its private object: bytes=%d error=%v", len(data), err)
		}
		if index == 0 {
			first = data
		} else if index == count && !bytes.Equal(data, first) {
			t.Fatal("reused indexed object changed its compiler output")
		}
	}
	if builds != count {
		t.Fatalf("indexed cache compiled %d times; expected %d distinct source builds", builds, count)
	}
	root := runtimeCacheRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "object-index.json"))
	if err != nil {
		t.Fatalf("completed runtime objects lack accounted storage: %v", err)
	}
	var index struct {
		Entries map[string]int64 `json:"entries"`
		Total   int64            `json:"total"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Entries) != count {
		t.Fatalf("indexed cache lost completed identities: %d entries", len(index.Entries))
	}
	var actual int64
	if err := filepath.WalkDir(filepath.Join(root, "objects"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		info, err := entry.Info()
		if err == nil {
			actual += info.Size()
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if actual != index.Total {
		t.Fatalf("runtime cache accounting differs from completed files: actual=%d indexed=%d", actual, index.Total)
	}
}

func TestLLVMRuntimeObjectCacheAccountedEviction(t *testing.T) {
	t.Setenv("LO_TEST_SESSION_DIRECTORY", t.TempDir())
	tool, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	const sourceText = "package fixture\nconst Value = 41\n"
	source := sourceFile(t, sourceText)
	compiled := filepath.Join(t.TempDir(), "package.a")
	runtimeCachedObject(t, tool, "eviction-fixture", source, compiled, []string{"tool", "compile", "-o", compiled, source}, func(directory string, arguments []string) {
		command := exec.Command(tool, arguments...)
		command.Dir = directory
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("compile bounded cache fixture: %v %s", err, output)
		}
	})
	root := runtimeCacheRoot(t)
	index, err := runtimeReadObjectIndex(root)
	if err != nil || len(index.Entries) != 1 {
		t.Fatalf("initial accounted object: %+v %v", index, err)
	}
	limit := index.Total
	var firstKey string
	for key := range index.Entries {
		firstKey = key
	}
	expected, err := os.ReadFile(compiled)
	if err != nil {
		t.Fatal(err)
	}
	publish := func(name string) (string, string) {
		t.Helper()
		key := fmt.Sprintf("%x", sha256.Sum256([]byte(name)))
		directory := filepath.Join(root, "objects", key)
		if err := os.Mkdir(directory, os.ModePerm); err != nil {
			t.Fatal(err)
		}
		input, err := os.Create(filepath.Join(directory, "source.go"))
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := input.WriteString(sourceText)
		if err := input.Close(); writeErr != nil || err != nil {
			t.Fatalf("write bounded source fixture: %v %v", writeErr, err)
		}
		if err := os.Link(compiled, filepath.Join(directory, "object.o")); err != nil {
			t.Fatal(err)
		}
		private := filepath.Join(t.TempDir(), "retained.a")
		if err := os.Link(filepath.Join(directory, "object.o"), private); err != nil {
			t.Fatal(err)
		}
		unlockEntry := runtimeCacheLock(t, root, key)
		defer unlockEntry()
		unlockStorage := runtimeCacheLock(t, root, "storage")
		defer unlockStorage()
		if err := runtimeRegisterObjectCache(root, key, limit); err != nil {
			t.Fatal(err)
		}
		return key, private
	}
	unlockFirst := runtimeCacheLock(t, root, firstKey)
	lockedKey, lockedPrivate := publish("cannot-evict-active-user")
	if _, err := os.Stat(filepath.Join(root, "objects", lockedKey)); !os.IsNotExist(err) {
		t.Fatalf("cache exceeded its budget while the old entry was locked: %v", err)
	}
	unlockFirst()
	newKey, newPrivate := publish("evict-released-object")
	index, err = runtimeReadObjectIndex(root)
	if err != nil || len(index.Entries) != 1 || index.Entries[newKey] != limit || index.Total != limit {
		t.Fatalf("released object eviction changed accounting: %+v %v", index, err)
	}
	if _, err := os.Stat(filepath.Join(root, "objects", firstKey)); !os.IsNotExist(err) {
		t.Fatalf("unlocked completed object was not evicted: %v", err)
	}
	for _, private := range []string{compiled, lockedPrivate, newPrivate} {
		actual, err := os.ReadFile(private)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("cache eviction damaged a privately retained compiled object: %s: %v", private, err)
		}
	}
}

func TestLLVMRuntimeObjectCacheRejectsMalformedIndex(t *testing.T) {
	key := strings.Repeat("a", 64)
	for _, raw := range []string{
		`{}`,
		`{"entries":{},"total":0,"unexpected":true}`,
		`{"entries":{"bad-key":1},"total":1}`,
		fmt.Sprintf(`{"entries":{%q:-1},"total":-1}`, key),
		fmt.Sprintf(`{"entries":{%q:1},"total":2}`, key),
		fmt.Sprintf(`{"entries":{%q:9223372036854775807,%q:1},"total":0}`, key, strings.Repeat("b", 64)),
		`{"entries":{},"total":0} {}`,
	} {
		root := t.TempDir()
		file, err := os.Create(filepath.Join(root, runtimeObjectIndexName))
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.WriteString(raw)
		if err := file.Close(); writeErr != nil || err != nil {
			t.Fatalf("write malformed cache fixture: %v %v", writeErr, err)
		}
		if _, err := runtimeReadObjectIndex(root); err == nil {
			t.Fatalf("malformed runtime object index was accepted: %s", raw)
		}
	}
}
