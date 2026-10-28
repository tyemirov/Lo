package adapter

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// ProcessPolicy describes explicit child authority without accessing files or
// qualifying a launcher. It confers no parent File authority.
type ProcessPolicy interface {
	Grants() []ProcessExecutableGrant
	Launcher() string
	ReadRoots() []string
	Scope(string) (ProcessScope, bool)
	processPolicy()
}

type processPolicy struct {
	grants   []ProcessExecutableGrant
	launcher string
	reads    []string
	scopes   map[string]processScope
}

func (processPolicy) processPolicy()                     {}
func (p processPolicy) Grants() []ProcessExecutableGrant { return slices.Clone(p.grants) }
func (p processPolicy) Launcher() string                 { return p.launcher }
func (p processPolicy) ReadRoots() []string              { return slices.Clone(p.reads) }
func (p processPolicy) Scope(path string) (ProcessScope, bool) {
	scope, ok := p.scopes[path]
	if !ok {
		return nil, false
	}
	return scope, true
}

func processPolicyPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsRune(path, 0)
}

// NewProcessPolicy validates SHA256,ABSOLUTE_PATH grants and explicit launcher
// and child read roots. No inputs selects no Process authority. Otherwise every
// category is required. Hexadecimal case does not change executable identity.
func NewProcessPolicy(input ProcessPolicyInput) (ProcessPolicy, error) {
	grants, launcher, readRoots := input.Grants, input.Launcher, input.ReadRoots
	policy := processPolicy{launcher: launcher, scopes: map[string]processScope{}}
	if len(grants) == 0 && launcher == "" && len(readRoots) == 0 {
		if len(input.Writes)+len(input.Prunes)+len(input.Endpoints) != 0 {
			return nil, fmt.Errorf("Process scopes require executable grants, a launcher, and read roots")
		}
		return policy, nil
	}
	if len(grants) == 0 || launcher == "" || len(readRoots) == 0 {
		return nil, fmt.Errorf("Process policy requires a launcher, at least one executable grant, and at least one read root")
	}
	if !processPolicyPath(launcher) {
		return nil, fmt.Errorf("Process launcher requires a clean absolute NUL-free path")
	}
	paths := make(map[string]bool, len(grants))
	for index, description := range grants {
		if len(description) < 66 || description[64] != ',' {
			return nil, fmt.Errorf("Process grant %d requires SHA256,ABSOLUTE_PATH", index)
		}
		decoded, err := hex.DecodeString(description[:64])
		if err != nil {
			return nil, fmt.Errorf("Process grant %d requires a 64-digit hexadecimal SHA256", index)
		}
		var digest [32]byte
		copy(digest[:], decoded)
		grant, err := NewProcessExecutableGrant(description[65:], digest)
		if err != nil {
			return nil, fmt.Errorf("Process grant %d: %w", index, err)
		}
		if paths[grant.Path()] {
			return nil, fmt.Errorf("duplicate Process executable path at grant %d", index)
		}
		if grant.Path() == launcher {
			return nil, fmt.Errorf("Process launcher and command paths must differ")
		}
		paths[grant.Path()] = true
		policy.grants = append(policy.grants, grant)
		policy.scopes[grant.Path()] = processScope{}
	}
	reads := make(map[string]bool, len(readRoots))
	for index, path := range readRoots {
		if !processPolicyPath(path) {
			return nil, fmt.Errorf("Process read root %d requires a clean absolute NUL-free path", index)
		}
		if reads[path] {
			return nil, fmt.Errorf("duplicate Process read root at index %d", index)
		}
		reads[path] = true
		policy.reads = append(policy.reads, path)
	}
	if err := policy.addScopes(input); err != nil {
		return nil, err
	}
	return policy, nil
}
