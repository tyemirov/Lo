package integration_test

import (
	"encoding/json"
	"testing"
)

func nativeValue(t *testing.T, got result) runtimeValue {
	t.Helper()
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("compiled program failed: %+v", got)
	}
	var envelope struct {
		Command, Status string
		Result          runtimeValue
	}
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "run" || envelope.Status != "ok" {
		t.Fatalf("compiled result envelope: %s", got.stdout)
	}
	return successfulValue(t, envelope.Result)
}

func nativeValueCheck(typ, payload string) func(*testing.T, result) {
	return func(t *testing.T, got result) {
		t.Helper()
		value := nativeValue(t, got)
		if value.Type != typ {
			t.Fatalf("compiled result type %q, want %q", value.Type, typ)
		}
		valueJSON(t, value, payload)
	}
}

func nativeBudgetOrSuccess(t *testing.T, got result) {
	t.Helper()
	if got.status == 0 {
		if got.stderr != "" {
			t.Fatalf("successful result has diagnostics: %+v", got)
		}
		return
	}
	if got.status != 2 || got.stdout != "" {
		t.Fatalf("unexpected budget outcome: %+v", got)
	}
	report := diagnostics(t, got.stderr)
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "runtime.limit" {
		t.Fatalf("expected logical budget diagnostic: %+v", got)
	}
}

func nativeFailure(t *testing.T, got result, status int, code, kind string) {
	t.Helper()
	if got.status != status || got.stdout != "" {
		t.Fatalf("compiled failure status/output: %+v", got)
	}
	var report struct {
		Diagnostics []struct {
			Code  string
			Error runtimeValue
		}
	}
	if err := json.Unmarshal([]byte(got.stderr), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != code {
		t.Fatalf("compiled failure class, want %s: %+v", code, got)
	}
	if kind != "" && report.Diagnostics[0].Error.Kind != kind {
		t.Fatalf("compiled error kind, want %s: %+v", kind, got)
	}
}
