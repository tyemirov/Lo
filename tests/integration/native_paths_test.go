package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var controlledLLVMInputs struct {
	sync.Once
	compiler string
	tool     string
	err      error
}

func developmentToolchainCompiler(t *testing.T) string {
	t.Helper()
	compiler, _ := controlledLLVMTools(t)
	relative, err := filepath.Rel(repository, compiler)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatal("development compiler fixture leaves Lo")
	}
	data, err := os.ReadFile(compiler)
	if err != nil {
		t.Fatal(err)
	}
	record := struct {
		Purpose  string `json:"purpose"`
		Compiler string `json:"compiler"`
		SHA256   string `json:"compiler_sha256"`
	}{"custom-toolchain-admission", filepath.ToSlash(relative), fmt.Sprintf("%x", sha256.Sum256(data))}
	identity, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("LO_COMPILER_FIXTURE_IDENTITY=%s", identity)
	return compiler
}

func controlledLLVMTools(t *testing.T) (string, string) {
	t.Helper()
	if compiler := qualificationFixture(t, "development-compiler"); compiler != "" {
		return compiler, qualificationFixture(t, "native-toolchain")
	}
	controlledLLVMInputs.Do(func() {
		work, err := os.MkdirTemp(fixtureDirectory, "controlled-llvm-")
		if err != nil {
			controlledLLVMInputs.err = err
			return
		}
		controlledLLVMInputs.compiler = filepath.Join(work, "lo")
		command := exec.Command("go", "build", "-o", controlledLLVMInputs.compiler, "./cmd/lo")
		command.Dir = repository
		if output, err := command.CombinedOutput(); err != nil {
			controlledLLVMInputs.err = fmt.Errorf("build development compiler: %w: %s", err, output)
			return
		}
		controlledLLVMInputs.tool = filepath.Join(work, "tool")
		controlledLLVMInputs.err = compileLLVMControl(t, controlledLLVMInputs.tool)
	})
	if controlledLLVMInputs.err != nil {
		t.Fatal(controlledLLVMInputs.err)
	}
	return controlledLLVMInputs.compiler, controlledLLVMInputs.tool
}

