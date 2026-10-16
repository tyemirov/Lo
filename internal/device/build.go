package device

import "fmt"

// BuildBackend selects the device implementation included in an artifact.
type BuildBackend string

const (
	SoftwareBuild BuildBackend = "software"
	MetalBuild    BuildBackend = "metal"
)

// ParseBuildBackend validates an explicit artifact backend at the CLI boundary.
func ParseBuildBackend(text string) (BuildBackend, error) {
	switch BuildBackend(text) {
	case SoftwareBuild, MetalBuild:
		return BuildBackend(text), nil
	default:
		return "", fmt.Errorf("unknown device backend %q", text)
	}
}
