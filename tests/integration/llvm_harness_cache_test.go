package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/testsession"
	"golang.org/x/sys/unix"
)

func TestLLVMRuntimeObjectCacheInvalidation(t *testing.T) {
	t.Setenv("LO_TEST_SESSION_DIRECTORY", t.TempDir())
	tool, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	var builds atomic.Int32
	build := func(directory string, arguments []string) {
		builds.Add(1)
		command := exec.Command(tool, arguments...)
		command.Dir = directory
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("compile cache fixture: %v\n%s", err, output)
		}
	}
	compile := func(source string, flags ...string) []byte {
		input := sourceFile(t, source)
		output := filepath.Join(t.TempDir(), "package.a")
		arguments := append([]string{"tool", "compile"}, flags...)
		arguments = append(arguments, "-o", output, input)
		runtimeCachedObject(t, tool, "go-fixture", input, output, arguments, build)
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	first := compile("package fixture\nconst Value = 1\n")
	second := compile("package fixture\nconst Value = 1\n")
	if builds.Load() != 1 || !bytes.Equal(first, second) {
		t.Fatalf("identical inputs require one build: builds=%d", builds.Load())
	}
	changed := compile("package fixture\nconst Value = 2\n")
	if builds.Load() != 2 || bytes.Equal(first, changed) {
		t.Fatalf("changed source must require a new object: builds=%d", builds.Load())
	}
	compile("package fixture\nconst Value = 2\n", "-N")
	if builds.Load() != 3 {
		t.Fatalf("changed compiler flags must require a new object: builds=%d", builds.Load())
	}
}

func TestLLVMRuntimeObjectCacheConcurrentPublication(t *testing.T) {
	t.Setenv("LO_TEST_SESSION_DIRECTORY", t.TempDir())
	tool, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	input := sourceFile(t, "package fixture\nconst Value = 3\n")
	var builds atomic.Int32
	var callers sync.WaitGroup
	for range 8 {
		output := filepath.Join(t.TempDir(), "package.a")
		callers.Add(1)
		go func() {
			defer callers.Done()
			runtimeCachedObject(t, tool, "go-fixture", input, output, []string{"tool", "compile", "-o", output, input}, func(directory string, arguments []string) {
				builds.Add(1)
				command := exec.Command(tool, arguments...)
				command.Dir = directory
				if output, err := command.CombinedOutput(); err != nil {
					t.Errorf("compile concurrent cache fixture: %v\n%s", err, output)
				}
			})
			data, err := os.ReadFile(output)
			if err != nil || len(data) == 0 {
				t.Errorf("completed object missing: bytes=%d err=%v", len(data), err)
			}
		}()
	}
	callers.Wait()
	if builds.Load() != 1 {
		t.Fatalf("concurrent callers require one completed build, got %d", builds.Load())
	}
}

const runtimeObjectCacheBytes = 256 << 20

