//go:build darwin

package processhost

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"golang.org/x/sys/unix"
)

const nativeConfigurationLimit = 65536
const nativeProfileLimit = 32768

type darwinCreator struct {
	setup               *nativeSetup
	launcher, directory string
	configuration       []byte
	commands            map[string]struct{}
	roots               []string
	helper              adapter.ProcessExecutableGrant
	parallelism         uint32
}

// MaximumParallelism is the host's conservative logical processor maximum. It
// is not a CPU quota. A P-times-lifetime work bound additionally requires complete
// admitted lifetime and termination evidence under a responsive OS.
func (c *darwinCreator) MaximumParallelism() uint32 { return c.parallelism }
func (c *darwinCreator) protectedImages() []adapter.ProcessExecutableGrant {
	return []adapter.ProcessExecutableGrant{c.helper}
}

func canonicalNativePath(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) || len(path) > nativeEntryLimit || !utf8.ValidString(path) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	return err == nil && resolved == path
}

// NewDarwinCreator verifies a build-pinned helper and explicit native authority.
// Open uses this creator for the qualified Darwin 27 arm64 profile.
func NewDarwinCreator(ctx context.Context, configuration Config) (Creator, error) {
	if configuration == nil {
		return nil, host("missing native configuration")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Select only the backend exercised by qualification.
	// Other kernel generations require qualification before helper lookup.
	release, err := unix.Sysctl("kern.osrelease")
	if err != nil || runtime.GOARCH != "arm64" || strings.SplitN(release, ".", 2)[0] != "27" {
		return nil, execution.ProcessUnavailable()
	}
	defaults := adapter.DefaultProcessLimits()
	if configuration.Limits().Timeout() > defaults.Timeout() || configuration.Limits().CleanupTimeout() > defaults.CleanupTimeout() {
		return nil, host("native lifecycle limits exceed the selected profile")
	}
	identity, err := hex.DecodeString(nativeHelperSHA256)
	if err != nil || len(identity) != sha256.Size {
		return nil, execution.ProcessUnavailable()
	}
	policy := configuration.Policy()
	if len(policy.Grants()) == 0 || len(policy.Grants()) > nativeEntryCount || len(policy.ReadRoots()) == 0 || len(policy.ReadRoots()) > nativeEntryCount {
		return nil, failure(Input)
	}
	launcher := policy.Launcher()
	if !canonicalNativePath(launcher) {
		return nil, host("invalid native helper path")
	}
	file, err := os.OpenFile(launcher, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, host("native helper unavailable")
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		return nil, execution.PreserveCleanup(host("native helper requires a regular file"), file.Close())
	}
	digest, hashErr := nativeHelperDigest(ctx, file)
	closeErr := file.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if statErr != nil || hashErr != nil || closeErr != nil || !bytes.Equal(identity, digest[:]) {
		return nil, host("native helper identity mismatch")
	}
	helper, err := adapter.NewProcessExecutableGrant(launcher, digest)
	if err != nil {
		return nil, host("invalid native helper grant")
	}
	parallelism, err := unix.SysctlUint32("hw.logicalcpu_max")
	if err != nil || parallelism == 0 {
		return nil, host("native processor bound unavailable")
	}
	c := &darwinCreator{launcher: launcher, directory: filepath.Dir(launcher), helper: helper, parallelism: parallelism, commands: make(map[string]struct{}), roots: policy.ReadRoots()}
	for _, grant := range policy.Grants() {
		if !canonicalNativePath(grant.Path()) {
			return nil, host("invalid native command path")
		}
		command, err := os.Stat(grant.Path())
		if err != nil || os.SameFile(info, command) {
			return nil, host("native helper cannot be a command")
		}
		c.commands[grant.Path()] = struct{}{}
	}
	for _, root := range c.roots {
		if !canonicalNativePath(root) {
			return nil, host("invalid native read root")
		}
		directory, err := os.OpenRoot(root)
		if err != nil {
			return nil, host("native read root unavailable")
		}
		if err := directory.Close(); err != nil {
			return nil, host("native read root release failed")
		}
	}
	profile, err := c.profile(policy.Grants())
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	writeEntry := func(entry string) {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(entry)))
		body.Write(size[:])
		body.WriteString(entry)
	}
	writeEntry(profile)
	writeEntry(c.directory)
	for _, grant := range policy.Grants() {
		writeEntry(grant.Path())
	}
	for _, root := range c.roots {
		writeEntry(root)
	}
	if body.Len() > nativeConfigurationLimit {
		return nil, failure(Input)
	}
	header := make([]byte, 16)
	copy(header, "LOC1")
	binary.BigEndian.PutUint32(header[4:8], uint32(body.Len()))
	binary.BigEndian.PutUint32(header[8:12], uint32(len(policy.Grants())))
	binary.BigEndian.PutUint32(header[12:16], uint32(len(c.roots)))
	c.configuration = append(header, body.Bytes()...)
	return c, nil
}

func nativeHelperDigest(ctx context.Context, file *os.File) ([32]byte, error) {
	hash := sha256.New()
	remaining := adapter.DefaultProcessRegistryLimits().MaxBytes()
	buffer := make([]byte, 32768)
	for {
		if err := ctx.Err(); err != nil {
			return [32]byte{}, err
		}
		limit := int64(len(buffer))
		if remaining < limit {
			limit = remaining + 1
		}
		count, err := file.Read(buffer[:int(limit)])
		if int64(count) > remaining {
			return [32]byte{}, failure(Input)
		}
		remaining -= int64(count)
		hash.Write(buffer[:count])
		if err == io.EOF {
			var digest [32]byte
			copy(digest[:], hash.Sum(nil))
			return digest, nil
		}
		if err != nil {
			return [32]byte{}, err
		}
	}
}

func (c *darwinCreator) profile(grants []adapter.ProcessExecutableGrant) (string, error) {
	quote := func(path string) string { encoded, _ := json.Marshal(path); return string(encoded) }
	lines := []string{"(version 1)", "(deny default (with no-log))", `(allow file-read* (subpath "/usr/lib") (subpath "/System/Library"))`, `(allow file-read* file-test-existence (literal "/"))`, "(deny network* (with no-log))", "(deny process-fork (with no-log))", "(allow signal (target self))"}
	for _, grant := range grants {
		path := quote(grant.Path())
		lines = append(lines, "(allow process-exec (literal "+path+"))", "(allow file-read* (literal "+path+"))")
	}
	for _, root := range c.roots {
		lines = append(lines, "(allow file-read* (subpath "+quote(root)+"))")
	}
	profile := strings.Join(lines, "\n") + "\n"
	if len(profile) > nativeProfileLimit {
		return "", failure(Input)
	}
	return profile, nil
}

func (c *darwinCreator) authorize(ctx context.Context, invocation adapter.ProcessInvocation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if invocation == nil {
		return failure(Input)
	}
	if _, allowed := c.commands[invocation.Executable()]; !allowed {
		return execution.Denied("Process.run", "command")
	}
	directory := invocation.Directory()
	if !canonicalNativePath(directory) {
		return execution.Denied("Process.run", "directory")
	}
	for _, root := range c.roots {
		relative, err := filepath.Rel(root, directory)
		if err == nil && (relative == "." || filepath.IsLocal(relative)) {
			return nil
		}
	}
	return execution.Denied("Process.run", "directory")
}
