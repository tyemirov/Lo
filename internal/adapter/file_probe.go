package adapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// FileProbeExecutable identifies the probe selected before program evaluation.
type FileProbeExecutable interface {
	Path() (string, error)
	fileProbeExecutable()
}

type fileProbe struct {
	tool     string
	err      error
	registry *processRegistry
}

func (*fileProbe) fileProbeExecutable()    {}
func (p *fileProbe) Path() (string, error) { return p.tool, p.err }

func openFileProbe() *fileProbe {
	p := &fileProbe{}
	p.tool, p.err = exec.LookPath("lsof")
	if p.err != nil {
		return p
	}
	p.tool, p.err = filepath.Abs(p.tool)
	if p.err != nil {
		return p
	}
	helper, err := os.Executable()
	if err != nil {
		p.err = err
		return p
	}
	grants := []ProcessExecutableGrant{}
	seen := map[string]bool{}
	for _, path := range []string{p.tool, helper} {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			p.err = err
			return p
		}
		for _, name := range []string{path, resolved} {
			if seen[name] {
				continue
			}
			seen[name] = true
			file, err := os.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK, 0)
			if err != nil {
				p.err = err
				return p
			}
			digest, hashErr := hashProcessFile(context.Background(), file, defaultProcessRegistryMaxBytes)
			if err := errors.Join(hashErr, file.Close()); err != nil {
				p.err = err
				return p
			}
			grant, err := NewProcessExecutableGrant(name, digest)
			if err != nil {
				p.err = err
				return p
			}
			grants = append(grants, grant)
		}
	}
	registry, err := OpenProcessRegistry(context.Background(), grants, DefaultProcessRegistryLimits())
	if err != nil {
		p.err = err
		return p
	}
	p.registry = registry.(*processRegistry)
	return p
}

func (f *files) probeOpenFiles(ctx context.Context, root *os.Root) ([]byte, error) {
	if f.probe.registry != nil {
		for _, registration := range f.probe.registry.registrations {
			if err := f.probe.registry.validateExecutable(ctx, registration.grant.Path()); err != nil {
				return nil, fmt.Errorf("validate open-file probe: %w", err)
			}
		}
	}
	return f.io.ProbeOpenFiles(ctx, root, f.probe)
}