func compileLLVMControl(t *testing.T, output string) error {
	t.Helper()
	manifest := os.Getenv("LO_LLVM_TOOLCHAIN")
	if manifest == "" {
		return fmt.Errorf("controlled LLVM fixture requires LO_LLVM_TOOLCHAIN")
	}
	type file struct{ Path, SHA256 string }
	type target struct {
		Triple          string
		Linker, Runtime file
		Inputs          map[string]file
		LinkArgs        []string `json:"link_args"`
	}
	var distribution struct {
		Compiler      file
		HostLibraries map[string]file `json:"host_libraries"`
		Targets       map[string]target
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &distribution); err != nil {
		return err
	}
	resolve := func(identity file) (string, error) {
		if !filepath.IsLocal(identity.Path) {
			return "", fmt.Errorf("fixture input requires a relative path")
		}
		path, err := filepath.EvalSymlinks(filepath.Join(filepath.Dir(manifest), identity.Path))
		if err != nil {
			return "", err
		}
		relative, err := filepath.Rel(repository, path)
		if err != nil || !filepath.IsLocal(relative) {
			return "", fmt.Errorf("fixture input leaves repository")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(content)
		if hex.EncodeToString(sum[:]) != identity.SHA256 {
			return "", fmt.Errorf("fixture input digest differs: %s", path)
		}
		return path, nil
	}
	compiler, err := resolve(distribution.Compiler)
	if err != nil {
		return err
	}
	selected, exists := distribution.Targets[runtime.GOOS+"/"+runtime.GOARCH]
	if !exists {
		return fmt.Errorf("fixture toolchain lacks host target")
	}
	linker, err := resolve(selected.Linker)
	if err != nil {
		return err
	}
	libraryDirectory := ""
	for _, identity := range distribution.HostLibraries {
		path, err := resolve(identity)
		if err != nil {
			return err
		}
		directory := filepath.Dir(path)
		if libraryDirectory != "" && directory != libraryDirectory {
			return fmt.Errorf("fixture host libraries require one directory")
		}
		libraryDirectory = directory
	}
	run := func(tool string, args []string) error {
		command := exec.Command(tool, args...)
		if runtime.GOOS == "linux" {
			if libraryDirectory == "" {
				return fmt.Errorf("fixture requires Linux host libraries")
			}
			loaderArgs := []string{"--inhibit-cache", "--library-path", libraryDirectory, "--argv0", tool, tool}
			command = exec.Command("/lib/ld-linux-aarch64.so.1", append(loaderArgs, args...)...)
		}
		command.Dir = filepath.Dir(output)
		command.Env = []string{"CI=1", "PATH=", "HOME=" + filepath.Dir(output), "TMPDIR=" + filepath.Dir(output)}
		if data, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("compile LLVM fixture: %w: %s", err, data)
		}
		return nil
	}
	object := output + ".o"
	compileArgs := []string{"-target", selected.Triple, "-O0", "-nostdinc", "-fno-stack-protector", "-c", filepath.Join(repository, "testdata/native_toolchain/control.c"), "-o", object}
	if runtime.GOOS == "darwin" {
		compileArgs = append(compileArgs, "-mmacosx-version-min=27.0")
	}
	if err := run(compiler, compileArgs); err != nil {
		return err
	}
	archive, err := resolve(selected.Runtime)
	if err != nil {
		return err
	}
	var arguments []string
	for _, arg := range selected.LinkArgs {
		switch arg {
		case "{object}":
			arguments = append(arguments, object)
		case "{runtime}":
			arguments = append(arguments, archive)
		case "{output}":
			arguments = append(arguments, output)
		default:
			if strings.HasPrefix(arg, "{input:") && strings.HasSuffix(arg, "}") {
				identity, exists := selected.Inputs[strings.TrimSuffix(strings.TrimPrefix(arg, "{input:"), "}")]
				if !exists {
					return fmt.Errorf("missing fixture link input")
				}
				input, err := resolve(identity)
				if err != nil {
					return err
				}
				arguments = append(arguments, input)
			} else {
				arguments = append(arguments, arg)
			}
		}
	}
	return run(linker, arguments)
}

func controlledLLVMState(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{"observed-work", "paths.json", "ready", "child-lifecycle"} {
		writeLLVMFixture(t, filepath.Join(root, name), nil)
	}
}

