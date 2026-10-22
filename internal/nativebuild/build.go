// Package nativebuild constructs isolated, source-independent native artifacts.
package nativebuild

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/metalconfig"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const BuildTimeout = 180 * time.Second
const MaxDiagnostics = 1 << 20
const MaxArtifactBytes = 64 << 20

type Receipt struct {
	Command       string                `json:"command"`
	Status        string                `json:"status"`
	Artifact      string                `json:"artifact"`
	Target        string                `json:"target"`
	Emit          string                `json:"emit,omitempty"`
	ABI           string                `json:"abi,omitempty"`
	Codegen       string                `json:"codegen,omitempty"`
	Runtime       string                `json:"runtime_sha256,omitempty"`
	DeviceBackend device.BuildBackend   `json:"device_backend,omitempty"`
	Metal         *metalconfig.Identity `json:"metal,omitempty"`
}

func runTool(command *exec.Cmd, cancel context.CancelFunc) ([]byte, error, bool) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = time.Second
	output := &boundedOutput{limit: MaxDiagnostics, cancel: cancel}
	command.Stdout, command.Stderr = output, output
	err := command.Run()
	return output.buffer.Bytes(), err, output.exceeded
}
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Close())
}

type boundedOutput struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	limit    int
	cancel   context.CancelFunc
	exceeded bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := b.limit - b.buffer.Len()
	if len(p) > remaining {
		b.buffer.Write(p[:remaining])
		b.exceeded = true
		b.cancel()
	} else {
		b.buffer.Write(p)
	}
	return n, nil
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