func TestLLVMRuntimeSessionInputsAndObjectReuse(t *testing.T) {
	manifest, err := filepath.Abs(os.Getenv("LO_LLVM_TOOLCHAIN"))
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "session")
	server, err := testsession.Start(context.Background(), testsession.Config{
		Directory: directory, Toolchain: manifest, Compiler: binary, BuildSlots: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	})
	t.Setenv("LO_TEST_SESSION", server.Address())
	t.Setenv("LO_TEST_SESSION_DIRECTORY", directory)
	const source = "return Math.add(1, 2)"
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{
		{name: "first", source: source, number: 0},
		{name: "same-program-new-process", source: source, number: 0},
	})
	objects, err := os.ReadDir(filepath.Join(runtimeCacheRoot(t), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 3 {
		t.Fatalf("the repeated native scenario must reuse its module, entry, and harness objects, got %d entries", len(objects))
	}
}

var runtimeHeaderIdentity struct {
	sync.Once
	digest string
	err    error
}

func runtimeCacheRoot(t *testing.T) string {
	t.Helper()
	root := os.Getenv("LO_TEST_SESSION_DIRECTORY")
	if root == "" {
		root = fixtureDirectory
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if relative, err := filepath.Rel(repository, root); err != nil || !filepath.IsLocal(relative) {
		t.Fatal("runtime cache must stay inside Lo")
	}
	root = filepath.Join(root, "runtime-cache")
	if err := os.MkdirAll(root, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	return root
}

func runtimeImmutableSource(t *testing.T, source string) string {
	t.Helper()
	if address := os.Getenv("LO_TEST_SESSION"); address != "" {
		path, err := testsession.Source(context.Background(), address, source)
		if err != nil {
			t.Fatal(err)
		}
		return path
	}
	root := runtimeCacheRoot(t)
	sum := sha256.Sum256([]byte(source))
	path := filepath.Join(root, hex.EncodeToString(sum[:])+".lo")
	lock := runtimeCacheLock(t, root, hex.EncodeToString(sum[:]))
	defer lock()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.WriteString(source)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatalf("write immutable runtime source: %v, %v", writeErr, closeErr)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	return path
}

func runtimeCacheLock(t *testing.T, root, key string) func() {
	t.Helper()
	directory := filepath.Join(root, "locks")
	if err := os.MkdirAll(directory, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(directory, key), os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	return func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}
}

func runtimeHeadersDigest(t *testing.T) string {
	t.Helper()
	runtimeHeaderIdentity.Do(func() {
		hasher := sha256.New()
		for _, directory := range []string{"runtime/native/include", "testdata/native-runtime"} {
			err := filepath.WalkDir(filepath.Join(repository, directory), func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() {
					return nil
				}
				if !strings.HasSuffix(path, ".h") {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				fmt.Fprintf(hasher, "%s\x00%d\x00", path, len(data))
				_, err = hasher.Write(data)
				return err
			})
			if err != nil {
				runtimeHeaderIdentity.err = err
				return
			}
		}
		runtimeHeaderIdentity.digest = hex.EncodeToString(hasher.Sum(nil))
	})
	if runtimeHeaderIdentity.err != nil {
		t.Fatal(runtimeHeaderIdentity.err)
	}
	return runtimeHeaderIdentity.digest
}

// runtimeCachedObject shares only completed objects from immutable generated inputs.
func runtimeCachedObject(t *testing.T, tool, identity, source, output string, arguments []string, build func(string, []string)) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	canonicalArguments := append([]string{}, arguments...)
	for index, argument := range canonicalArguments {
		switch argument {
		case source:
			canonicalArguments[index] = "{source}"
		case output:
			canonicalArguments[index] = "{output}"
		}
	}
	encoded, err := json.Marshal([]any{tool, identity, runtimeHeadersDigest(t), filepath.Ext(source), canonicalArguments, data})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(encoded)
	key := hex.EncodeToString(sum[:])
	runtimeCachedArtifact(t, runtimeCacheRoot(t), key, runtimeObjectCacheBytes, "LO_RUNTIME_OBJECT", func(directory, staged string) {
		input := filepath.Join(directory, "source"+filepath.Ext(source))
		file, err := os.Create(input)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatalf("write runtime compiler input: %v, %v", writeErr, closeErr)
		}
		commandArguments := append([]string{}, canonicalArguments...)
		for index, argument := range commandArguments {
			switch argument {
			case "{source}":
				commandArguments[index] = input
			case "{output}":
				commandArguments[index] = staged
			}
		}
		build(directory, commandArguments)
	}, func(cached string) error { return os.Link(cached, output) })
}

// The builder and publication both run while the completed artifact is retained.
func runtimeCachedArtifact(t *testing.T, root, key string, limit int64, marker string, build func(string, string), publish func(string) error) {
	t.Helper()
	unlock := runtimeCacheLock(t, root, key)
	defer unlock()
	directory := filepath.Join(root, "objects", key)
	cached := filepath.Join(directory, "object.o")
	if _, err := os.Stat(cached); err == nil {
		runtimeRequireObjectCacheEntry(t, root, key)
		if err := publish(cached); err != nil {
			t.Fatalf("reuse runtime artifact: %v", err)
		}
		t.Logf("%s=reused:%s", marker, key)
		return
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(directory, "staged.o")
	defer func() {
		if err := os.Remove(staged); err != nil && !os.IsNotExist(err) {
			t.Error(err)
		}
	}()
	build(directory, staged)
	if err := publish(staged); err != nil {
		t.Fatalf("publish private runtime artifact: %v", err)
	}
	if err := os.Rename(staged, cached); err != nil {
		t.Fatalf("publish reusable runtime object: %v", err)
	}
	t.Logf("%s=built:%s", marker, key)
	runtimeLimitArtifactCache(t, root, key, limit)
}

func runtimeLimitArtifactCache(t *testing.T, root, active string, limit int64) {
	t.Helper()
	unlock := runtimeCacheLock(t, root, "storage")
	defer unlock()
	if err := runtimeRegisterObjectCache(root, active, limit); err != nil {
		t.Fatalf("account runtime object cache: %v", err)
	}
}

func runtimeToolIdentity(t *testing.T, path string) string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	hasher := sha256.New()
	_, readErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("hash runtime compiler: %v, %v", readErr, closeErr)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}