func controlledLLVMManifest(t *testing.T, root, tool, action string) string {
	t.Helper()
	content, err := os.ReadFile(tool)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	compilerName, linkerName := "compiler-"+action, "linker-"+action
	for _, name := range []string{compilerName, linkerName, "input"} {
		if err := os.Link(tool, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	identity := func(path string) map[string]string {
		return map[string]string{"path": path, "sha256": hex.EncodeToString(sum[:])}
	}
	host := runtime.GOOS + "/" + runtime.GOARCH
	triple := "aarch64-apple-darwin"
	names := []string{"system", "security", "corefoundation"}
	recipe := []string{"-dead_strip", "-arch", "arm64", "-platform_version", "macos", "27.0", "27.0", "-o", "{output}", "{object}", "{runtime}", "{input:system}", "{input:security}", "{input:corefoundation}"}
	if host == "linux/arm64" {
		triple = "aarch64-unknown-linux-gnu"
		names = []string{"crt1", "crti", "crtn", "libc", "libm", "libgcc", "nonshared"}
		recipe = []string{"-m", "aarch64elf", "-dynamic-linker", "/lib/ld-linux-aarch64.so.1", "-o", "{output}", "{input:crt1}", "{input:crti}", "{object}", "{runtime}", "{input:libgcc}", "{input:libm}", "{input:libc}", "{input:nonshared}", "{input:crtn}"}
	}
	inputs := map[string]any{}
	for _, name := range names {
		inputs[name] = identity("input")
	}
	manifest := map[string]any{"abi": "lo-native-2", "llvm": "21.1.8", "host": host, "compiler": identity(compilerName), "targets": map[string]any{host: map[string]any{"triple": triple, "linker": identity(linkerName), "runtime": identity("input"), "inputs": inputs, "link_args": recipe}}}
	if host == "linux/arm64" {
		selectedManifest := os.Getenv("LO_LLVM_TOOLCHAIN")
		data, err := os.ReadFile(selectedManifest)
		if err != nil {
			t.Fatal(err)
		}
		var selected struct {
			HostLibraries map[string]struct{ Path, SHA256 string } `json:"host_libraries"`
		}
		if err := json.Unmarshal(data, &selected); err != nil {
			t.Fatal(err)
		}
		libraries := map[string]any{}
		for _, name := range []string{"libstdc++.so.6", "libgcc_s.so.1", "libxml2.so.2", "libicuuc.so.72", "libicudata.so.72", "liblzma.so.5", "libz.so.1"} {
			input, exists := selected.HostLibraries[name]
			if !exists || !filepath.IsLocal(input.Path) {
				t.Fatalf("controlled fixture requires admitted host library %s", name)
			}
			source, err := filepath.EvalSymlinks(filepath.Join(filepath.Dir(selectedManifest), input.Path))
			if err != nil {
				t.Fatal(err)
			}
			contained, err := filepath.Rel(repository, source)
			if err != nil || !filepath.IsLocal(contained) {
				t.Fatal("controlled host library leaves repository")
			}
			content, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(content)
			if hex.EncodeToString(sum[:]) != input.SHA256 {
				t.Fatalf("controlled host library digest differs: %s", name)
			}
			relative := filepath.Join("host", "lib", name)
			path := filepath.Join(root, relative)
			if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(source, path); err != nil {
				t.Fatal(err)
			}
			libraries[name] = map[string]string{"path": relative, "sha256": input.SHA256}
		}
		manifest["host_libraries"] = libraries
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "toolchain.json")
	writeLLVMFixture(t, path, data)
	return path
}

func TestNativeBuildPaths(t *testing.T) {
	compiler, tool := controlledLLVMTools(t)
	state := t.TempDir()
	manifest := controlledLLVMManifest(t, state, tool, "paths")
	root, err := os.MkdirTemp(state, "build-root-")
	if err != nil {
		t.Fatal(err)
	}
	controlledLLVMState(t, root)
	command := exec.Command(compiler, "build", sourceFile(t, "return 1"), "--toolchain", manifest, "--output", filepath.Join(state, "artifact"), "--format", "json")
	command.Env = append(os.Environ(), "PATH=", "LO_BUILD_ROOT="+root, "GOFLAGS=must-not-enter", "GOCACHE=/outside", "CC=/outside", "CFLAGS=must-not-enter")
	data, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(data), "build.failed") {
		t.Fatalf("controlled toolchain: %v %s", err, data)
	}
	recorded, err := os.ReadFile(filepath.Join(root, "paths.json"))
	if err != nil {
		t.Fatal(err)
	}
	var paths map[string]string
	if err := json.Unmarshal(recorded, &paths); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"work", "HOME", "TMPDIR", "TMP", "TEMP"} {
		path := paths[key]
		relative, err := filepath.Rel(root, path)
		if err != nil || !filepath.IsAbs(path) || !filepath.IsLocal(relative) {
			t.Errorf("%s=%q leaves explicit build root %q", key, path, root)
		}
	}
	for _, key := range []string{"PATH", "GOCACHE", "GOMODCACHE", "GOPATH", "GOTOOLCHAIN", "GOWORK", "GOFLAGS", "CC", "CFLAGS", "XDG_CACHE_HOME", "CLANG_MODULE_CACHE_PATH", "CCACHE_DIR"} {
		if paths[key] != "" {
			t.Errorf("unadmitted tool environment %s=%q", key, paths[key])
		}
	}
	if paths["CI"] != "1" || paths["LC_ALL"] != "C" || paths["LANG"] != "C" || paths["ZERO_AR_DATE"] != "1" {
		t.Fatalf("tool environment: %+v", paths)
	}
	if _, err := os.Stat(paths["work"]); !os.IsNotExist(err) {
		t.Fatalf("tool work remained: %v", err)
	}
}

