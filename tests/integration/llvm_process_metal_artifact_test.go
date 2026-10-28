package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLLVMProcessMetalArtifact(t *testing.T) {
	fixture := newCleanupFixture(t)
	data, err := os.ReadFile(os.Getenv("LO_LLVM_TOOLCHAIN"))
	if err != nil {
		t.Fatal(err)
	}
	var toolchain struct {
		Metal json.RawMessage `json:"metal"`
	}
	if err := json.Unmarshal(data, &toolchain); err != nil {
		t.Fatal(err)
	}
	if len(toolchain.Metal) == 0 {
		t.Skip("requires an explicit Metal runtime bundle")
	}
	t.Setenv("LO_F024_METAL_CONTEXT", "inherited")
	path := fileFixture(t, fixture.writes, "data", "before")
	source := sourceFile(t, processContextProgram("String", fmt.Sprintf(`initial = Process.Context.current(); context = Process.Context.with_directory(initial, File.Path.new(%q)); inherited = Process.Context.environment_value(context, "LO_F024_METAL_CONTEXT"); executable = Process.Executable.new(File.Path.new(%q)); command = Process.Command.new(context, executable, List.new("write", %q), Bytes.new("")); result = commit command; return inherited`, fixture.root, fixture.executable, path)))
	artifact := filepath.Join(t.TempDir(), "metal-process")
	built := runExecutable(t, binary, "build", source, "--output", artifact, "--device-backend", "metal", "--format", "json")
	if built.status != 0 {
		t.Fatalf("build Metal Process artifact: %+v", built)
	}
	metadata := runExecutable(t, artifact, "metadata")
	var description struct {
		Backend string `json:"device_backend"`
	}
	if metadata.status != 0 || json.Unmarshal([]byte(metadata.stdout), &description) != nil || description.Backend != "metal" {
		t.Fatalf("artifact backend: %+v", metadata)
	}
	flags := append(fixture.flags(), "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes))
	got := runExecutable(t, artifact, append([]string{"run", "--format", "json"}, flags...)...)
	valueJSON(t, fileOutput(t, got), `"inherited"`)
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "changed" {
		t.Fatalf("Metal artifact Process scope: %q %v", data, err)
	}
}
