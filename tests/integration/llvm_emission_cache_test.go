package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const runtimeEmissionCacheBytes = 64 << 20

func runtimeCachedLLVMEmission(t *testing.T, compilerIdentity, manifestIdentity, source, output string, arguments []string) {
	t.Helper()
	admitted, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	verify := func() {
		t.Helper()
		observed, err := os.ReadFile(source)
		if err != nil || !bytes.Equal(admitted, observed) {
			t.Fatalf("immutable LLVM source changed during emission: %s: %v", source, err)
		}
	}
	canonical := append([]string{}, arguments...)
	for index, argument := range canonical {
		if argument == output {
			canonical[index] = "{output}"
		}
	}
	encoded, err := json.Marshal([]any{binary, compilerIdentity, manifestIdentity, source, admitted, canonical})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(encoded)
	key := hex.EncodeToString(sum[:])
	root := filepath.Join(runtimeCacheRoot(t), "llvm-emission")
	runtimeCachedArtifact(t, root, key, runtimeEmissionCacheBytes, "LO_LLVM_EMISSION", func(directory, staged string) {
		verify()
		selected := append([]string{}, canonical...)
		for index, argument := range selected {
			if argument == "{output}" {
				selected[index] = staged
			}
		}
		emitted := runExecutable(t, binary, selected...)
		verify()
		if emitted.status != 0 {
			t.Fatalf("emit LLVM: %+v", emitted)
		}
	}, func(cached string) error {
		input, err := os.Open(cached)
		if err != nil {
			return err
		}
		private, err := os.Create(output)
		if err != nil {
			return errors.Join(err, input.Close())
		}
		_, copyErr := io.Copy(private, input)
		return errors.Join(copyErr, private.Close(), input.Close())
	})
	verify()
}

func llvmRecordingCompiler(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	record := filepath.Join(directory, "emissions")
	log, err := os.Create(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`package main
import("fmt";"os";"syscall")
func main(){
 for index,argument:=range os.Args[1:]{
  if argument=="--emit" && index+2<len(os.Args) && os.Args[index+2]=="llvm" {
   file,err:=os.OpenFile(%q,os.O_APPEND|os.O_WRONLY,0);if err!=nil{panic(err)}
   if _,err=fmt.Fprintln(file,"emit");err!=nil{panic(err)};if err=file.Close();err!=nil{panic(err)}
  }
 }
 if err:=syscall.Exec(%q,append([]string{%q},os.Args[1:]...),os.Environ());err!=nil{panic(err)}
}`, record, binary, binary)
	input := filepath.Join(directory, "compiler.go")
	file, err := os.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString(source)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("write recording compiler: %v, %v", writeErr, closeErr)
	}
	wrapper := filepath.Join(directory, "compiler")
	command := exec.Command("go", "build", "-o", wrapper, input)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build recording compiler: %v %s", err, output)
	}
	original := binary
	binary = wrapper
	t.Cleanup(func() { binary = original })
	return record
}

func llvmEmissionCount(t *testing.T, record string) int {
	t.Helper()
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "emit\n")
}

func TestLLVMRuntimeEmissionReuse(t *testing.T) {
	t.Setenv("LO_TEST_SESSION_DIRECTORY", t.TempDir())
	record := llvmRecordingCompiler(t)
	const source = "return Math.add(17, 23)"
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{
		{name: "private-transform", source: source, transform: func(t *testing.T, contents []byte) []byte {
			contents[2] = 'X'
			return contents
		}},
		{name: "unmodified-repeat", source: source, transform: func(t *testing.T, contents []byte) []byte {
			if contents[2] != 'L' {
				t.Fatal("a previous scenario changed the cached LLVM emission")
			}
			return contents
		}},
	})
	if count := llvmEmissionCount(t, record); count != 1 {
		t.Fatalf("identical immutable source must emit once before private transforms, got %d emissions", count)
	}
}

func TestLLVMRuntimeEmissionIdentity(t *testing.T) {
	t.Setenv("LO_TEST_SESSION_DIRECTORY", t.TempDir())
	record := llvmRecordingCompiler(t)
	compilerIdentity := runtimeToolIdentity(t, binary)
	manifestIdentity := runtimeToolIdentity(t, os.Getenv("LO_LLVM_TOOLCHAIN"))
	source := sourceFile(t, "return Math.add(17, 23)")
	emit := func(path, target string, extra ...string) []byte {
		t.Helper()
		output := filepath.Join(t.TempDir(), "program.ll")
		arguments := []string{"build", path, "--emit", "llvm", "--target", target, "--output", output, "--format", "json"}
		arguments = append(arguments, extra...)
		runtimeCachedLLVMEmission(t, compilerIdentity, manifestIdentity, path, output, arguments)
		contents, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(contents, []byte("source_filename = \""+path+"\"")) {
			t.Fatalf("cached LLVM lost the original source identity %q", path)
		}
		return contents
	}
	first := emit(source, "darwin/arm64")
	if !bytes.Equal(first, emit(source, "darwin/arm64")) || llvmEmissionCount(t, record) != 1 {
		t.Fatal("identical frontend inputs did not reuse the exact LLVM emission")
	}
	linux := emit(source, "linux/arm64")
	if bytes.Equal(first, linux) || !bytes.Contains(linux, []byte("aarch64-unknown-linux-gnu")) {
		t.Fatal("changed target reused the previous LLVM emission")
	}
	emit(source, "darwin/arm64", "--device-backend", "metal")
	lexical := filepath.Dir(source) + "/./" + filepath.Base(source)
	emit(lexical, "darwin/arm64")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString("return Math.add(19, 23)")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("change private source input: %v, %v", writeErr, closeErr)
	}
	emit(source, "darwin/arm64")
	manifestIdentity = "different-verified-manifest"
	emit(source, "darwin/arm64")
	if count := llvmEmissionCount(t, record); count != 6 {
		t.Fatalf("target, backend, lexical path, source bytes, and manifest must each invalidate frontend reuse: got %d emissions", count)
	}
}
