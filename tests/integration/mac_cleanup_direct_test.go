package integration_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type macCleanupFixture struct {
	cleanupFixture
	calls string
}

func newMacCleanupFixture(t *testing.T) macCleanupFixture {
	t.Helper()
	fixture := newCleanupFixtureSource(t, "mac-cleanup-probe.c")
	name := filepath.Join(fixture.root, "lsof")
	if err := os.Rename(fixture.executable, name); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	fixture.executable = name
	fixture.grant = fmt.Sprintf("%x,%s", sha256.Sum256(data), name)
	return macCleanupFixture{fixture, fileFixture(t, fixture.prunes, "calls", "")}
}

func (fixture macCleanupFixture) directSource(t *testing.T, gateway, apply bool, behavior, opened, changed string) string {
	t.Helper()
	flow := "StaleFiles"
	call := fmt.Sprintf("StaleFiles(context, File.Path.new(%q), List.new(\"*.tmp\"), 0)", fixture.writes)
	if gateway {
		flow = "GatewayPresent"
		call = fmt.Sprintf("GatewayPresent(context, File.Path.new(%q), 0)", fixture.writes)
	}
	return cleanupFlows(t, flow) + fmt.Sprintf(`
flow DirectFixture() -> Int! {
    lock = Lock.File.new(Program.path())
    lease = commit lock
    environment = Map.new(Record.new(key: "PATH", value: %q), Record.new(key: "FIXTURE_CALLS", value: %q), Record.new(key: "FIXTURE_BEHAVIOR", value: %q), Record.new(key: "FIXTURE_OPENED", value: %q), Record.new(key: "FIXTURE_CHANGED", value: %q))
    process = Process.Context.new(environment, File.Path.new(%q))
    context = Record.new(apply: %t, home: File.Path.new(%q), uid: %d, process: process)
    return %s
}
return DirectFixture()`, fixture.root, fixture.calls, behavior, opened, changed, fixture.root, apply, fixture.root, os.Getuid(), call)
}

func (fixture macCleanupFixture) directFlags(t *testing.T, apply bool) []string {
	t.Helper()
	flags := []string{"--lock-entry", "--list-root", fixture.writes}
	if apply {
		flags = append(flags, fixture.flags()...)
		flags = append(flags, "--max-process-ms", "30000", "--remove-root", fixture.writes,
			"--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes),
			"--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.prunes))
	}
	return flags
}

func macCleanupPresent(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Lstat(path)
	if want && err != nil || !want && !os.IsNotExist(err) {
		t.Fatalf("presence %s: want %t, error %v", path, want, err)
	}
}

func TestMacCleanupDirectFiles(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(fmt.Sprintf("apply-%t", apply), func(t *testing.T) {
			fixture := newMacCleanupFixture(t)
			removed := fileFixture(t, fixture.writes, "old.tmp", "old")
			opened := fileFixture(t, fixture.writes, "open.tmp", "opened")
			changed := fileFixture(t, fixture.writes, "changed.tmp", "before")
			preserved := []string{
				fileFixture(t, fixture.writes, ".hidden.tmp", "hidden"),
				fileFixture(t, fixture.writes, "unselected.log", "other"),
				fileFixture(t, fixture.writes, "unsafe\n.tmp", "unsafe"),
				fileFixture(t, fixture.prunes, "sibling.tmp", "sibling"),
			}
			hardlink := filepath.Join(fixture.writes, "linked.tmp")
			if err := os.Link(preserved[3], hardlink); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(fixture.writes, "symlink.tmp")
			if err := os.Symlink(preserved[3], link); err != nil {
				t.Fatal(err)
			}
			preserved = append(preserved, hardlink, link)
			got := runProgram(t, sourceFile(t, fixture.directSource(t, false, apply, "success", opened, changed)), "run", fixture.directFlags(t, apply)...)
			if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "3 eligible files") {
				t.Fatalf("direct category: %+v", got)
			}
			macCleanupPresent(t, removed, !apply)
			for _, path := range append(preserved, opened, changed) {
				macCleanupPresent(t, path, true)
			}
			calls, err := os.ReadFile(fixture.calls)
			if err != nil {
				t.Fatal(err)
			}
			if apply {
				want := "-F0n\n--\n" + changed + "\n" + removed + "\n" + opened + "\n"
				if string(calls) != want || !strings.Contains(got.stdout, "Deleted 1 files") || !strings.Contains(got.stdout, "SKIP changed file: "+changed) || !strings.Contains(got.stdout, "SKIP open file: "+opened) {
					t.Fatalf("probe or confirmed outcome: calls=%q, output=%+v", calls, got)
				}
			} else if len(calls) != 0 {
				t.Fatalf("dry run started a probe: %q", calls)
			}
		})
	}
}

