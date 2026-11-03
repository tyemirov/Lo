// Package metalconfig defines the pinned Metal artifact inputs and toolchain.
package metalconfig

import "github.com/MarcoPoloResearchLab/Lo/internal/targetconfig"

const (
	Target           = "darwin/arm64"
	DeploymentTarget = "27.0"
)

// KernelFlags fixes the arithmetic configuration of the embedded kernels.
func KernelFlags() []string {
	return []string{"-std=metal3.1", "-mmacosx-version-min=" + DeploymentTarget, "-fmetal-math-mode=safe", "-fmetal-math-fp32-functions=precise", "-ffp-contract=off"}
}

// Identity records build inputs; device identity and grants are runtime inputs.
type Identity struct {
	Profile          string   `json:"profile"`
	Xcode            string   `json:"xcode"`
	SDK              string   `json:"sdk"`
	Clang            string   `json:"clang"`
	Metal            string   `json:"metal_compiler"`
	Metallib         string   `json:"kernel_linker"`
	DeploymentTarget string   `json:"deployment_target"`
	KernelFlags      []string `json:"kernel_flags"`
	KernelSHA256     string   `json:"kernel_sha256"`
}

// PinnedIdentity returns the supported compiler and numeric profile configuration.
func PinnedIdentity() Identity {
	return Identity{Profile: targetconfig.MetalProfile, Xcode: "Xcode 27.1\nBuild version 27A9275", SDK: "27.0", Clang: "Apple clang version 21.0.0 (clang-2100.3.34.2)", Metal: "Apple metal version 32023.921 (metalfe-32023.921.6)", Metallib: "AIR-LLD 32023.921 (metalfe-32023.921.6) (compatible with legacy metallib linker)", DeploymentTarget: DeploymentTarget, KernelFlags: KernelFlags()}
}
