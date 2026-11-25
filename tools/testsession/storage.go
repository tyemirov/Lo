package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
)

type storageIdentity struct{ device, inode uint64 }
type storageSnapshot map[storageIdentity]int64

type storageTracker struct {
	sync.Mutex
	work     string
	limit    int64
	caches   []string
	baseline storageSnapshot
	peak     int64
}

var persistentCacheVariables = map[string]bool{
	"GOCACHE": true, "GOMODCACHE": true, "GOPATH": true,
	"CARGO_TARGET_DIR": true, "UV_CACHE_DIR": true,
	"LO_NATIVE_PREPARATION": true, "LO_DISTRIBUTION_BUILD_CACHE": true,
}

func (tracker *storageTracker) declare(root string, base []string, spec manifest) error {
	tracker.Lock()
	defer tracker.Unlock()
	paths := map[string]bool{}
	add := func(key, value string) error {
		if !persistentCacheVariables[key] || value == "" {
			return nil
		}
		selected, err := localPath(root, strings.ReplaceAll(value, "{work}", tracker.work))
		if err != nil {
			return fmt.Errorf("persistent cache %s: %w", key, err)
		}
		if relative, err := filepath.Rel(tracker.work, selected); err == nil && filepath.IsLocal(relative) {
			return nil
		}
		if relative, err := filepath.Rel(selected, tracker.work); err == nil && filepath.IsLocal(relative) {
			return fmt.Errorf("persistent cache %s contains the session directory", key)
		}
		paths[selected] = true
		return nil
	}
	for _, entry := range append(append(append([]jobSpec{}, spec.Prepare...), spec.Groups...), spec.Finalize...) {
		values := map[string]string{}
		for _, item := range base {
			key, value, _ := strings.Cut(item, "=")
			values[key] = value
		}
		for key, value := range entry.Environment {
			values[key] = value
		}
		for key, value := range values {
			if err := add(key, value); err != nil {
				return err
			}
		}
	}
	for path := range paths {
		tracker.caches = append(tracker.caches, path)
	}
	slices.Sort(tracker.caches)
	baseline, err := snapshotStorage(tracker.caches)
	if err != nil {
		return fmt.Errorf("measure persistent cache baseline: %w", err)
	}
	tracker.baseline = baseline
	return nil
}

func snapshotStorage(paths []string) (storageSnapshot, error) {
	result := storageSnapshot{}
	for _, path := range paths {
		err := filepath.WalkDir(path, func(name string, entry os.DirEntry, walkErr error) error {
			if os.IsNotExist(walkErr) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || info.Size() < 0 {
				return fmt.Errorf("measure session file identity %s: unavailable device, inode, or size", name)
			}
			result[storageIdentity{uint64(stat.Dev), uint64(stat.Ino)}] = info.Size()
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func snapshotBytes(files, excluded storageSnapshot) (int64, error) {
	var total int64
	for identity, size := range files {
		if _, exists := excluded[identity]; exists {
			continue
		}
		if size > math.MaxInt64-total {
			return 0, fmt.Errorf("session storage size exceeds numeric range")
		}
		total += size
	}
	return total, nil
}

func (tracker *storageTracker) check() error {
	tracker.Lock()
	defer tracker.Unlock()
	owned, err := snapshotStorage([]string{tracker.work})
	if err != nil {
		return err
	}
	current, err := snapshotStorage(tracker.caches)
	if err != nil {
		return fmt.Errorf("measure persistent cache growth: %w", err)
	}
	ownedBytes, err := snapshotBytes(owned, nil)
	if err != nil {
		return err
	}
	cacheBytes, err := snapshotBytes(current, owned)
	if err != nil {
		return err
	}
	baselineBytes, err := snapshotBytes(tracker.baseline, owned)
	if err != nil {
		return err
	}
	growth := max(0, cacheBytes-baselineBytes)
	if growth <= math.MaxInt64-ownedBytes {
		tracker.peak = max(tracker.peak, ownedBytes+growth)
	}
	if ownedBytes > tracker.limit || growth > tracker.limit-ownedBytes {
		return fmt.Errorf("session storage limit at %s: owned=%d cache_growth=%d exceeds %d bytes", tracker.work, ownedBytes, growth, tracker.limit)
	}
	return nil
}

func (tracker *storageTracker) observedPeak() int64 {
	tracker.Lock()
	defer tracker.Unlock()
	return tracker.peak
}
