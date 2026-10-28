//go:build darwin

package processhost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
)

const cleanupEntryLimit = 100000
const cleanupDepthLimit = 128
const nativeUnixEndpointLimit = 103

type cleanupIdentity struct{ device, inode uint64 }
type cleanupLinks struct{ observed, dataNames, declared uint64 }
type cleanupAccess uint8

const (
	cleanupData cleanupAccess = iota + 1
	cleanupNamespace
)

type cleanupRoot struct {
	path   string
	access cleanupAccess
}
type cleanupScan struct {
	ctx     context.Context
	entries int
	links   map[cleanupIdentity]cleanupLinks
}

func validateCleanupScope(ctx context.Context, scope adapter.ProcessScope) error {
	var roots []cleanupRoot
	for _, path := range scope.WriteRoots() {
		roots = append(roots, cleanupRoot{path, cleanupData})
	}
	for _, path := range scope.PruneRoots() {
		roots = append(roots, cleanupRoot{path, cleanupNamespace})
	}
	scan := cleanupScan{ctx: ctx, links: map[cleanupIdentity]cleanupLinks{}}
	for _, selected := range roots {
		path := selected.path
		if err := ctx.Err(); err != nil {
			return err
		}
		if !canonicalNativePath(path) {
			return execution.Denied(catalog.ProcessRun, "cleanup root")
		}
		root, err := os.OpenRoot(path)
		if err != nil {
			return execution.HostFailure("open Process cleanup root", path, err)
		}
		if len(scope.WriteRoots()) != 0 {
			err = scan.directory(root, 0, selected.access)
		}
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, execution.HostFailure("close Process cleanup root", path, closeErr))
		}
		if err != nil {
			return err
		}
	}
	for _, links := range scan.links {
		if links.dataNames != 0 && links.observed != links.declared {
			return execution.Denied(catalog.ProcessRun, "cleanup root has an outside hardlink alias")
		}
		if links.dataNames != 0 && links.dataNames != links.declared {
			return execution.Denied(catalog.ProcessRun, "cleanup inode crosses data-write authority")
		}
	}
	for _, endpoint := range scope.UnixEndpoints() {
		if len(endpoint) > nativeUnixEndpointLimit || !canonicalNativePath(endpoint) {
			return execution.Denied(catalog.ProcessRun, "Unix endpoint")
		}
		info, err := os.Lstat(endpoint)
		if err != nil {
			return execution.HostFailure("observe Process Unix endpoint", endpoint, err)
		}
		// Classify the file kind only. No permission bits participate in admission.
		if info.Mode().Type() != os.ModeSocket {
			return execution.Denied(catalog.ProcessRun, "Unix endpoint requires a socket")
		}
	}
	return nil
}

func cleanupFileIdentity(info os.FileInfo) (cleanupIdentity, uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return cleanupIdentity{}, 0, fmt.Errorf("native cleanup metadata has an invalid representation")
	}
	return cleanupIdentity{uint64(stat.Dev), stat.Ino}, uint64(stat.Nlink), nil
}

func (scan *cleanupScan) directory(root *os.Root, depth int, access cleanupAccess) (resultErr error) {
	if depth > cleanupDepthLimit {
		return execution.Limit("Process cleanup directory depth")
	}
	directory, err := root.Open(".")
	if err != nil {
		return execution.HostFailure("open Process cleanup directory", "root", err)
	}
	defer func() {
		if err := directory.Close(); err != nil {
			resultErr = errors.Join(resultErr, execution.HostFailure("close Process cleanup directory", "root", err))
		}
	}()
	for {
		if err := scan.ctx.Err(); err != nil {
			return err
		}
		entries, readErr := directory.ReadDir(256)
		for _, entry := range entries {
			if err := scan.ctx.Err(); err != nil {
				return err
			}
			scan.entries++
			if scan.entries > cleanupEntryLimit {
				return execution.Limit("Process cleanup entries")
			}
			info, err := root.Lstat(entry.Name())
			if err != nil {
				return execution.HostFailure("observe Process cleanup entry", entry.Name(), err)
			}
			identity, links, err := cleanupFileIdentity(info)
			if err != nil {
				return err
			}
			if !info.IsDir() {
				prior, exists := scan.links[identity]
				if exists && prior.declared != links {
					return execution.Denied(catalog.ProcessRun, "cleanup entry changed during admission")
				}
				prior.observed++
				if access == cleanupData {
					prior.dataNames++
				}
				prior.declared = links
				scan.links[identity] = prior
				continue
			}
			child, err := root.OpenRoot(entry.Name())
			if err != nil {
				return execution.HostFailure("open Process cleanup child", entry.Name(), err)
			}
			observed, statErr := child.Stat(".")
			if statErr == nil {
				actual, _, identityErr := cleanupFileIdentity(observed)
				statErr = identityErr
				if identityErr == nil && identity != actual {
					statErr = execution.Denied(catalog.ProcessRun, "cleanup directory changed during admission")
				}
			}
			if statErr == nil {
				statErr = scan.directory(child, depth+1, access)
			}
			if closeErr := child.Close(); closeErr != nil {
				statErr = errors.Join(statErr, execution.HostFailure("close Process cleanup child", entry.Name(), closeErr))
			}
			if statErr != nil {
				return statErr
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return execution.HostFailure("list Process cleanup directory", "root", readErr)
		}
	}
}