func TestMacCleanupDirectProbePolicies(t *testing.T) {
	for _, test := range []struct {
		name, behavior   string
		gateway, success bool
	}{
		{"candidate-stderr", "stderr", false, false},
		{"candidate-whitespace", "whitespace", false, false},
		{"candidate-exit-status", "status2", false, false},
		{"candidate-invalid-utf8", "malformed", false, false},
		{"gateway-whitespace", "whitespace", true, true},
		{"gateway-stderr", "stderr", true, false},
		{"gateway-exit-status", "status2", true, false},
		{"gateway-invalid-utf8", "malformed", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMacCleanupFixture(t)
			name := "old.tmp"
			if test.gateway {
				name = "deployment-123.log"
			}
			selected := fileFixture(t, fixture.writes, name, "old")
			got := runProgram(t, sourceFile(t, fixture.directSource(t, test.gateway, true, test.behavior, "", "")), "run", fixture.directFlags(t, true)...)
			wantStatus := 1
			if test.success {
				wantStatus = 0
			}
			if got.status != wantStatus || strings.Contains(got.stderr, `"diagnostics"`) {
				t.Fatalf("probe failure contract: %+v", got)
			}
			if !test.success {
				wantKind := "Cleanup.OpenFiles"
				if test.behavior == "malformed" {
					wantKind = "UTF8.InvalidBytes"
				}
				if !strings.Contains(got.stderr, wantKind) {
					t.Fatalf("wrong probe failure: want %s, result=%+v", wantKind, got)
				}
			}
			macCleanupPresent(t, selected, !test.success)
		})
	}
}

func TestMacCleanupGatewaySelection(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	removed := fileFixture(t, fixture.writes, "deployment-123.log", "old")
	opened := fileFixture(t, fixture.writes, "deployment-124.log", "opened")
	changed := fileFixture(t, fixture.writes, "deployment-125.log", "before")
	preserved := []string{
		fileFixture(t, fixture.writes, "deployment-12x.log", "other"),
		fileFixture(t, fixture.writes, "deployment-.log", "other"),
		fileFixture(t, fixture.writes, "deployment-126.log.bak", "other"),
	}
	got := runProgram(t, sourceFile(t, fixture.directSource(t, true, true, "success", opened, changed)), "run", fixture.directFlags(t, true)...)
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "Deleted 1 old Gateway logs") {
		t.Fatalf("Gateway category: %+v", got)
	}
	macCleanupPresent(t, removed, false)
	for _, path := range append(preserved, opened, changed) {
		macCleanupPresent(t, path, true)
	}
	calls, err := os.ReadFile(fixture.calls)
	if err != nil || string(calls) != "-Fn\n+D\n"+fixture.writes+"\n" {
		t.Fatalf("Gateway probe framing: %q, %v", calls, err)
	}
}

func TestMacCleanupDirectMissingDirectory(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	if err := os.Remove(fixture.writes); err != nil {
		t.Fatal(err)
	}
	got := runProgram(t, sourceFile(t, fixture.directSource(t, false, true, "success", "", "")), "run", fixture.directFlags(t, false)...)
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "SKIP absent directory: "+fixture.writes) {
		t.Fatalf("missing directory: %+v", got)
	}
	calls, err := os.ReadFile(fixture.calls)
	if err != nil || len(calls) != 0 {
		t.Fatalf("missing directory started a probe: %q, %v", calls, err)
	}
}

