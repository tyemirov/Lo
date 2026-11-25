package main_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionSerializesHostHTTPWithoutBlockingOtherHostWork(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	short, err := os.MkdirTemp(filepath.Join(root, "_scratch"), "http-slot-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(short); err != nil {
			t.Error(err)
		}
	})
	marker := filepath.Join(t.TempDir(), "host-progress")
	first, second, host := job(t, "first-http", "fixed-endpoint"), job(t, "second-http", "fixed-endpoint"), job(t, "host", "write")
	for _, entry := range []map[string]any{first, second} {
		entry["resource"] = "host-http"
		environment := entry["environment"].(map[string]string)
		environment["LO_SESSION_ENDPOINT"] = filepath.Join(short, "socket")
		environment["LO_SESSION_PEER"] = marker
	}
	host["environment"].(map[string]string)["LO_SESSION_WRITE"] = marker
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{first, second, host}}, "--workers", "2")
	if err != nil || !strings.Contains(stdout, `"completed":3`) || !strings.Contains(stdout, `"status":"pass"`) {
		t.Fatalf("fixed HTTP endpoints overlapped or blocked independent host work: %v\n%s\n%s", err, stdout, stderr)
	}
}

func TestSessionDeclaresFixedHostHTTPExclusionAndKeepsLinuxGuestResource(t *testing.T) {
	command := exec.Command(runner(t), "--describe", "--group", "^go/tests/integration/host/TestLLVM.*HTTP")
	command.Dir = "../.."
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("describe HTTP resources: %v\n%s\n%s", err, &stdout, &stderr)
	}
	var descriptor struct {
		Groups []struct{ ID, Resource string }
	}
	if err := json.Unmarshal(stdout.Bytes(), &descriptor); err != nil {
		t.Fatal(err)
	}
	resources := map[string]string{}
	for _, group := range descriptor.Groups {
		resources[group.ID] = group.Resource
	}
	for _, name := range []string{"TestLLVMHTTPCheckedCleanup", "TestLLVMHTTPErrorInputs", "TestLLVMHTTPRequestLifetime", "TestLLVMDeviceHTTPRequestLifetime"} {
		if resources["go/tests/integration/host/"+name] != "host-http" {
			t.Fatalf("host HTTP group lacks exclusion: %s: %v", name, resources)
		}
	}
	if resources["go/tests/integration/host/TestLLVMLinuxHTTPRequestLifetime"] != "guest" {
		t.Fatal("Linux HTTP execution lost its guest resource")
	}
}
