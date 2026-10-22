package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativeplan"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInstalledCompilerIdentity(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "original")
	if err := os.MkdirAll(filepath.Join(original, "bin"), os.ModePerm); err != nil {
		t.Fatal(err)
	}
	manifest := []byte("{}\n")
	sum := sha256.Sum256(manifest)
	executable := filepath.Join(original, "bin", "lo")
	command := exec.Command("go", "build", "-ldflags", "-X main.installedToolchainSHA256="+hex.EncodeToString(sum[:]), "-o", executable, "./cmd/lo")
	command.Dir = repository
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build installed frontend: %v: %s", err, data)
	}
	relocated := filepath.Join(root, "relocated")
	if err := os.Rename(original, relocated); err != nil {
		t.Fatal(err)
	}
	executable = filepath.Join(relocated, "bin", "lo")
	alias := filepath.Join(root, "lo")
	if err := os.Symlink(executable, alias); err != nil {
		t.Fatal(err)
	}
	input := sourceFile(t, "return 17")
	output := filepath.Join(root, "program")
	for _, scenario := range []struct {
		name     string
		contents []byte
		message  string
	}{
		{"missing", nil, "read toolchain manifest"},
		{"corrupt", []byte("tampered"), "toolchain manifest SHA-256 mismatch"},
		{"identity", manifest, "toolchain ABI, LLVM version, or host identity does not match"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if scenario.contents != nil {
				if err := os.WriteFile(filepath.Join(relocated, "toolchain.json"), scenario.contents, os.ModePerm); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(output, []byte("preserve existing output"), os.ModePerm); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(alias, "build", input, "--output", output, "--format", "json")
			command.Dir = root
			command.Env = append(os.Environ(), "PATH=")
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			err := command.Run()
			if err == nil || stdout.Len() != 0 || !strings.Contains(stderr.String(), scenario.message) {
				t.Fatalf("installed admission: %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
			}
			got, err := os.ReadFile(output)
			if err != nil || string(got) != "preserve existing output" {
				t.Fatalf("output was replaced: %q %v", got, err)
			}
		})
	}
	for _, args := range [][]string{{"check", input}, {"run", input}, {"build", input, "--emit", "llvm", "--output", filepath.Join(root, "program.ll")}} {
		got := runExecutable(t, alias, args...)
		if got.status != 0 {
			t.Fatalf("installed non-executable command: %+v", got)
		}
	}
	got := runExecutable(t, alias, "build", input, "--output", output, "--toolchain", filepath.Join(root, "other.json"))
	if got.status != 2 || !strings.Contains(got.stderr, "installed compiler uses its own toolchain") {
		t.Fatalf("override: %+v", got)
	}
}