func TestMacCleanupDirectVanishedEntry(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	vanished := fileFixture(t, fixture.writes, "a.tmp", "gone during probe")
	removed := fileFixture(t, fixture.writes, "b.tmp", "removed by application")
	got := runProgram(t, sourceFile(t, fixture.directSource(t, false, true, "vanish", "", vanished)), "run", fixture.directFlags(t, true)...)
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "Deleted 1 files") {
		t.Fatalf("vanished entry counted as removal: %+v", got)
	}
	macCleanupPresent(t, vanished, false)
	macCleanupPresent(t, removed, false)
}

func TestMacCleanupDirectBatches(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	var paths []string
	for index := 0; index < 101; index++ {
		paths = append(paths, fileFixture(t, fixture.writes, fmt.Sprintf("old-%03d.tmp", index), "old"))
	}
	got := runProgram(t, sourceFile(t, fixture.directSource(t, false, true, "success", "", "")), "run", append(fixture.directFlags(t, true), "--max-steps", "10000000")...)
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, "Deleted 101 files") {
		t.Fatalf("candidate batching: %+v", got)
	}
	want := "-F0n\n--\n" + strings.Join(paths[:100], "\n") + "\n-F0n\n--\n" + paths[100] + "\n"
	calls, err := os.ReadFile(fixture.calls)
	if err != nil || string(calls) != want {
		t.Fatalf("candidate batch boundary: %q, %v", calls, err)
	}
	for _, path := range paths {
		macCleanupPresent(t, path, false)
	}
}

func TestMacCleanupDirectRejectsNamesOutsideBatch(t *testing.T) {
	fixture := newMacCleanupFixture(t)
	selected := fileFixture(t, fixture.writes, "old.tmp", "preserved")
	outside := fileFixture(t, fixture.prunes, "outside.tmp", "unrelated")
	got := runProgram(t, sourceFile(t, fixture.directSource(t, false, true, "success", outside, "")), "run", fixture.directFlags(t, true)...)
	if got.status != 1 || !strings.Contains(got.stderr, "Cleanup.OpenFiles") {
		t.Fatalf("out-of-batch probe result accepted: %+v", got)
	}
	macCleanupPresent(t, selected, true)
	macCleanupPresent(t, outside, true)
}

func TestMacCleanupDirectEmptySelection(t *testing.T) {
	for _, gateway := range []bool{false, true} {
		t.Run(fmt.Sprintf("gateway-%t", gateway), func(t *testing.T) {
			fixture := newMacCleanupFixture(t)
			preserved := fileFixture(t, fixture.writes, "unselected.txt", "preserved")
			got := runProgram(t, sourceFile(t, fixture.directSource(t, gateway, true, "success", "", "")), "run", fixture.directFlags(t, false)...)
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("empty selection required probe or removal authority: %+v", got)
			}
			macCleanupPresent(t, preserved, true)
			calls, err := os.ReadFile(fixture.calls)
			if err != nil || len(calls) != 0 {
				t.Fatalf("empty selection started probe: %q, %v", calls, err)
			}
		})
	}
}

func TestMacCleanupDirectSymlinkRoot(t *testing.T) {
	for _, gateway := range []bool{false, true} {
		t.Run(fmt.Sprintf("gateway-%t", gateway), func(t *testing.T) {
			fixture := newMacCleanupFixture(t)
			preserved := fileFixture(t, fixture.prunes, "old.tmp", "preserved")
			if err := os.Remove(fixture.writes); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(fixture.prunes, fixture.writes); err != nil {
				t.Fatal(err)
			}
			got := runProgram(t, sourceFile(t, fixture.directSource(t, gateway, true, "success", "", "")), "run", fixture.directFlags(t, false)...)
			if got.status != 2 || !strings.Contains(got.stderr, "runtime.host-failure") {
				t.Fatalf("symlink root rejection: %+v", got)
			}
			macCleanupPresent(t, preserved, true)
			calls, err := os.ReadFile(fixture.calls)
			if err != nil || len(calls) != 0 {
				t.Fatalf("symlink root started probe: %q, %v", calls, err)
			}
		})
	}
}
