package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func (fixture macCleanupFixture) treeSource(t *testing.T, apply bool, behavior, opened, changed, tree string) string {
	t.Helper()
	return cleanupFlows(t, "StaleBuildTree") + fmt.Sprintf(`
flow TreeFixture() -> Int! {
    lock = Lock.File.new(Program.path())
    lease = commit lock
    environment = Map.new(Record.new(key: "PATH", value: %q), Record.new(key: "FIXTURE_CALLS", value: %q), Record.new(key: "FIXTURE_BEHAVIOR", value: %q), Record.new(key: "FIXTURE_OPENED", value: %q), Record.new(key: "FIXTURE_CHANGED", value: %q))
    process = Process.Context.new(environment, File.Path.new(%q))
    context = Record.new(apply: %t, home: File.Path.new(%q), uid: %d, process: process)
    parent = File.directory(File.Path.new(%q))
    root = File.Directory.entry(parent, %q)
    return StaleBuildTree(context, parent, root, 0)
}
return TreeFixture()`, fixture.root, fixture.calls, behavior, opened, changed, fixture.root, apply, fixture.root, os.Getuid(), fixture.writes, filepath.Base(tree))
}

func (fixture macCleanupFixture) treeFlags(t *testing.T, apply bool, tree string) []string {
	t.Helper()
	flags := []string{"--lock-entry", "--observe-tree-root", fixture.writes, "--max-steps", "10000000"}
	if apply {
		flags = append(flags, fixture.flags()...)
		flags = append(flags, "--max-process-ms", "30000", "--remove-tree-root", tree,
			"--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes),
			"--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.prunes))
	}
	return flags
}

func TestMacCleanupWholeTree(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(fmt.Sprintf("apply-%t", apply), func(t *testing.T) {
			fixture := newMacCleanupFixture(t)
			tree := f033Directory(t, fixture.writes, "Build space ü")
			nested := f033Directory(t, tree, "nested")
			fileFixture(t, tree, "a.o", "object")
			fileFixture(t, nested, "b.o", "nested object")
			sibling := fileFixture(t, fixture.writes, "sibling", "preserved")
			if err := os.Symlink(sibling, filepath.Join(tree, "link")); err != nil {
				t.Fatal(err)
			}
			got := runProgram(t, sourceFile(t, fixture.treeSource(t, apply, "success", "", "", tree)), "run", fixture.treeFlags(t, apply, tree)...)
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("whole tree: %+v", got)
			}
			macCleanupPresent(t, tree, !apply)
			macCleanupPresent(t, sibling, true)
			macCleanupPresent(t, fixture.writes, true)
			calls, err := os.ReadFile(fixture.calls)
			if err != nil {
				t.Fatal(err)
			}
			if apply {
				if string(calls) != "-F0n\n+D\n"+tree+"\n" || !strings.Contains(got.stdout, "Deleted whole build tree: "+tree) {
					t.Fatalf("whole tree probe or result: calls=%q result=%+v", calls, got)
				}
			} else if len(calls) != 0 || !strings.Contains(got.stdout, "PLAN whole stale build tree: "+tree) {
				t.Fatalf("whole tree preview: calls=%q result=%+v", calls, got)
			}
		})
	}
}

func TestMacCleanupWholeTreeProbePolicies(t *testing.T) {
	for _, test := range []struct {
		name, behavior, opened string
		status                 int
	}{
		{"root-handle", "success", "root", 0},
		{"file-handle", "success", "file", 0},
		{"unparsed-output", "malformed", "", 0},
		{"stderr", "stderr", "", 1},
		{"whitespace", "whitespace", "", 1},
		{"exit-status", "status2", "", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMacCleanupFixture(t)
			tree := f033Directory(t, fixture.writes, "Build")
			entry := fileFixture(t, tree, "a.o", "object")
			opened := ""
			if test.opened == "root" {
				opened = tree
			} else if test.opened == "file" {
				opened = entry
			}
			got := runProgram(t, sourceFile(t, fixture.treeSource(t, true, test.behavior, opened, "", tree)), "run", fixture.treeFlags(t, true, tree)...)
			if got.status != test.status || strings.Contains(got.stdout, "Deleted whole build tree:") {
				t.Fatalf("whole tree probe policy: %+v", got)
			}
			if test.status == 0 {
				if got.stderr != "" || !strings.Contains(got.stdout, "SKIP open build tree: "+tree) {
					t.Fatalf("whole tree output was ignored: %+v", got)
				}
			} else if !strings.Contains(got.stderr, "Cleanup.OpenFiles") {
				t.Fatalf("whole tree probe error: %+v", got)
			}
			macCleanupPresent(t, tree, true)
			macCleanupPresent(t, entry, true)
		})
	}
}