func TestDistributionCompilerRejectsUnadmittedInputs(t *testing.T) {
	host := runtime.GOOS + "/" + runtime.GOARCH
	compiler := developmentToolchainCompiler(t)
	triple := "aarch64-apple-darwin"
	linkArgs := []string{"-dead_strip", "-arch", "arm64", "-platform_version", "macos", "27.0", "27.0", "-o", "{output}", "{object}", "{runtime}", "{input:system}", "{input:security}", "{input:corefoundation}"}
	names := []string{"system", "security", "corefoundation"}
	if host == "linux/arm64" {
		triple = "aarch64-unknown-linux-gnu"
		linkArgs = []string{"-m", "aarch64elf", "-dynamic-linker", "/lib/ld-linux-aarch64.so.1", "-o", "{output}", "{input:crt1}", "{input:crti}", "{object}", "{runtime}", "{input:libgcc}", "{input:libm}", "{input:libc}", "{input:nonshared}", "{input:crtn}"}
		names = []string{"crt1", "crti", "crtn", "libc", "libm", "libgcc", "nonshared"}
	}
	root := t.TempDir()
	content := []byte("admission fixture")
	sum := sha256.Sum256(content)
	identity := map[string]any{"path": "input", "sha256": hex.EncodeToString(sum[:])}
	writeLLVMFixture(t, filepath.Join(root, "input"), content)
	inputs := map[string]any{}
	for _, name := range names {
		inputs[name] = identity
	}
	base := map[string]any{"abi": nativeplan.ABIIdentity, "llvm": "21.1.8", "host": host, "compiler": identity, "targets": map[string]any{host: map[string]any{"triple": triple, "linker": identity, "runtime": identity, "inputs": inputs, "link_args": linkArgs}}}
	if host == "linux/arm64" {
		libraries := map[string]any{}
		for _, name := range []string{"libstdc++.so.6", "libgcc_s.so.1", "libxml2.so.2", "libicuuc.so.72", "libicudata.so.72", "liblzma.so.5", "libz.so.1"} {
			libraries[name] = map[string]any{"path": "host/lib/" + name, "sha256": hex.EncodeToString(sum[:])}
		}
		base["host_libraries"] = libraries
	}
	original, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	input := sourceFile(t, "return 1")
	outside := filepath.Join(t.TempDir(), "outside")
	writeLLVMFixture(t, outside, content)
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name, message string
		edit          func(map[string]any)
	}{
		{"host", "identity does not match", func(m map[string]any) { m["host"] = "wrong/host" }},
		{"abi", "identity does not match", func(m map[string]any) { m["abi"] = "wrong-abi" }},
		{"escaped", "escapes the toolchain directory", func(m map[string]any) { m["compiler"].(map[string]any)["path"] = "escape" }},
		{"corrupt", "SHA-256 mismatch", func(m map[string]any) { m["compiler"].(map[string]any)["sha256"] = strings.Repeat("0", 64) }},
		{"host-library-set", "exact host library set", func(m map[string]any) { m["host_libraries"] = map[string]any{"unexpected.so": identity} }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var manifest map[string]any
			if err := json.Unmarshal(original, &manifest); err != nil {
				t.Fatal(err)
			}
			scenario.edit(manifest)
			data, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "toolchain.json")
			writeLLVMFixture(t, path, data)
			output := filepath.Join(root, "artifact")
			writeLLVMFixture(t, output, []byte("previous artifact"))
			got := runExecutable(t, compiler, "build", input, "--toolchain", path, "--output", output)
			if got.status != 2 || !strings.Contains(got.stderr, scenario.message) {
				t.Fatalf("admission: %+v", got)
			}
			preserved, err := os.ReadFile(output)
			if err != nil || string(preserved) != "previous artifact" {
				t.Fatalf("output changed: %q %v", preserved, err)
			}
		})
	}
}

