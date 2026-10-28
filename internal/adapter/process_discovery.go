package adapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// ErrProcessExecutableAbsent distinguishes a completed search from a discovery failure.
var ErrProcessExecutableAbsent = errors.New("Process executable is absent")

// ResolveProcessExecutable selects the first executable independently of launch grants.
// Empty and relative PATH entries are rejected. Symlinks select their resolved target.
func ResolveProcessExecutable(ctx context.Context, initial value.ProcessContext, name string) (value.ProcessExecutable, error) {
	if name == "" || strings.ContainsRune(name, 0) {
		return nil, fmt.Errorf("discover Process executable: invalid name")
	}
	var candidates []string
	explicit := strings.ContainsRune(name, filepath.Separator)
	if explicit {
		if !filepath.IsAbs(name) || filepath.Clean(name) != name {
			return nil, fmt.Errorf("discover Process executable: explicit path must be absolute and canonical")
		}
		candidates = []string{name}
	} else {
		path, exists := initial.EnvironmentValue("PATH")
		if !exists {
			return nil, ErrProcessExecutableAbsent
		}
		for _, directory := range strings.Split(path.Text(), string(os.PathListSeparator)) {
			if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
				return nil, fmt.Errorf("discover Process executable: PATH entries must be absolute and canonical")
			}
			candidates = append(candidates, filepath.Join(directory, name))
		}
	}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("discover Process executable: candidate resolution failed")
		}
		entry, err := os.Stat(resolved)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("discover Process executable: candidate observation failed")
		}
		if entry.IsDir() {
			continue
		}
		if err := unix.Access(resolved, unix.X_OK); err != nil {
			if !explicit && errors.Is(err, unix.EACCES) {
				continue
			}
			return nil, fmt.Errorf("discover Process executable: candidate cannot execute")
		}
		path, err := value.NewText(types.Path(), resolved)
		if err != nil {
			return nil, fmt.Errorf("discover Process executable: candidate is not valid text")
		}
		return value.NewProcessExecutable(path)
	}
	return nil, ErrProcessExecutableAbsent
}
