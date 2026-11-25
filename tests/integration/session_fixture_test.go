package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/testsession"
)

func TestSessionRuntimeFixturesReuseImmutableValues(t *testing.T) {
	server, err := testsession.Start(context.Background(), testsession.Config{
		Directory:  filepath.Join(t.TempDir(), "session"),
		Toolchain:  os.Getenv("LO_LLVM_TOOLCHAIN"),
		Compiler:   binary,
		BuildSlots: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	})
	t.Setenv("LO_TEST_SESSION", server.Address())
	for range 2 {
		valueJSON(t, runValue(t, "return Math.add(2, 3)"), `"5"`)
	}
	if stats := server.Stats(); stats.Builds != 1 || stats.Reused != 1 {
		t.Fatalf("value fixture preparation repeated: %+v", stats)
	}
}
