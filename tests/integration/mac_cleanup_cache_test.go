package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (fixture macCleanupFixture) cacheSource(t *testing.T, apply bool, behavior, opened, changed string) string {
	t.Helper()
	return cleanupFlows(t, "StaleCache") + fmt.Sprintf(`
flow CacheFixture() -> Int! {
    lock = Lock.File.new(Program.path())
    lease = commit lock
    environment = Map.new(Record.new(key: "PATH", value: %q), Record.new(key: "FIXTURE_CALLS", value: %q), Record.new(key: "FIXTURE_BEHAVIOR", value: %q), Record.new(key: "FIXTURE_OPENED", value: %q), Record.new(key: "FIXTURE_CHANGED", value: %q))
    process = Process.Context.new(environment, File.Path.new(%q))
    context = Record.new(apply: %t, home: File.Path.new(%q), uid: %d, process: process)
    return StaleCache(context, File.Path.new(%q), 0)
}
return CacheFixture()`, fixture.root, fixture.calls, behavior, opened, changed, fixture.root, apply, fixture.root, os.Getuid(), fixture.writes)
}

func (fixture macCleanupFixture) cacheFlags(t *testing.T, apply bool) []string {
	t.Helper()
	flags := []string{"--lock-entry", "--observe-tree-root", fixture.writes, "--max-steps", "10000000"}
	if apply {
		flags = append(flags, fixture.flags()...)
		flags = append(flags, "--max-process-ms", "30000", "--remove-tree-root", fixture.writes,
			"--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes),
			"--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.prunes))
	}
	return flags
}

func TestMacCleanupCacheTraversal(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(fmt.Sprintf("apply-%t", apply), func(t *testing.T) {
			fixture := newMacCleanupFixture(t)
			left := f033Directory(t, fixture.writes, "a")
			nested := f033Directory(t, left, "nested")
			right := f033Directory(t, fixture.writes, "z")
			rootFile := fileFixture(t, fixture.writes, "root.cache", "root")
			opened := fileFixture(t, left, "open.cache", "opened")
			changed := fileFixture(t, left, "changed.cache", "before")
			unicode := fileFixture(t, nested, "é.cache", "unicode")
			hidden := fileFixture(t, right, ".hidden", "hidden")
			unsafe := fileFixture(t, right, "unsafe\n.cache", "unsafe")
			sibling := fileFixture(t, fixture.prunes, "sibling.cache", "sibling")
			linked := filepath.Join(right, "linked")
			if err := os.Link(sibling, linked); err != nil {
				t.Fatal(err)
			}
			symlink := filepath.Join(left, "loop")
			if err := os.Symlink(fixture.writes, symlink); err != nil {
				t.Fatal(err)
			}
			got := runProgram(t, sourceFile(t, fixture.cacheSource(t, apply, "scoped", opened, changed)), "run", fixture.cacheFlags(t, apply)...)
			if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "6 stale files") {
				t.Fatalf("cache traversal: %+v", got)
			}
			for _, path := range []string{rootFile, unicode, hidden} {
				macCleanupPresent(t, path, !apply)
			}
			for _, path := range []string{fixture.writes, left, nested, right, opened, changed, unsafe, sibling, linked, symlink} {
				macCleanupPresent(t, path, true)
			}
			calls, err := os.ReadFile(fixture.calls)
			if err != nil {
				t.Fatal(err)
			}
			if apply {
				want := "-F0n\n--\n" + rootFile + "\n-F0n\n--\n" + changed + "\n" + opened + "\n-F0n\n--\n" + unicode + "\n-F0n\n--\n" + hidden + "\n"
				if string(calls) != want || !strings.Contains(got.stdout, "deleted 3") || !strings.Contains(got.stdout, "skipped 3") {
					t.Fatalf("cache order or counters: calls=%q, output=%+v", calls, got)
				}
			} else if len(calls) != 0 {
				t.Fatalf("cache preview started probes: %q", calls)
			}
		})
	}
}

func TestMacCleanupCachePartialProgress(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	left := f033Directory(t, fixture.writes, "a")
	right := f033Directory(t, fixture.writes, "z")
	removed := fileFixture(t, fixture.writes, "root.cache", "first")
	failed := fileFixture(t, left, "failed.cache", "failure")
	untouched := fileFixture(t, right, "later.cache", "later")
	got := runProgram(t, sourceFile(t, fixture.cacheSource(t, true, "later-fails", "", "")), "run", fixture.cacheFlags(t, true)...)
	if got.status != 1 || !strings.Contains(got.stderr, "Cleanup.OpenFiles") || !strings.Contains(got.stdout, "deleted 1") {
		t.Fatalf("cache partial failure: %+v", got)
	}
	macCleanupPresent(t, removed, false)
	macCleanupPresent(t, failed, true)
	macCleanupPresent(t, untouched, true)
	calls, err := os.ReadFile(fixture.calls)
	want := "-F0n\n--\n" + removed + "\n-F0n\n--\n" + failed + "\n"
	if err != nil || string(calls) != want {
		t.Fatalf("cache continued after failure: %q, %v", calls, err)
	}
}