func TestMacCleanupWholeTreeChangedByProbe(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	tree := f033Directory(t, fixture.writes, "Build")
	changed := fileFixture(t, tree, "a.o", "before")
	other := fileFixture(t, tree, "b.o", "preserved")
	got := runProgram(t, sourceFile(t, fixture.treeSource(t, true, "success", "", changed, tree)), "run", fixture.treeFlags(t, true, tree)...)
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "SKIP changed build tree: "+tree) || strings.Contains(got.stdout, "Deleted whole build tree:") {
		t.Fatalf("changed tree: %+v", got)
	}
	data, err := os.ReadFile(changed)
	if err != nil || string(data) != "changed" {
		t.Fatalf("probe did not change its selected fixture: %q, %v", data, err)
	}
	macCleanupPresent(t, other, true)
}

func TestMacCleanupWholeTreeIneligibleNeedsNoMutationAuthority(t *testing.T) {
	for _, reason := range []string{"recent", "hardlink"} {
		t.Run(reason, func(t *testing.T) {
			fixture := newMacCleanupFixture(t)
			tree := f033Directory(t, fixture.writes, "Build")
			entry := fileFixture(t, tree, "a.o", "preserved")
			if reason == "recent" {
				future := time.Now().Add(24 * time.Hour)
				if err := os.Chtimes(entry, future, future); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Link(entry, filepath.Join(fixture.writes, "outside-alias")); err != nil {
				t.Fatal(err)
			}
			got := runProgram(t, sourceFile(t, fixture.treeSource(t, true, "success", "", "", tree)), "run", fixture.treeFlags(t, false, tree)...)
			if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "SKIP active/recent/unsupported build tree: "+tree) {
				t.Fatalf("ineligible tree without mutation authority: %+v", got)
			}
			macCleanupPresent(t, entry, true)
			calls, err := os.ReadFile(fixture.calls)
			if err != nil || len(calls) != 0 {
				t.Fatalf("ineligible tree started probes: %q, %v", calls, err)
			}
		})
	}
}

func TestMacCleanupWholeTreeRootChangedByProbe(t *testing.T) {
	for _, behavior := range []string{"rename-root", "replace-root"} {
		t.Run(behavior, func(t *testing.T) {
			fixture := newMacCleanupFixture(t)
			tree := f033Directory(t, fixture.writes, "Build")
			fileFixture(t, tree, "a.o", "original")
			replacement := f033Directory(t, fixture.writes, "Build-replacement")
			fileFixture(t, replacement, "replacement.o", "replacement")
			got := runProgram(t, sourceFile(t, fixture.treeSource(t, true, behavior, "", tree, tree)), "run", fixture.treeFlags(t, true, tree)...)
			if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "SKIP changed build tree: "+tree) || strings.Contains(got.stdout, "Deleted whole build tree:") {
				t.Fatalf("changed root: %+v", got)
			}
			macCleanupPresent(t, filepath.Join(tree+"-retained", "a.o"), true)
			if behavior == "replace-root" {
				macCleanupPresent(t, filepath.Join(tree, "replacement.o"), true)
			} else {
				macCleanupPresent(t, tree, false)
				macCleanupPresent(t, filepath.Join(replacement, "replacement.o"), true)
			}
		})
	}
}