func TestInstalledCompilerExecutionCleanup(t *testing.T) {
	if runtime.GOOS+"/"+runtime.GOARCH != "darwin/arm64" {
		t.Skip("controlled compiler fixture uses the Darwin executable snapshot lane")
	}
	root := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	helperSource := filepath.Join(root, "compiler.go")
	writeLLVMFixture(t, helperSource, []byte(`package main
import("net";"os";"io")
var witness string
func main(){
 connection,err:=net.Dial("tcp",witness);if err!=nil{os.Exit(31)}
 defer connection.Close()
 if _,err=io.WriteString(connection,"admitted compiler execution\n");err!=nil{os.Exit(32)}
 var command [1]byte
 if _,err=connection.Read(command[:]);err!=nil{os.Exit(33)}
 os.Exit(42)
}
`))
	install := filepath.Join(root, "installation")
	if err := os.MkdirAll(filepath.Join(install, "bin"), os.ModePerm); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(install, "compiler")
	command := exec.Command("go", "build", "-ldflags", "-X main.witness="+listener.Addr().String(), "-o", helper, helperSource)
	command.Dir = repository
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build controlled compiler: %v %s", err, data)
	}
	helperBytes, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	helperSum := sha256.Sum256(helperBytes)
	identity := map[string]any{"path": "compiler", "sha256": hex.EncodeToString(helperSum[:])}
	inputs := map[string]any{"system": identity, "security": identity, "corefoundation": identity}
	manifest := map[string]any{"abi": nativeplan.ABIIdentity, "llvm": "21.1.8", "host": "darwin/arm64", "compiler": identity, "targets": map[string]any{"darwin/arm64": map[string]any{
		"triple": "aarch64-apple-darwin", "linker": identity, "runtime": identity, "inputs": inputs,
		"link_args": []string{"-dead_strip", "-arch", "arm64", "-platform_version", "macos", "27.0", "27.0", "-o", "{output}", "{object}", "{runtime}", "{input:system}", "{input:security}", "{input:corefoundation}"},
	}}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeLLVMFixture(t, filepath.Join(install, "toolchain.json"), data)
	manifestSum := sha256.Sum256(data)
	frontend := filepath.Join(install, "bin", "lo")
	command = exec.Command("go", "build", "-ldflags", "-X main.installedToolchainSHA256="+hex.EncodeToString(manifestSum[:]), "-o", frontend, "./cmd/lo")
	command.Dir = repository
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build installed frontend: %v %s", err, data)
	}
	input := sourceFile(t, "return 23")
	for _, scenario := range []struct {
		name, code string
		cancel     bool
	}{{"tool-failure", "build.failed", false}, {"cancelled", "build.cancelled", true}} {
		t.Run(scenario.name, func(t *testing.T) {
			workspace := t.TempDir()
			buildRoot := filepath.Join(workspace, "state")
			if err := os.MkdirAll(buildRoot, os.ModePerm); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(workspace, "program")
			writeLLVMFixture(t, output, []byte("preserve existing output"))
			ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
			defer stop()
			command := exec.CommandContext(ctx, frontend, "build", input, "--output", output, "--format", "json")
			command.Env = append(os.Environ(), "PATH=", "LO_BUILD_ROOT="+buildRoot)
			command.Dir = workspace
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			accepted := make(chan net.Conn, 1)
			go func() {
				connection, err := listener.Accept()
				if err == nil {
					accepted <- connection
				}
			}()
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			var connection net.Conn
			select {
			case connection = <-accepted:
			case err := <-done:
				t.Fatalf("compiler exited before execution witness: %v %s", err, stderr.String())
			case <-ctx.Done():
				t.Fatal("controlled compiler did not connect before deadline")
			}
			defer connection.Close()
			if err := connection.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatal(err)
			}
			witness, err := bufio.NewReader(connection).ReadString('\n')
			if err != nil || witness != "admitted compiler execution\n" {
				t.Fatalf("execution witness: %q %v", witness, err)
			}
			if scenario.cancel {
				if err := command.Process.Signal(os.Interrupt); err != nil {
					t.Fatal(err)
				}
			} else if _, err := connection.Write([]byte("f")); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("controlled compiler unexpectedly succeeded")
				}
			case <-ctx.Done():
				t.Fatal("installed compiler did not stop after execution outcome")
			}
			if command.ProcessState.ExitCode() != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"code":"`+scenario.code+`"`) {
				t.Fatalf("execution outcome: %s stdout=%s stderr=%s", command.ProcessState, stdout.String(), stderr.String())
			}
			preserved, err := os.ReadFile(output)
			if err != nil || string(preserved) != "preserve existing output" {
				t.Fatalf("output changed: %q %v", preserved, err)
			}
			assertDistributionBuildClean(t, workspace, buildRoot)
		})
	}
}

func assertDistributionBuildClean(t *testing.T, outputDirectory, buildRoot string) {
	t.Helper()
	leftovers, err := filepath.Glob(filepath.Join(outputDirectory, ".lo-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("staged output remains: %v %v", leftovers, err)
	}
	entries, err := os.ReadDir(filepath.Join(buildRoot, "work"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("build workspace remains: %v %v", entries, err)
	}
}
