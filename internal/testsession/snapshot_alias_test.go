package testsession_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/testsession"
)

func TestFixtureSessionDeduplicatesPrivateInputAliases(t *testing.T) {
	directory := t.TempDir()
	compiler, err := exec.LookPath("cc")
	if err != nil {
		t.Fatal(err)
	}
	source, dispatcher := filepath.Join(directory, "dispatch.c"), filepath.Join(directory, "dispatcher")
	if err := os.WriteFile(source, []byte(`extern int printf(const char *,...);
int main(int argc,char **argv){if(argc!=1)return 2;const char *name=argv[0];for(const char *p=name;*p;p++)if(*p=='/')name=p+1;return printf("%s",name)<0;}`), os.ModePerm); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(compiler, source, "-o", dispatcher).CombinedOutput(); err != nil {
		t.Fatalf("build dispatch fixture: %v %s", err, output)
	}
	bytes, err := os.ReadFile(dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ld.lld", "ld64.lld"} {
		if err := os.Link(dispatcher, filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}
	for name, contents := range map[string]string{"compiler": "controlled compiler input", "runtime.a": "controlled runtime archive"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), os.ModePerm); err != nil {
			t.Fatal(err)
		}
	}
	identity := func(name string) map[string]string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		return map[string]string{"path": name, "sha256": hex.EncodeToString(digest[:])}
	}
	manifest := filepath.Join(directory, "toolchain.json")
	data, err := json.Marshal(map[string]any{"abi": "lo-native-3", "llvm": "21.1.8", "host": runtime.GOOS + "/" + runtime.GOARCH,
		"compiler": identity("compiler"), "targets": map[string]any{
			"linux/arm64":  map[string]any{"linker": identity("ld.lld"), "runtime": identity("runtime.a")},
			"darwin/arm64": map[string]any{"linker": identity("ld64.lld"), "runtime": identity("runtime.a")}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, data, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	frontend, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	frontendInfo, err := os.Stat(frontend)
	if err != nil {
		t.Fatal(err)
	}
	budget := int64(len(bytes)+len("controlled compiler input")+len("controlled runtime archive")) + frontendInfo.Size()
	server, err := testsession.Start(context.Background(), testsession.Config{Directory: filepath.Join(directory, "session"), Toolchain: manifest, Compiler: frontend, BuildSlots: 1, MaxBytes: budget})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	})
	inputs, err := testsession.Inputs(t.Context(), server.Address())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(inputs.Manifest)
	first, second := filepath.Join(root, "ld.lld"), filepath.Join(root, "ld64.lld")
	firstInfo, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	secondInfo, err := os.Stat(second)
	if err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !os.SameFile(firstInfo, secondInfo) || os.SameFile(firstInfo, originalInfo) {
		t.Fatal("identical tool aliases must share one private snapshot, distinct from their original source")
	}
	if stats := server.Stats(); stats.Bytes != budget {
		t.Fatalf("immutable aliases were charged more than once: %+v; expected %d", stats, budget)
	}
	if err := os.Remove(filepath.Join(directory, "ld64.lld")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "ld64.lld"), []byte("untrusted second alias"), os.ModePerm); err != nil {
		t.Fatal(err)
	}
	bad, err := testsession.Start(t.Context(), testsession.Config{Directory: filepath.Join(directory, "invalid-session"), Toolchain: manifest, Compiler: frontend, BuildSlots: 1})
	if bad != nil {
		t.Cleanup(func() {
			if err := bad.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	if err == nil || bad != nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("identical declared digest bypassed source verification: %v", err)
	}
	file, err := os.Create(dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("mutated original tool bytes"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{first, second} {
		current, err := os.ReadFile(path)
		if err != nil || string(current) != string(bytes) {
			t.Fatalf("original input mutation poisoned private snapshot: %v", err)
		}
		command := exec.Command(path)
		if runtime.GOOS == "linux" {
			command = exec.Command("/lib/ld-linux-aarch64.so.1", "--argv0", filepath.Base(path), path)
		}
		if output, err := command.CombinedOutput(); err != nil || string(output) != filepath.Base(path) {
			t.Fatalf("alias lost argv0 dispatch: %v %q", err, output)
		}
	}
}
