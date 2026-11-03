package integration_test

import (
	"testing"
	"time"
)

// OpenTrusted is the explicit Go-embedding seam for the same session assembly
// used by Open. Config contains no qualification bypass or Creator field.

type sessionDiscard struct{}

func (sessionDiscard) Write(data []byte) (int, error) { return len(data), nil }
func (sessionDiscard) Close() error                   { return nil }

func sessionAwait(t *testing.T, event <-chan struct{}, subject string) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", subject)
	}
}

func sessionStillOwned(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("retained ownership returned before release: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
}

// Historical timeout must not keep WaitRetained blocked once ownership settled.
