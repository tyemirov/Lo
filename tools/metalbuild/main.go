// Command metalbuild builds the explicitly selected Metal compiler and records its inputs.
package main

import (
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
	"strconv"
	"strings"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
)

const (
	libraryPath = "internal/device/metal.metallib"
	kernelPath  = "internal/device/metal_kernels.metal"
	binaryPath  = "bin/lo-metal"
	receiptPath = "bin/lo-metal-build.json"
)

// This manifest lists Metal source inputs and kernel compiler options.
var sources = []string{
	"internal/device/metal_darwin.go", "internal/device/metal_bridge.h",
	"internal/device/metal_bridge_darwin.m", "internal/device/metal_types.h", kernelPath, "tools/metalbuild/main.go",
}
var metalFlags = []string{"-std=metal3.1", "-mmacosx-version-min=27.0", "-fmetal-math-mode=safe", "-fmetal-math-fp32-functions=precise", "-ffp-contract=off"}
var goFlags = []string{"build", "-trimpath", "-tags=metal"}

type toolPin struct {
	Name     string
	Args     []string
	Expected string
}

var pins = []toolPin{
	{"xcode", []string{"xcodebuild", "-version"}, "Xcode 27.0\nBuild version 27A266a"},
	{"sdk", []string{"xcrun", "--sdk", "macosx", "--show-sdk-version"}, "27.0"},
	{"clang", []string{"xcrun", "clang", "--version"}, "Apple clang version 21.0.0 (clang-2100.3.34.2)"},
	{"metal", []string{"xcrun", "metal", "--version"}, "Apple metal version 32023.921 (metalfe-32023.921.6)"},
	{"metallib", []string{"xcrun", "metallib", "--version"}, "AIR-LLD 32023.921 (metalfe-32023.921.6) (compatible with legacy metallib linker)"},
}

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
	test := flag.Bool("test", false, "run the explicit local Metal integration tests")
	flag.Parse()
	if err := run(*test, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(test bool, testArgs []string) (result error) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return fmt.Errorf("build Metal: require a darwin/arm64 host")
	}
	if runtime.Version() != "go1.27.1" {
		return fmt.Errorf("build Metal: require Go 1.27.1, got %s", runtime.Version())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	r := receipt{Target: "darwin/arm64", Profile: device.MetalProfile, DeviceFeatures: []string{"Apple7", "unified memory"}, Versions: map[string]string{"go": runtime.Version()}, Sources: map[string]string{}, MetalFlags: metalFlags, GoFlags: goFlags}
	for _, pin := range pins {
		output, err := command(ctx, nil, pin.Args...)
		if err != nil {
			return err
		}
		value := strings.TrimSpace(output)
		if pin.Name == "clang" || pin.Name == "metal" {
			value = strings.SplitN(value, "\n", 2)[0]
		}
		if value != pin.Expected {
			return fmt.Errorf("build Metal: %s requires %q, got %q", pin.Name, pin.Expected, value)
		}
		r.Versions[pin.Name] = value
	}
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
	clang, err := command(ctx, nil, "xcrun", "--sdk", "macosx", "--find", "clang")
	if err != nil {
		return err
	}
	sdk, err := command(ctx, nil, "xcrun", "--sdk", "macosx", "--show-sdk-path")
	if err != nil {
		return err
	}
	env := buildEnvironment(strings.TrimSpace(clang), strings.TrimSpace(sdk))
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
	if test {
		args := append([]string{"go", "test", "-tags=metal", "./tests/integration", "-run", "^TestMetalLive"}, testArgs...)
		output, err := command(ctx, env, args...)
		if err != nil {
			return err
		}
		fmt.Print(output)
	}
	return nil
}
func buildEnvironment(clang, sdk string) []string {
	owned := map[string]string{"CGO_ENABLED": "1", "GOOS": "darwin", "GOARCH": "arm64", "CC": clang, "SDKROOT": sdk, "MACOSX_DEPLOYMENT_TARGET": "27.0", "CGO_CFLAGS": "", "CGO_CPPFLAGS": "-isysroot " + strconv.Quote(sdk), "CGO_CXXFLAGS": "", "CGO_LDFLAGS": "-isysroot " + strconv.Quote(sdk), "GOFLAGS": "", "GOWORK": "off", "GOENV": "off", "GOTOOLCHAIN": "go1.27.1"}
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, ok := owned[key]; !ok {
			env = append(env, entry)
		}
	}
	for key, value := range owned {
		env = append(env, key+"="+value)
	}
	return env
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
