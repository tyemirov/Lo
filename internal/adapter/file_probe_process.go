//go:build darwin || linux

package adapter

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const fileProbeChildArgument = "--lo-internal-file-probe"

func fileProbeCommand(ctx context.Context, directory *os.File) (*exec.Cmd, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate open-file probe helper: %w", err)
	}
	command := exec.CommandContext(ctx, executable, fileProbeChildArgument)
	command.ExtraFiles = []*os.File{directory}
	return command, nil
}

// HandleFileProbe executes the private subprocess entry before command parsing.
func HandleFileProbe(args []string) bool {
	if len(args) != 1 || args[0] != fileProbeChildArgument {
		return false
	}
	if err := runFileProbeChild(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(127)
	}
	return true
}

func runFileProbeChild() error {
	tool, err := exec.LookPath("lsof")
	if err != nil {
		return fmt.Errorf("locate open-file probe: %w", err)
	}
	tool, err = filepath.Abs(tool)
	if err != nil {
		return fmt.Errorf("resolve open-file probe: %w", err)
	}
	if err := syscall.Fchdir(3); err != nil {
		return fmt.Errorf("enter retained probe directory: %w", err)
	}
	if err := syscall.Close(3); err != nil {
		return fmt.Errorf("close probe directory descriptor: %w", err)
	}
	if err := syscall.Exec(tool, []string{"lsof", "-Fn", "+D", "."}, os.Environ()); err != nil {
		return fmt.Errorf("start open-file probe: %w", err)
	}
	return nil
}
