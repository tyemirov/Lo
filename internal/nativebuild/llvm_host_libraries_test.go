package nativebuild

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativeplan"
)

func TestLLVMLinuxHostLibrarySnapshots(t *testing.T) {
	if runtime.GOOS+"/"+runtime.GOARCH != "linux/arm64" {
		t.Skip("requires Linux arm64 host")
	}
	root := snapshotTestDirectory(t)
	input := filepath.Join(root, "input")
	if err := writeFile(input, []byte("admitted inputs")); err != nil {
		t.Fatal(err)
	}
	pin := llvmFile{Path: "input", SHA256: snapshotTestDigest(t, input)}
	arguments, names := llvmLinkRecipe("linux/arm64", device.SoftwareBuild)
	inputs := map[string]llvmFile{}
	for _, name := range names {
		inputs[name] = pin
	}
	libraries := map[string]llvmFile{}
	for _, name := range []string{"libstdc++.so.6", "libgcc_s.so.1", "libxml2.so.2", "libicuuc.so.72", "libicudata.so.72", "liblzma.so.5", "libz.so.1"} {
		relative := filepath.Join("host", "lib", name)
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
			t.Fatal(err)
		}
		if err := writeFile(path, []byte("admitted "+name)); err != nil {
			t.Fatal(err)
		}
		libraries[name] = llvmFile{Path: relative, SHA256: snapshotTestDigest(t, path)}
	}
	distribution := llvmDistribution{ABI: nativeplan.ABIIdentity, LLVM: llvmVersion, Host: "linux/arm64", Compiler: pin, HostLibraries: libraries, Targets: map[string]llvmTarget{"linux/arm64": {Triple: "aarch64-unknown-linux-gnu", Linker: pin, Runtime: pin, Inputs: inputs, LinkArgs: arguments}}}
	data, err := json.Marshal(distribution)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "toolchain.json")
	if err := writeFile(manifest, data); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(root, "snapshots")
	admitted, err := admitLLVM(context.Background(), manifest, "linux/arm64", snapshot, device.SoftwareBuild)
	if err != nil {
		t.Fatal(err)
	}
	if admitted.hostLibraries != filepath.Join(snapshot, "host", "lib") {
		t.Fatalf("loader directory: %q", admitted.hostLibraries)
	}
	for name, identity := range libraries {
		if err := writeFile(filepath.Join(root, identity.Path), []byte("mutated installation")); err != nil {
			t.Fatal(err)
		}
		if got := snapshotTestDigest(t, filepath.Join(admitted.hostLibraries, name)); got != identity.SHA256 {
			t.Fatalf("host library %s changed with installation", name)
		}
	}
	for name, identity := range libraries {
		if err := writeFile(filepath.Join(root, identity.Path), []byte("admitted "+name)); err != nil {
			t.Fatal(err)
		}
	}
	distribution.HostLibraries["libz.so.1"] = llvmFile{Path: "input", SHA256: pin.SHA256}
	data, err = json.Marshal(distribution)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFile(manifest, data); err != nil {
		t.Fatal(err)
	}
	_, err = admitLLVM(context.Background(), manifest, "linux/arm64", filepath.Join(root, "rejected"), device.SoftwareBuild)
	if err == nil || !strings.Contains(err.Error(), "requires path host/lib/") {
		t.Fatalf("host library alias admission: %v", err)
	}
}
