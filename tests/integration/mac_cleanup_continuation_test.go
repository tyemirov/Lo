package integration_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestMacCleanupDirectContinuation(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprintf("terminal-%t", terminal), func(t *testing.T) {
			fixture := newMacCleanupFixture(t)
			first := fileFixture(t, fixture.writes, "old.tmp", "preserved")
			second := fileFixture(t, fixture.prunes, "later.tmp", "later category")
			program := cleanupFlows(t, "StaleFiles", "RecordOutcome", "CapturedPerformError", "JoinStrings") + fmt.Sprintf(`
flow TryDirect(context: Record{apply: Bool, home: File.Path, uid: Int, process: Process.Context}, path: File.Path) -> Record{result: Int!, errors: List<Error>} onError CapturedPerformError {
    result = StaleFiles(context, path, List.new("*.tmp"), 0)
    return Record.new(result: result, errors: List.new())
}
flow InitialState() -> Record{failures: List<String>, estimates: List<Int>} { return Record.new(failures: List.new(), estimates: List.new()) }
flow DirectSequence() -> Program.Exit! {
    lock = Lock.File.new(Program.path())
    lease = commit lock
    environment = Map.new(Record.new(key: "PATH", value: %q), Record.new(key: "FIXTURE_CALLS", value: %q), Record.new(key: "FIXTURE_BEHAVIOR", value: "first-fails"))
    process = Process.Context.new(environment, File.Path.new(%q))
    context = Record.new(apply: true, home: File.Path.new(%q), uid: %d, process: process)
    initial = InitialState()
    first = TryDirect(context, File.Path.new(%q))
    recorded = RecordOutcome(initial, "first category", first)
    second = TryDirect(context, File.Path.new(%q))
    final = RecordOutcome(recorded, "later category", second)
    return Program.Exit.new(Match.new(List.length(final.failures), 0: 0, _: 1), UTF8.Bytes.new(String.concat("FAILURES ", JoinStrings(final.failures, ","), "\n")), Bytes.new(""))
}
return DirectSequence()`, fixture.root, fixture.calls, fixture.root, fixture.root, os.Getuid(), fixture.writes, fixture.prunes)
			flags := fixture.directFlags(t, true)
			flags = append(flags, "--list-root", fixture.prunes, "--remove-root", fixture.prunes)
			if terminal {
				// A missing exact observation grant is a terminal authority failure.
				flags = flags[3:]
				flags = append(flags, "--lock-entry")
			}
			got := runProgram(t, sourceFile(t, program), "run", flags...)
			wantStatus := 1
			if terminal {
				wantStatus = 2
			}
			if got.status != wantStatus || strings.Contains(got.stderr, `"diagnostics"`) {
				t.Fatalf("category status: %+v", got)
			}
			macCleanupPresent(t, first, true)
			macCleanupPresent(t, second, terminal)
			calls, err := os.ReadFile(fixture.calls)
			if err != nil {
				t.Fatal(err)
			}
			if terminal {
				if len(calls) != 0 || strings.Contains(got.stdout, "FAILURES") || !strings.Contains(got.stderr, "runtime.capability-denied") {
					t.Fatalf("terminal failure continued: calls=%q, result=%+v", calls, got)
				}
			} else {
				want := "-F0n\n--\n" + first + "\n-F0n\n--\n" + second + "\n"
				if string(calls) != want || !strings.Contains(got.stderr, "FAILED first category:") || !strings.Contains(got.stdout, "FAILURES first category\n") || !strings.Contains(got.stdout, "Deleted 1 files") {
					t.Fatalf("ordinary failure did not retain label and continue: calls=%q, result=%+v", calls, got)
				}
			}
		})
	}
}
