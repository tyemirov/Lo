package testsession

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestFixtureSessionPublicationCancellationMaintainsOwnership(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "artifacts"), os.ModePerm); err != nil {
		t.Fatal(err)
	}
	for index := range 64 {
		key := fmt.Sprintf("fixture-%d", index)
		staged := filepath.Join(directory, "staged-"+key)
		data := []byte("complete owned native fixture")
		if err := writeBytes(staged, data); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		record := &buildRecord{done: make(chan struct{}), cancel: cancel, waiters: 1}
		server := &Server{directory: directory, builds: map[string]*buildRecord{key: record}, maxBytes: int64(len(data) + 1)}
		ready := make(chan struct{})
		published := make(chan error, 1)
		released := make(chan struct{})
		go func() {
			<-ready
			_, err := server.publishArtifact(ctx, key, CompileRequest{Source: "fixture.lo"}, record, staged, int64(len(data)), "runtime")
			published <- err
		}()
		go func() {
			<-ready
			server.releaseBuild(key, record)
			close(released)
		}()
		close(ready)
		err := <-published
		<-released
		artifact := filepath.Join(directory, "artifacts", key)
		if err == nil {
			select {
			case <-record.done:
			default:
				t.Fatal("published artifact has no completed ownership record")
			}
			if server.builds[key] != record || server.stats.Bytes != int64(len(data)) || record.waiters != 0 {
				t.Fatalf("published artifact lost its ownership/accounting: %+v record=%+v", server.stats, record)
			}
			if contents, err := os.ReadFile(artifact); err != nil || string(contents) != string(data) {
				t.Fatalf("published executable differs from its complete staged input: %q %v", contents, err)
			}
			server.mu.Lock()
			evictionErr := server.makeSpaceLocked(server.maxBytes)
			server.mu.Unlock()
			if evictionErr != nil || server.stats.Bytes != 0 || len(server.builds) != 0 {
				t.Fatalf("completed inactive result cannot be evicted: %v %+v", evictionErr, server.stats)
			}
		} else {
			if _, err := os.Stat(artifact); !os.IsNotExist(err) || server.stats.Bytes != 0 || len(server.builds) != 0 {
				t.Fatalf("cancelled generation left an orphan or accounting: artifact=%v stats=%+v", err, server.stats)
			}
			if err := os.Remove(staged); err != nil {
				t.Fatal(err)
			}
		}
		cancel()
	}
}
