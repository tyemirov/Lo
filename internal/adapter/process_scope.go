package adapter

import (
	"fmt"
	"path/filepath"
	"slices"
)

// ProcessPathScopeInput supplies one unvalidated per-executable resource grant.
type ProcessPathScopeInput struct{ Executable, Path string }

// ProcessPolicyInput supplies unvalidated host authority descriptions.
type ProcessPolicyInput struct {
	Grants                    []string
	Launcher                  string
	ReadRoots                 []string
	Writes, Prunes, Endpoints []ProcessPathScopeInput
}

// ProcessScopeKind selects a current execution profile.
type ProcessScopeKind uint8

const (
	ProcessReadOnlyScope ProcessScopeKind = iota + 1
	ProcessCleanupScope
)

// ProcessScope is immutable authority for the selected initial executable.
// An in-process interpreter or exec chain retains this same scope.
type ProcessScope interface {
	Kind() ProcessScopeKind
	WriteRoots() []string
	PruneRoots() []string
	UnixEndpoints() []string
	processScope()
}

type processScope struct{ writes, prunes, endpoints []string }

func (processScope) processScope() {}
func (scope processScope) Kind() ProcessScopeKind {
	if len(scope.writes)+len(scope.prunes)+len(scope.endpoints) == 0 {
		return ProcessReadOnlyScope
	}
	return ProcessCleanupScope
}
func (scope processScope) WriteRoots() []string    { return slices.Clone(scope.writes) }
func (scope processScope) PruneRoots() []string    { return slices.Clone(scope.prunes) }
func (scope processScope) UnixEndpoints() []string { return slices.Clone(scope.endpoints) }

func pathContains(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && (relative == "." || filepath.IsLocal(relative))
}

func (policy *processPolicy) addScopes(input ProcessPolicyInput) error {
	for _, group := range []struct {
		kind   string
		values []ProcessPathScopeInput
	}{{"write root", input.Writes}, {"prune root", input.Prunes}, {"Unix endpoint", input.Endpoints}} {
		for _, entry := range group.values {
			scope, exists := policy.scopes[entry.Executable]
			if !exists {
				return fmt.Errorf("Process %s requires an exact granted executable", group.kind)
			}
			if !processPolicyPath(entry.Path) {
				return fmt.Errorf("Process %s requires a clean absolute NUL-free path", group.kind)
			}
			var values *[]string
			switch group.kind {
			case "write root":
				values = &scope.writes
			case "prune root":
				values = &scope.prunes
			case "Unix endpoint":
				values = &scope.endpoints
			}
			if slices.Contains(*values, entry.Path) {
				return fmt.Errorf("duplicate Process %s", group.kind)
			}
			*values = append(*values, entry.Path)
			policy.scopes[entry.Executable] = scope
		}
	}
	for executable, scope := range policy.scopes {
		mutations := append(slices.Clone(scope.writes), scope.prunes...)
		for index, root := range mutations {
			for _, other := range mutations[:index] {
				if pathContains(root, other) || pathContains(other, root) {
					return fmt.Errorf("Process mutation roots overlap for %q", executable)
				}
			}
			if pathContains(root, policy.launcher) {
				return fmt.Errorf("Process mutation root contains the launcher")
			}
			for _, grant := range policy.grants {
				if pathContains(root, grant.Path()) {
					return fmt.Errorf("Process mutation root contains a registered executable")
				}
			}
			covered := false
			for _, read := range policy.reads {
				covered = covered || pathContains(read, root)
			}
			if !covered {
				return fmt.Errorf("Process mutation root requires a containing child read root")
			}
			for _, endpoint := range scope.endpoints {
				if pathContains(root, endpoint) {
					return fmt.Errorf("Process Unix endpoint overlaps a mutation root")
				}
			}
		}
	}
	return nil
}