func TestMacCleanupCacheBoundedWalk(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	parent := fixture.writes
	for index := 0; index < 140; index++ {
		f033Directory(t, parent, "z")
		parent = f033Directory(t, parent, "a")
	}
	got := runProgram(t, sourceFile(t, fixture.cacheSource(t, false, "scoped", "", "")), "run", fixture.cacheFlags(t, false)...)
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "0 stale files") {
		t.Fatalf("cache retained-ancestor walk: %+v", got)
	}
	macCleanupPresent(t, parent, true)
}

func TestMacCleanupCacheAbsentRoot(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	fixture.writes = filepath.Join(fixture.writes, "absent")
	got := runProgram(t, sourceFile(t, fixture.cacheSource(t, false, "scoped", "", "")), "run", fixture.cacheFlags(t, false)...)
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "SKIP absent cache: "+fixture.writes) {
		t.Fatalf("absent cache: %+v", got)
	}
	macCleanupPresent(t, fixture.writes, false)
}

func TestMacCleanupCacheEmptyApplyNeedsNoMutationAuthority(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	got := runProgram(t, sourceFile(t, fixture.cacheSource(t, true, "scoped", "", "")), "run", fixture.cacheFlags(t, false)...)
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "0 stale files") || !strings.Contains(got.stdout, "deleted 0") {
		t.Fatalf("empty cleanup without process/removal authority: %+v", got)
	}
	calls, err := os.ReadFile(fixture.calls)
	if err != nil || len(calls) != 0 {
		t.Fatalf("empty cleanup started probes: %q, %v", calls, err)
	}
}

func TestMacCleanupCacheProbePolicies(t *testing.T) {
	for _, test := range []struct{ name, behavior, kind string }{
		{"stderr", "stderr", "Cleanup.OpenFiles"},
		{"whitespace", "whitespace", "Cleanup.OpenFiles"},
		{"exit-status", "status2", "Cleanup.OpenFiles"},
		{"invalid-utf8", "malformed", "UTF8.InvalidBytes"},
		{"outside-batch", "success", "Cleanup.OpenFiles"},
		{"newline-framing", "newline-framed-open", "Cleanup.OpenFiles"},
		{"unterminated", "unterminated-open", "Cleanup.OpenFiles"},
		{"embedded-newline", "embedded-newline-open", "Cleanup.OpenFiles"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMacCleanupFixture(t)
			selected := fileFixture(t, fixture.writes, "old.cache", "old")
			opened := ""
			if test.name == "outside-batch" {
				opened = fileFixture(t, fixture.prunes, "outside.cache", "outside")
			} else if strings.Contains(test.behavior, "open") {
				opened = selected
			}
			got := runProgram(t, sourceFile(t, fixture.cacheSource(t, true, test.behavior, opened, "")), "run", fixture.cacheFlags(t, true)...)
			if got.status != 1 || !strings.Contains(got.stderr, test.kind) || !strings.Contains(got.stdout, "deleted 0") {
				t.Fatalf("cache probe policy: %+v", got)
			}
			macCleanupPresent(t, selected, true)
		})
	}
}

func TestMacCleanupCacheOwnerAfterProbe(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	selected := fileFixture(t, fixture.writes, "old.cache", "old")
	arguments := append([]string{"fixture", "run"}, fixture.cacheFlags(t, true)...)
	quoted := make([]string, len(arguments))
	for index, argument := range arguments {
		quoted[index] = fmt.Sprintf("%q", argument)
	}
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{
		name: "owner-after-probe", source: fixture.cacheSource(t, true, "scoped", "", ""),
		harness: "testdata/native-runtime/mac-cleanup-cache-observer.c",
		transform: func(t *testing.T, module []byte) []byte {
			t.Helper()
			const entry = "define void @lo_entry("
			if strings.Count(string(module), entry) != 1 {
				t.Fatal("missing unique cache entry")
			}
			return []byte(strings.Replace(string(module), entry, "define void @lo_cleanup_entry(", 1))
		},
		harnessTransform: func(t *testing.T, data []byte) []byte {
			t.Helper()
			return []byte(strings.NewReplacer("LO_FIXTURE_CALLS", fmt.Sprintf("%q", fixture.calls), "LO_FIXTURE_ARGUMENTS", strings.Join(quoted, ", ")).Replace(string(data)))
		},
		verify: func(t *testing.T, actual result) {
			t.Helper()
			if actual.status != 0 || actual.stderr != "" || !strings.Contains(actual.stdout, "Cleanup.CacheDirectory") || !strings.Contains(actual.stdout, "deleted 0") {
				t.Fatalf("owner changed after probe: %+v", actual)
			}
			macCleanupPresent(t, selected, true)
			calls, err := os.ReadFile(fixture.calls)
			if err != nil || string(calls) != "-F0n\n--\n"+selected+"\n" {
				t.Fatalf("owner probe calls: %q, %v", calls, err)
			}
		},
	}})
}

func TestMacCleanupCacheProbeRecordBoundaries(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	opened := fileFixture(t, fixture.writes, "open.cache", "open")
	closed := fileFixture(t, fixture.writes, "closed.cache", "closed")
	got := runProgram(t, sourceFile(t, fixture.cacheSource(t, true, "record-boundaries", opened, "")), "run", fixture.cacheFlags(t, true)...)
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "2 stale files") || !strings.Contains(got.stdout, "deleted 1") || !strings.Contains(got.stdout, "skipped 1") {
		t.Fatalf("probe process/file records: %+v", got)
	}
	macCleanupPresent(t, opened, true)
	macCleanupPresent(t, closed, false)
}