func TestNativeBuildRootValidation(t *testing.T) {
	compiler, _ := controlledLLVMTools(t)
	for _, scenario := range []string{"missing", "relative", "absent", "symlink-root", "symlink-work"} {
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
			case "symlink-work":
				other := filepath.Join(state, "other")
				if err := os.Mkdir(other, os.ModePerm); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, filepath.Join(root, "work")); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command(compiler, "build", sourceFile(t, "return 1"), "--toolchain", os.Getenv("LO_LLVM_TOOLCHAIN"), "--output", filepath.Join(state, "artifact"), "--format", "json")
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
			if scenario == "symlink-work" {
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

func controlledLLVMFailures(t *testing.T) {
	t.Helper()
	compiler, tool := controlledLLVMTools(t)
	for _, action := range []string{"cancel", "overflow", "failure", "artifact-overflow"} {
		t.Run(action, func(t *testing.T) {
			state := t.TempDir()
			manifest := controlledLLVMManifest(t, state, tool, action)
			root, err := os.MkdirTemp(state, "build-root-")
			if err != nil {
				t.Fatal(err)
			}
			controlledLLVMState(t, root)
			artifact := filepath.Join(state, "artifact")
			writeLLVMFixture(t, artifact, []byte("previous"))
			command := exec.Command(compiler, "build", sourceFile(t, "return 1"), "--toolchain", manifest, "--output", artifact, "--format", "json")
			command.Env = append(os.Environ(), "PATH=", "LO_BUILD_ROOT="+root)
			var stderr bytes.Buffer
			command.Stderr = &stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			if action == "cancel" {
				deadline := time.NewTimer(5 * time.Second)
				defer deadline.Stop()
				ticker := time.NewTicker(5 * time.Millisecond)
				defer ticker.Stop()
			ready:
				for {
					select {
					case err := <-done:
						t.Fatalf("tool exited before cancellation: %v %s", err, &stderr)
					case <-deadline.C:
						_ = command.Process.Kill()
						t.Fatal("controlled tool did not start")
					case <-ticker.C:
						if data, err := os.ReadFile(filepath.Join(root, "ready")); err == nil && string(data) == "ready" {
							break ready
						}
					}
				}
				if err := command.Process.Signal(os.Interrupt); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("build unexpectedly succeeded")
				}
			case <-time.After(5 * time.Second):
				_ = command.Process.Kill()
				t.Fatal("controlled build did not stop")
			}
			code := "build.cancelled"
			if action == "failure" {
				code = "build.failed"
			}
			if strings.HasSuffix(action, "overflow") {
				code = "build.limit"
			}
			diagnostic := diagnostics(t, stderr.String()).Diagnostics[0]
			if diagnostic.Code != code {
				t.Fatalf("diagnostic=%s, want %s: %s", diagnostic.Code, code, &stderr)
			}
			message := map[string]string{"overflow": "tool diagnostics exceed", "failure": "controlled compiler failure", "artifact-overflow": "artifact exceeds"}[action]
			if message != "" && !strings.Contains(diagnostic.Message, message) {
				t.Fatalf("controlled failure cause missing %q: %s", message, &stderr)
			}
			data, err := os.ReadFile(artifact)
			if err != nil || string(data) != "previous" {
				t.Fatalf("failed build replaced output: %q %v", data, err)
			}
			work, err := os.ReadFile(filepath.Join(root, "observed-work"))
			if err != nil {
				t.Fatal(err)
			}
			relative, err := filepath.Rel(root, string(work))
			if len(work) == 0 || err != nil || !filepath.IsLocal(relative) {
				t.Fatalf("controlled compiler execution witness is invalid: %q %v", work, err)
			}
			if _, err := os.Stat(string(work)); !os.IsNotExist(err) {
				t.Fatalf("build work remained: %s %v", work, err)
			}
			entries, err := os.ReadDir(state)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".lo-llvm-") {
					t.Fatal("staged artifact remained")
				}
			}
			if action == "cancel" {
				time.Sleep(800 * time.Millisecond)
				if observed, err := os.ReadFile(filepath.Join(root, "child-lifecycle")); err != nil || len(observed) != 0 {
					t.Fatalf("compiler descendant survived cancellation: %q %v", observed, err)
				}
			}
		})
	}
}
