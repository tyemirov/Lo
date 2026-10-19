package adapter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/bits"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

const maxProbeStreamBytes = execution.MaxBytes / 2

type probeOutput struct {
	buffer   bytes.Buffer
	cancel   context.CancelFunc
	overflow bool
}

func (w *probeOutput) Write(data []byte) (int, error) {
	if len(data) > maxProbeStreamBytes-w.buffer.Len() {
		w.overflow = true
		w.cancel()
		return 0, execution.Limit("open-file probe output")
	}
	return w.buffer.Write(data)
}

func (nativeFileIO) ProbeOpenFiles(ctx context.Context, root *os.Root, probe FileProbeExecutable) (output []byte, resultErr error) {
	tool, err := probe.Path()
	if err != nil {
		return nil, fmt.Errorf("locate open-file probe: %w", err)
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	directory, err := root.Open(".")
	if err != nil {
		return nil, fmt.Errorf("open probe directory: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	command, err := fileProbeCommand(child, directory, tool)
	if err != nil {
		return nil, err
	}
	stdout, stderr := &probeOutput{cancel: cancel}, &probeOutput{cancel: cancel}
	command.Stdout, command.Stderr = stdout, stderr
	command.WaitDelay = time.Second
	err = command.Run()
	if ctx.Err() != nil {
		return nil, execution.BoundaryHostError(ctx.Err())
	}
	if stdout.overflow || stderr.overflow {
		return nil, execution.Limit("open-file probe output")
	}
	if err != nil {
		var status *exec.ExitError
		if !errors.As(err, &status) || status.ExitCode() != 1 {
			return nil, fmt.Errorf("probe open files: %w", err)
		}
	}
	if !utf8.Valid(stderr.buffer.Bytes()) {
		return nil, errors.New("open-file probe returned invalid UTF-8 on standard error")
	}
	if strings.TrimSpace(stderr.buffer.String()) != "" {
		return nil, fmt.Errorf("open-file probe reported standard error: %s", stderr.buffer.String())
	}
	return stdout.buffer.Bytes(), nil
}

func parseOpenPaths(session execution.Session, root string, output []byte) (value.List, error) {
	if len(output) > maxProbeStreamBytes {
		return nil, execution.Limit("open-file probe output")
	}
	// Charge scanning before validation, and each record before allocation.
	if err := session.ReserveFile(uint64(len(output)), uint64(len(output))); err != nil {
		return nil, err
	}
	if !utf8.Valid(output) || bytes.ContainsRune(output, 0) {
		return nil, errors.New("open-file probe returned invalid text")
	}
	paths := map[string]struct{}{}
	weight := uint64(32)
	for line := range strings.SplitSeq(string(output), "\n") {
		if line == "" {
			continue
		}
		if err := session.ReserveFile(1, weight); err != nil {
			return nil, err
		}
		switch line[0] {
		case 'p':
			if len(line) == 1 {
				return nil, errors.New("open-file probe returned an empty process identifier")
			}
			for _, digit := range line[1:] {
				if digit < '0' || digit > '9' {
					return nil, errors.New("open-file probe returned an invalid process identifier")
				}
			}
		case 'f':
			if len(line) == 1 {
				return nil, errors.New("open-file probe returned an empty descriptor")
			}
			for _, character := range line[1:] {
				if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z') {
					return nil, errors.New("open-file probe returned an invalid descriptor")
				}
			}
		case 'n':
			path := line[1:]
			for _, character := range path {
				if character == '\\' || unicode.IsControl(character) {
					return nil, errors.New("open-file probe returned an ambiguous filename")
				}
			}
			if path == "." {
				continue
			}
			if !strings.HasPrefix(path, "./") {
				return nil, errors.New("open-file probe returned a path outside its directory")
			}
			relative := strings.TrimPrefix(path, "./")
			if !filepath.IsLocal(relative) || relative == "." || filepath.Clean(relative) != relative {
				return nil, errors.New("open-file probe returned an invalid relative path")
			}
			path = filepath.Join(root, relative)
			if _, exists := paths[path]; exists {
				continue
			}
			if len(paths) == execution.MaxFileEntries {
				return nil, execution.Limit("open-file probe paths")
			}
			weight += uint64(16 + len(path))
			if err := session.ReserveFile(0, weight); err != nil {
				return nil, err
			}
			paths[path] = struct{}{}
		default:
			return nil, errors.New("open-file probe returned an unknown record")
		}
	}
	count := uint64(len(paths))
	steps := count
	if count > 1 {
		steps += count * uint64(bits.Len64(count-1))
	}
	if err := session.ReserveFile(steps, weight); err != nil {
		return nil, err
	}
	sorted := make([]string, 0, len(paths))
	for path := range paths {
		sorted = append(sorted, path)
	}
	slices.Sort(sorted)
	items := make([]value.Value, len(sorted))
	for index, path := range sorted {
		item, err := value.NewText(types.Path(), path)
		if err != nil {
			return nil, err
		}
		items[index] = item
	}
	return value.NewList(must(types.NewList(types.Path())), items)
}

func (f *files) OpenPaths(ctx context.Context, path value.Text) (value.List, error) {
	if err := ctx.Err(); err != nil {
		return nil, execution.BoundaryHostError(err)
	}
	session, ok := execution.SessionFromContext(ctx)
	if !ok {
		return nil, execution.HostFailure(catalog.FileOpenPaths, path.Text(), errors.New("missing execution session"))
	}
	selected, directory, err := f.exactGrant(f.openFiles, catalog.FileOpenPaths, path.Text())
	if err != nil {
		return nil, err
	}
	if err := session.ReserveFile(1, 32); err != nil {
		return nil, err
	}
	output, err := f.probeOpenFiles(ctx, selected.root)
	if err == nil {
		var result value.List
		result, err = parseOpenPaths(session, directory.Path, output)
		if err == nil {
			return result, nil
		}
	}
	if execution.BoundaryHostError(err) != nil {
		return nil, err
	}
	return nil, execution.NewFileError(catalog.OpenFilesFailed, nil, nil, fmt.Errorf("probe %q: %w", directory.Path, err))
}
