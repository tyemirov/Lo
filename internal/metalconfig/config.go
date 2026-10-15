// Package metalconfig defines the pinned Metal artifact inputs and toolchain.
package metalconfig

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
)

const (
	Target           = "darwin/arm64"
	GoToolchain      = "go1.27.1"
	DeploymentTarget = "27.0"
	LibraryPath      = "internal/device/metal.metallib"
	KernelPath       = "internal/device/metal_kernels.metal"
)

// RuntimeFiles is the complete Metal supplement to the software runtime bundle.
func RuntimeFiles() []string {
	return []string{"internal/device/metal_darwin.go", "internal/device/metal_bridge_darwin.m", "internal/device/metal_bridge.h", "internal/device/metal_types.h", LibraryPath}
}

// KernelFlags fixes the arithmetic configuration of the embedded kernels.
func KernelFlags() []string {
	return []string{"-std=metal3.1", "-mmacosx-version-min=" + DeploymentTarget, "-fmetal-math-mode=safe", "-fmetal-math-fp32-functions=precise", "-ffp-contract=off"}
}

// Identity records build inputs; device identity and grants are runtime inputs.
type Identity struct {
	Profile          string   `json:"profile"`
	Go               string   `json:"go"`
	Xcode            string   `json:"xcode"`
	SDK              string   `json:"sdk"`
	Clang            string   `json:"clang"`
	Metal            string   `json:"metal_compiler"`
	Metallib         string   `json:"kernel_linker"`
	DeploymentTarget string   `json:"deployment_target"`
	KernelFlags      []string `json:"kernel_flags"`
	KernelSHA256     string   `json:"kernel_sha256"`
}

// Manifest identifies each file in the selected Metal runtime supplement.
type Manifest struct {
	Identity Identity          `json:"identity"`
	Files    map[string]string `json:"files"`
}

// PinnedIdentity returns the supported compiler and numeric profile configuration.
func PinnedIdentity() Identity {
	return Identity{Profile: device.MetalProfile, Go: GoToolchain, Xcode: "Xcode 27.0\nBuild version 27A266a", SDK: "27.0", Clang: "Apple clang version 21.0.0 (clang-2100.3.34.2)", Metal: "Apple metal version 32023.921 (metalfe-32023.921.6)", Metallib: "AIR-LLD 32023.921 (metalfe-32023.921.6) (compatible with legacy metallib linker)", DeploymentTarget: DeploymentTarget, KernelFlags: KernelFlags()}
}

// NativeEnvironment verifies the local native tools and returns their cgo settings.
func NativeEnvironment(run func(...string) (string, error)) ([]string, error) {
	identity := PinnedIdentity()
	for _, pin := range []struct {
		args      []string
		expected  string
		firstLine bool
	}{
		{[]string{"xcodebuild", "-version"}, identity.Xcode, false},
		{[]string{"xcrun", "--sdk", "macosx", "--show-sdk-version"}, identity.SDK, false},
		{[]string{"xcrun", "--sdk", "macosx", "clang", "--version"}, identity.Clang, true},
	} {
		if err := check(run, pin.args, pin.expected, pin.firstLine); err != nil {
			return nil, err
		}
	}
	clang, err := run("xcrun", "--sdk", "macosx", "--find", "clang")
	if err != nil {
		return nil, err
	}
	sdk, err := run("xcrun", "--sdk", "macosx", "--show-sdk-path")
	if err != nil {
		return nil, err
	}
	clang, sdk = strings.TrimSpace(clang), strings.TrimSpace(sdk)
	if clang == "" || sdk == "" {
		return nil, fmt.Errorf("Metal toolchain returned an empty compiler or SDK path")
	}
	return []string{"CGO_ENABLED=1", "CC=" + clang, "SDKROOT=" + sdk, "MACOSX_DEPLOYMENT_TARGET=" + DeploymentTarget, "CGO_CFLAGS=", "CGO_CPPFLAGS=-isysroot " + strconv.Quote(sdk), "CGO_CXXFLAGS=", "CGO_LDFLAGS=-isysroot " + strconv.Quote(sdk)}, nil
}

// CheckKernelTools verifies the compilers used to produce the embedded library.
func CheckKernelTools(run func(...string) (string, error)) error {
	identity := PinnedIdentity()
	if err := check(run, []string{"xcrun", "--sdk", "macosx", "metal", "--version"}, identity.Metal, true); err != nil {
		return err
	}
	return check(run, []string{"xcrun", "--sdk", "macosx", "metallib", "--version"}, identity.Metallib, false)
}

func check(run func(...string) (string, error), args []string, expected string, firstLine bool) error {
	output, err := run(args...)
	if err != nil {
		return err
	}
	actual := strings.TrimSpace(output)
	if firstLine {
		actual = strings.SplitN(actual, "\n", 2)[0]
	}
	if actual != expected {
		return fmt.Errorf("Metal %s requires %q, received %q", strings.Join(args, " "), expected, actual)
	}
	return nil
}
