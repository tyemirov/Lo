// Command metalbuild builds the explicitly selected Metal compiler and records its inputs.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/metalconfig"
)

const (
	libraryPath = metalconfig.LibraryPath
	kernelPath  = metalconfig.KernelPath
	binaryPath  = "bin/lo-metal"
	receiptPath = "bin/lo-metal-build.json"
)

// This manifest lists Metal source inputs and kernel compiler options.
var sources = []string{
	"internal/device/metal_darwin.go", "internal/device/metal_bridge.h",
	"internal/device/metal_bridge_darwin.m", "internal/device/metal_types.h", kernelPath, "tools/metalbuild/main.go", "internal/metalconfig/config.go", "tools/runtimebundle/main.go",
}
var metalFlags = metalconfig.KernelFlags()
var goFlags = []string{"build", "-trimpath", "-tags=metal"}

type receipt struct {
	Target         string            `json:"target"`
	Profile        string            `json:"profile"`
	DeviceFeatures []string          `json:"device_features"`
	Versions       map[string]string `json:"versions"`
	Sources        map[string]string `json:"source_sha256"`
	MetalFlags     []string          `json:"metal_flags"`
	GoFlags        []string          `json:"go_flags"`
	Library        string            `json:"metallib_sha256"`
	Binary         string            `json:"binary_sha256"`
}

func main() {
	test := flag.String("test", "", "run Metal integration tests that match this expression")
	flag.Parse()
	if err := run(*test, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(testPattern string, testArgs []string) (result error) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return fmt.Errorf("build Metal: require a darwin/arm64 host")
	}
	if runtime.Version() != "go1.27.1" {
		return fmt.Errorf("build Metal: require Go 1.27.1, got %s", runtime.Version())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	r := receipt{Target: "darwin/arm64", Profile: device.MetalProfile, DeviceFeatures: []string{"Apple7", "unified memory"}, Versions: map[string]string{"go": runtime.Version()}, Sources: map[string]string{}, MetalFlags: metalFlags, GoFlags: goFlags}
	probe := func(args ...string) (string, error) { return command(ctx, nil, args...) }
	settings, err := metalconfig.NativeEnvironment(probe)
	if err != nil {
		return err
	}
	if err := metalconfig.CheckKernelTools(probe); err != nil {
		return err
	}
	identity := metalconfig.PinnedIdentity()
	r.Versions["xcode"], r.Versions["sdk"], r.Versions["clang"] = identity.Xcode, identity.SDK, identity.Clang
	r.Versions["metal"], r.Versions["metallib"] = identity.Metal, identity.Metallib
	for key, option := range map[string]string{"macos": "-productVersion", "macos_build": "-buildVersion"} {
		version, err := command(ctx, nil, "sw_vers", option)
		if err != nil {
			return err
		}
		r.Versions[key] = strings.TrimSpace(version)
	}
	for _, path := range sources {
		sum, err := digest(path)
		if err != nil {
			return err
		}
		r.Sources[path] = sum
	}
	// Include the current complete shared runtime identity in the build receipt.
	for _, path := range []string{"go.mod", "go.sum", "internal/runtimebundle/runtime.zip"} {
		sum, err := digest(path)
		if err != nil {
			return err
		}
		r.Sources[path] = sum
	}
	work, err := os.MkdirTemp("bin", ".metal-build-")
	if err != nil {
		return fmt.Errorf("create Metal build directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(work); err != nil {
			result = errors.Join(result, fmt.Errorf("remove Metal build directory %s: %w", work, err))
		}
	}()
	air := filepath.Join(work, "copy.air")
	library := filepath.Join(work, "copy.metallib")
	args := append([]string{"xcrun", "-sdk", "macosx", "metal"}, metalFlags...)
	args = append(args, "-c", kernelPath, "-o", air)
	if _, err = command(ctx, nil, args...); err != nil {
		return err
	}
	if _, err = command(ctx, nil, "xcrun", "-sdk", "macosx", "metallib", air, "-o", library); err != nil {
		return err
	}
	r.Library, err = digest(library)
	if err != nil {
		return err
	}
	if err = os.Rename(library, libraryPath); err != nil {
		return fmt.Errorf("publish Metal library: %w", err)
	}
	identity.KernelSHA256 = r.Library
	if err := packageRuntime(identity); err != nil {
		return err
	}
	for _, path := range []string{"internal/runtimebundle/metal.zip", "internal/runtimebundle/metal-manifest.json"} {
		sum, err := digest(path)
		if err != nil {
			return err
		}
		r.Sources[path] = sum
	}
	env := buildEnvironment(settings)
	artifact := filepath.Join(work, "lo-metal")
	args = append([]string{"go"}, goFlags...)
	args = append(args, "-o", artifact, "./cmd/lo")
	if _, err = command(ctx, env, args...); err != nil {
		return err
	}
	r.Binary, err = digest(artifact)
	if err != nil {
		return err
	}
	if err = os.Rename(artifact, binaryPath); err != nil {
		return fmt.Errorf("publish Metal compiler: %w", err)
	}
	file, err := os.Create(receiptPath)
	if err != nil {
		return fmt.Errorf("create Metal receipt: %w", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	writeErr := encoder.Encode(r)
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("write Metal receipt: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close Metal receipt: %w", closeErr)
	}
	fmt.Printf("Built %s; identity recorded in %s\n", binaryPath, receiptPath)
	if testPattern != "" {
		args := append([]string{"go", "test", "-tags=metal", "./tests/integration", "-run", testPattern}, testArgs...)
		output, err := command(ctx, env, args...)
		if err != nil {
			return err
		}
		fmt.Print(output)
	}
	return nil
}
func buildEnvironment(settings []string) []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GO") || strings.HasPrefix(key, "CGO") || key == "CC" || key == "CXX" || key == "AR" || key == "SDKROOT" || key == "MACOSX_DEPLOYMENT_TARGET" {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "GOOS=darwin", "GOARCH=arm64", "GOFLAGS=", "GOWORK=off", "GOENV=off", "GOTOOLCHAIN="+metalconfig.GoToolchain)
	return append(env, settings...)
}

func packageRuntime(identity metalconfig.Identity) error {
	paths := metalconfig.RuntimeFiles()
	sort.Strings(paths)
	manifest := metalconfig.Manifest{Identity: identity, Files: map[string]string{}}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read Metal runtime %s: %w", path, err)
		}
		file, err := writer.Create(path)
		if err != nil {
			return err
		}
		if _, err := file.Write(data); err != nil {
			return err
		}
		manifest.Files[path] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	if err := writer.Close(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	for path, data := range map[string][]byte{"internal/runtimebundle/metal.zip": archive.Bytes(), "internal/runtimebundle/metal-manifest.json": append(data, '\n')} {
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		if err := errors.Join(writeErr, file.Close()); err != nil {
			return err
		}
	}
	return nil
}
func command(ctx context.Context, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Metal build %s: %w\n%s", strings.Join(args, " "), err, output)
	}
	return string(output), nil
}
func digest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("hash Metal build input %s: %w", path, err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}
