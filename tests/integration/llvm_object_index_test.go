package integration_test

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/sys/unix"
)

const runtimeObjectIndexName = "object-index.json"
const runtimeObjectIndexLimit = 4 << 20

type runtimeObjectIndex struct {
	Entries map[string]int64 `json:"entries"`
	Total   int64            `json:"total"`
}

func runtimeObjectKey(key string) bool {
	decoded, err := hex.DecodeString(key)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == key
}

func runtimeReadObjectIndex(root string) (runtimeObjectIndex, error) {
	file, err := os.Open(filepath.Join(root, runtimeObjectIndexName))
	if err != nil {
		return runtimeObjectIndex{}, err
	}
	var document struct {
		Entries map[string]int64 `json:"entries"`
		Total   *int64           `json:"total"`
	}
	decoder := json.NewDecoder(io.LimitReader(file, runtimeObjectIndexLimit+1))
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&document)
	if decodeErr == nil && decoder.InputOffset() > runtimeObjectIndexLimit {
		decodeErr = fmt.Errorf("runtime object index exceeds %d bytes", runtimeObjectIndexLimit)
	}
	if decodeErr == nil {
		if err := decoder.Decode(new(any)); err != io.EOF {
			decodeErr = fmt.Errorf("runtime object index contains trailing data: %v", err)
		}
	}
	if err := errors.Join(decodeErr, file.Close()); err != nil {
		return runtimeObjectIndex{}, fmt.Errorf("read runtime object index: %w", err)
	}
	if document.Entries == nil || document.Total == nil || *document.Total < 0 {
		return runtimeObjectIndex{}, fmt.Errorf("runtime object index requires entries and a nonnegative total")
	}
	var total int64
	for key, size := range document.Entries {
		if !runtimeObjectKey(key) || size <= 0 || size > math.MaxInt64-total {
			return runtimeObjectIndex{}, fmt.Errorf("runtime object index contains an invalid key or size")
		}
		total += size
	}
	if total != *document.Total {
		return runtimeObjectIndex{}, fmt.Errorf("runtime object index total differs from its entries: total=%d entries=%d", *document.Total, total)
	}
	return runtimeObjectIndex{document.Entries, total}, nil
}

func runtimeWriteObjectIndex(root string, index runtimeObjectIndex) (failure error) {
	file, err := os.CreateTemp(root, ".object-index-")
	if err != nil {
		return fmt.Errorf("create runtime object index: %w", err)
	}
	staged := file.Name()
	defer func() {
		if err := os.Remove(staged); err != nil && !os.IsNotExist(err) {
			failure = errors.Join(failure, err)
		}
	}()
	if err := errors.Join(json.NewEncoder(file).Encode(index), file.Close()); err != nil {
		return fmt.Errorf("write runtime object index: %w", err)
	}
	if err := os.Rename(staged, filepath.Join(root, runtimeObjectIndexName)); err != nil {
		return fmt.Errorf("publish runtime object index: %w", err)
	}
	return nil
}

func runtimeCompletedObjectBytes(root, key string) (int64, error) {
	directory := filepath.Join(root, "objects", key)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0, err
	}
	if len(entries) == 0 || len(entries) > 3 {
		return 0, fmt.Errorf("runtime object entry requires its bounded completed files")
	}
	var total int64
	completed := false
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			return 0, fmt.Errorf("runtime object entry contains a nested directory: %s", path)
		}
		if _, err := os.Readlink(path); err == nil {
			return 0, fmt.Errorf("runtime object entry contains a symlink: %s", path)
		} else if !errors.Is(err, unix.EINVAL) {
			return 0, fmt.Errorf("resolve completed runtime object %s: %w", path, err)
		}
		info, err := entry.Info()
		if err != nil {
			return 0, err
		}
		if info.Size() < 0 || info.Size() > math.MaxInt64-total {
			return 0, fmt.Errorf("runtime object entry size exceeds its counter")
		}
		total += info.Size()
		completed = completed || entry.Name() == "object.o"
	}
	if !completed || total <= 0 {
		return 0, fmt.Errorf("runtime object entry lacks its completed object")
	}
	return total, nil
}

// The caller owns the storage lock and the active entry's compilation lock.
func runtimeRegisterObjectCache(root, active string, limit int64) error {
	if !runtimeObjectKey(active) || limit < 1 {
		return fmt.Errorf("runtime object registration requires a digest key and positive storage limit")
	}
	index, err := runtimeReadObjectIndex(root)
	if os.IsNotExist(err) {
		index = runtimeObjectIndex{Entries: map[string]int64{}}
	} else if err != nil {
		return err
	}
	if _, exists := index.Entries[active]; exists {
		return fmt.Errorf("runtime object index already contains the new completed entry: %s", active)
	}
	size, err := runtimeCompletedObjectBytes(root, active)
	if err != nil {
		return fmt.Errorf("size completed runtime object: %w", err)
	}
	if size > math.MaxInt64-index.Total {
		return fmt.Errorf("runtime object index total overflow")
	}
	index.Entries[active] = size
	index.Total += size
	keys := make([]string, 0, len(index.Entries))
	if index.Total > limit {
		for key := range index.Entries {
			keys = append(keys, key)
		}
		slices.Sort(keys)
	}
	for _, key := range keys {
		if index.Total <= limit {
			break
		}
		if key == active {
			continue
		}
		file, err := os.OpenFile(filepath.Join(root, "locks", key), os.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("open indexed runtime object lock: %w", err)
		}
		if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			closeErr := file.Close()
			if errors.Is(err, unix.EWOULDBLOCK) && closeErr == nil {
				continue
			}
			return errors.Join(err, closeErr)
		}
		if _, err := os.Stat(filepath.Join(root, "objects", key, "object.o")); err != nil {
			return errors.Join(fmt.Errorf("indexed runtime object is missing: %w", err), file.Close())
		}
		if err := errors.Join(os.RemoveAll(filepath.Join(root, "objects", key)), file.Close()); err != nil {
			return fmt.Errorf("evict inactive runtime object: %w", err)
		}
		index.Total -= index.Entries[key]
		delete(index.Entries, key)
	}
	if index.Total > limit {
		if err := os.RemoveAll(filepath.Join(root, "objects", active)); err != nil {
			return fmt.Errorf("release current runtime object cache entry: %w", err)
		}
		index.Total -= size
		delete(index.Entries, active)
	}
	return runtimeWriteObjectIndex(root, index)
}

func runtimeRequireObjectCacheEntry(t *testing.T, root, key string) {
	t.Helper()
	unlock := runtimeCacheLock(t, root, "storage")
	defer unlock()
	index, err := runtimeReadObjectIndex(root)
	if err != nil {
		t.Fatalf("validate reused runtime object index: %v", err)
	}
	if _, ok := index.Entries[key]; !ok {
		t.Fatalf("completed runtime object is absent from its accounted index: %s", key)
	}
}
