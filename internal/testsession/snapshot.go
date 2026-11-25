package testsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

type contextReader struct {
	context context.Context
	reader  io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	if err := reader.context.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func digestFile(ctx context.Context, path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	digest := sha256.New()
	size, readErr := io.Copy(digest, contextReader{ctx, file})
	if err := errors.Join(readErr, file.Close()); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(digest.Sum(nil)), size, nil
}

func (server *Server) snapshot(manifest, compiler string) error {
	data, err := os.ReadFile(manifest)
	if err != nil {
		return fmt.Errorf("read fixture toolchain: %w", err)
	}
	if err := json.Unmarshal(data, &server.distribution); err != nil {
		return fmt.Errorf("decode fixture toolchain: %w", err)
	}
	if server.distribution.ABI != "lo-native-3" || server.distribution.LLVM != "21.1.8" || server.distribution.Host != runtime.GOOS+"/"+runtime.GOARCH {
		return fmt.Errorf("fixture toolchain ABI, LLVM version, or host mismatch")
	}
	root := filepath.Dir(manifest)
	snapshotRoot := filepath.Join(server.directory, "inputs")
	if err := os.MkdirAll(snapshotRoot, os.ModePerm); err != nil {
		return fmt.Errorf("create fixture input snapshot: %w", err)
	}
	files := map[string]string{}
	verified := map[string]string{}
	copyInput := func(identity fileIdentity) error {
		if !filepath.IsLocal(identity.Path) || len(identity.SHA256) != sha256.Size*2 {
			return fmt.Errorf("fixture input requires relative path and SHA-256: %q", identity.Path)
		}
		if digest, ok := files[identity.Path]; ok {
			if digest != identity.SHA256 {
				return fmt.Errorf("fixture input has conflicting identities: %q", identity.Path)
			}
			return nil
		}
		path, err := localPath(root, filepath.Join(root, identity.Path))
		if err != nil {
			return err
		}
		destination := filepath.Join(snapshotRoot, identity.Path)
		if err := os.MkdirAll(filepath.Dir(destination), os.ModePerm); err != nil {
			return err
		}
		if original, exists := verified[identity.SHA256]; exists {
			digest, _, err := digestFile(server.ctx, path)
			if err != nil {
				return fmt.Errorf("verify fixture alias %q: %w", identity.Path, err)
			}
			if digest != identity.SHA256 {
				return fmt.Errorf("fixture snapshot SHA-256 mismatch: %q", identity.Path)
			}
			if err := os.Link(original, destination); err != nil {
				return fmt.Errorf("snapshot fixture alias %q: %w", identity.Path, err)
			}
			files[identity.Path] = digest
			return nil
		}
		if err := cloneFile(server.ctx, path, destination); err != nil {
			return fmt.Errorf("snapshot fixture input %q: %w", identity.Path, err)
		}
		digest, size, err := digestFile(server.ctx, destination)
		if err != nil {
			return fmt.Errorf("read fixture snapshot %q: %w", identity.Path, err)
		}
		if digest != identity.SHA256 {
			return fmt.Errorf("fixture snapshot SHA-256 mismatch: %q", identity.Path)
		}
		server.stats.Bytes += size
		if server.stats.Bytes > server.maxBytes {
			return fmt.Errorf("fixture input snapshots exceed session storage budget")
		}
		files[identity.Path] = digest
		verified[digest] = destination
		return nil
	}
	copyTarget := func(target targetInputs) error {
		for _, identity := range []fileIdentity{target.Linker, target.Runtime} {
			if err := copyInput(identity); err != nil {
				return err
			}
		}
		for _, identity := range target.Inputs {
			if err := copyInput(identity); err != nil {
				return err
			}
		}
		return nil
	}
	if err := copyInput(server.distribution.Compiler); err != nil {
		return err
	}
	for _, target := range server.distribution.Targets {
		if err := copyTarget(target); err != nil {
			return err
		}
	}
	for _, identity := range server.distribution.HostLibraries {
		if err := copyInput(identity); err != nil {
			return err
		}
	}
	if metal := server.distribution.Metal; metal != nil {
		if err := copyTarget(metal.targetInputs); err != nil {
			return err
		}
		if err := copyInput(metal.Kernel); err != nil {
			return err
		}
	}
	selected := filepath.Join(snapshotRoot, "toolchain.json")
	file, err := os.Create(selected)
	if err != nil {
		return fmt.Errorf("create fixture snapshot manifest: %w", err)
	}
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("write fixture snapshot manifest: %w", err)
	}
	frontend := filepath.Join(snapshotRoot, "frontend")
	if err := retainFrontend(server.ctx, compiler, frontend); err != nil {
		return fmt.Errorf("snapshot fixture frontend: %w", err)
	}
	digest, size, err := digestFile(server.ctx, frontend)
	if err != nil {
		return fmt.Errorf("identify fixture frontend: %w", err)
	}
	server.stats.Bytes += size
	if server.stats.Bytes > server.maxBytes {
		return fmt.Errorf("fixture frontend snapshot exceeds session storage budget")
	}
	server.identity = fmt.Sprintf("%x:%s", sha256.Sum256(data), digest)
	server.inputs = InputsResult{Manifest: selected, Compiler: frontend}
	return nil
}
