package main_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestSessionClassifiesAbortedCasesAfterFailedCommand(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name     string
		expected []string
		events   []map[string]string
		outcomes map[string]string
	}{
		{
			name:     "first-failure",
			expected: []string{"first", "second", "third"},
			events: []map[string]string{
				{"Action": "collect", "Test": "first"},
				{"Action": "collect", "Test": "second"},
				{"Action": "collect", "Test": "third"},
				{"Action": "run", "Test": "first"},
				{"Action": "fail", "Test": "first"},
			},
			outcomes: map[string]string{"first": "fail", "second": "aborted", "third": "aborted"},
		},
		{
			name:     "declared-collected-and-started",
			expected: []string{"declared-only", "shared"},
			events: []map[string]string{
				{"Action": "collect", "Test": "collected-only"},
				{"Action": "collect", "Test": "shared"},
				{"Action": "run", "Test": "started-only"},
				{"Action": "run", "Test": "shared"},
			},
			outcomes: map[string]string{"declared-only": "aborted", "collected-only": "aborted", "started-only": "aborted", "shared": "aborted"},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			events, err := json.Marshal(scenario.events)
			if err != nil {
				t.Fatal(err)
			}
			const script = "import json,sys\nfor event in json.loads(sys.argv[1]): print('LO_TEST_EVENT:'+json.dumps(event),flush=True)\nsys.stderr.write('original-command-failure-sentinel\\n')\nsys.exit(1)\n"
			entry := map[string]any{"id": "broken", "format": "pytest", "expected": scenario.expected, "argv": []string{python, "-B", "-c", script, string(events)}}
			stdout, stderr, err := invoke(t, map[string]any{"groups": []any{entry}})
			if err == nil || !strings.Contains(stderr, "exit status 1") || !strings.Contains(stderr, "original-command-failure-sentinel") || strings.Contains(stdout, `"status":"pass"`) {
				t.Fatalf("original command failure lost: %v\n%s\n%s", err, stdout, stderr)
			}
			if strings.Contains(stderr, "missing") || strings.Contains(stderr, "incomplete case") {
				t.Fatalf("aborted tail obscured original command failure:\n%s", stderr)
			}
			if scenario.name == "first-failure" && !strings.Contains(stderr, "case broken/first: fail") {
				t.Fatalf("first test failure lost: %s", stderr)
			}
			observed := map[string]string{}
			for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
				var event struct {
					Type, ID, Action string
				}
				if err := json.Unmarshal([]byte(line), &event); err != nil {
					t.Fatalf("invalid public event %q: %v", line, err)
				}
				if event.Type != "case" {
					continue
				}
				name := strings.TrimPrefix(event.ID, "broken/")
				if _, duplicate := observed[name]; duplicate {
					t.Fatalf("case reported more than once: %s", name)
				}
				observed[name] = event.Action
			}
			if len(observed) != len(scenario.outcomes) {
				t.Fatalf("unfinished cases lost: got %v; expected %v", observed, scenario.outcomes)
			}
			for name, action := range scenario.outcomes {
				if observed[name] != action {
					t.Fatalf("case %s reported %q; expected %q", name, observed[name], action)
				}
			}
		})
	}
}
