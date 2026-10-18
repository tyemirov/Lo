package nativebuild

import (
	"fmt"
	"os"
	"path/filepath"
)

type buildPaths struct{ root, work string }

func prepareBuildPaths(root string) (buildPaths, error) {
	if root == "" || !filepath.IsAbs(root) {
		return buildPaths{}, fmt.Errorf("LO_BUILD_ROOT must name an existing absolute directory")
	}
	root = filepath.Clean(root)
	if err := validateBuildDirectory(root); err != nil {
		return buildPaths{}, fmt.Errorf("LO_BUILD_ROOT: %w", err)
	}
	names := []string{"work", "go-build", "modules", "gopath", "cache", "clang", "ccache"}
	for _, name := range names {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// A dangling symlink is an invalid input, not an absent state directory.
			if _, linkErr := os.Readlink(path); linkErr == nil {
				return buildPaths{}, fmt.Errorf("LO_BUILD_ROOT contains a symlink at %q", path)
			}
			continue
		}
		if err := validateBuildDirectory(path); err != nil {
			return buildPaths{}, fmt.Errorf("LO_BUILD_ROOT: %w", err)
		}
	}
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(root, name), os.ModePerm); err != nil {
			return buildPaths{}, fmt.Errorf("prepare LO_BUILD_ROOT: %w", err)
		}
	}
	return buildPaths{root: root, work: filepath.Join(root, "work")}, nil
}
func validateBuildDirectory(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve build directory %q: %w", path, err)
	}
	if resolved != path {
		return fmt.Errorf("build directory %q traverses a symlink", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("read build directory %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("build directory %q is not a directory", path)
	}
	return nil
}
