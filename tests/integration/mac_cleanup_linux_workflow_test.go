package integration_test

import (
	"strings"
	"testing"
)

func TestMacCleanupLinuxCompleteArguments(t *testing.T) {
	artifact := linuxLLVMArtifact(t, cleanupApplication(t, "Main()"))
	for _, test := range []struct {
		name      string
		arguments []string
		status    int
		text      string
	}{
		{"help", []string{"run", "--", "--help"}, 0, "mac-cleanup"},
		{"unknown", []string{"run", "--", "--unknown"}, 2, "declared option parsing failed"},
		{"literal", []string{"run", "--", "$(touch should-not-exist)"}, 2, "declared option parsing failed"},
		{"root-uid", []string{"run", "--lock-entry"}, 2, "without sudo"},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual := runLinuxLLVMExecutable(t, artifact, test.arguments...)
			if actual.status != test.status || !strings.Contains(actual.stdout+actual.stderr, test.text) || strings.Contains(actual.stdout, "Starting mac-cleanup") {
				t.Fatalf("portable application boundary: %+v", actual)
			}
		})
	}
}
