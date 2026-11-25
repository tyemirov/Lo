//go:build darwin && arm64

package main_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSessionStartsIndependentLinuxAcceptanceWithoutArchiveDependency(t *testing.T) {
	command := exec.Command(runner(t), "--describe", "--group", "^(acceptance/|python/tools/lodistribution)")
	command.Dir = "../.."
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("describe independent package consumers: %v\n%s\n%s", err, &stdout, &stderr)
	}
	var descriptor struct {
		Groups []struct {
			ID    string
			After []string
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &descriptor); err != nil {
		t.Fatal(err)
	}
	foundLinux, foundPackageTests := false, false
	for _, entry := range descriptor.Groups {
		switch entry.ID {
		case "acceptance/linux-semantic":
			foundLinux = true
			if len(entry.After) != 0 {
				t.Fatalf("independent expanded Linux package waits for archive consumers: %v", entry.After)
			}
		case "python/tools/lodistribution":
			foundPackageTests = true
			if len(entry.After) != 1 || entry.After[0] != "acceptance/distributions" {
				t.Fatalf("package producer tests lost their storage dependency: %v", entry.After)
			}
		}
	}
	if !foundLinux || !foundPackageTests {
		t.Fatal("manifest omitted a selected package consumer")
	}
}

func TestSessionRequiresMetalCoverageWithAmbientCGODisabled(t *testing.T) {
	command := exec.Command(runner(t), "--describe", "--group", "^go/tests/integration/metal/")
	command.Dir = "../.."
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("describe required Metal coverage: %v\n%s\n%s", err, &stdout, &stderr)
	}
	var descriptor struct {
		Prepare []struct {
			ID          string
			Environment map[string]string
		}
		Groups []struct {
			Expected []string
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &descriptor); err != nil {
		t.Fatal(err)
	}
	prepared := false
	for _, entry := range descriptor.Prepare {
		if entry.ID == "prepare/tests/integration/metal" && entry.Environment["CGO_ENABLED"] == "1" {
			prepared = true
		}
	}
	if !prepared {
		t.Fatal("Metal test compilation does not declare required CGO configuration")
	}
	data, err := os.ReadFile("../../testdata/testsession/inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	var original struct {
		Go []struct{ Name, Constraint string }
	}
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}
	actual := map[string]bool{}
	for _, group := range descriptor.Groups {
		for _, name := range group.Expected {
			actual[name] = true
		}
	}
	for _, entry := range original.Go {
		if entry.Constraint == "metal && darwin && arm64 && cgo" && !actual[entry.Name] {
			t.Fatalf("original Metal case omitted: %s", entry.Name)
		}
	}
}

func TestSessionRetainsLinuxIdentityInputsThroughCoverageReport(t *testing.T) {
	for _, coverage := range []bool{false, true} {
		t.Run(map[bool]string{false: "execution-only", true: "coverage-report"}[coverage], func(t *testing.T) {
			selection := "^acceptance/linux-semantic$"
			consumer := "acceptance/linux-semantic"
			if coverage {
				selection = "^acceptance/(linux-semantic|semantic-report)$"
				consumer = "acceptance/semantic-report"
			}
			command := exec.Command(runner(t), "--describe", "--group", selection)
			command.Dir = "../.."
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			if err := command.Run(); err != nil {
				t.Fatalf("describe identity consumers: %v\n%s\n%s", err, &stdout, &stderr)
			}
			type entry struct {
				ID      string
				Release []string
			}
			var descriptor struct {
				Groups, Finalize []entry
			}
			if err := json.Unmarshal(stdout.Bytes(), &descriptor); err != nil {
				t.Fatal(err)
			}
			released := false
			for _, job := range append(descriptor.Groups, descriptor.Finalize...) {
				for _, path := range job.Release {
					if filepath.Base(path) != "lo-linux-arm64-software" {
						continue
					}
					if job.ID != consumer || released {
						t.Fatalf("Linux identity inputs released by %s before final consumer %s", job.ID, consumer)
					}
					released = true
				}
			}
			if !released {
				t.Fatal("last consumer does not release Linux identity inputs")
			}
		})
	}
}

func TestSessionReleasesPackageArchivesAfterTheirLastDeclaredConsumer(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "linux-only", true: "installed-acceptance"}[installed], func(t *testing.T) {
			selection, consumer := "^acceptance/(linux-semantic|semantic-report)$", "prepare/compiler-packages"
			if installed {
				selection, consumer = "^acceptance/(distributions|linux-semantic|semantic-report)$", "acceptance/distributions"
			}
			command := exec.Command(runner(t), "--describe", "--group", selection)
			command.Dir = "../.."
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			if err := command.Run(); err != nil {
				t.Fatalf("describe archive consumers: %v\n%s\n%s", err, &stdout, &stderr)
			}
			type entry struct {
				ID      string
				Release []string
			}
			var descriptor struct{ Prepare, Groups, Finalize []entry }
			if err := json.Unmarshal(stdout.Bytes(), &descriptor); err != nil {
				t.Fatal(err)
			}
			expected := map[string]bool{}
			for _, name := range []string{"lo-darwin-arm64-software", "lo-linux-arm64-software", "lo-darwin-arm64-metal"} {
				expected[name+".tar"], expected[name+".receipt.json"] = false, false
			}
			for _, job := range append(append(descriptor.Prepare, descriptor.Groups...), descriptor.Finalize...) {
				for _, path := range job.Release {
					name := filepath.Base(path)
					if seen, exists := expected[name]; exists {
						if seen || job.ID != consumer {
							t.Fatalf("archive %s released by %s; expected only %s", name, job.ID, consumer)
						}
						expected[name] = true
					}
					if name == "lo-linux-arm64-software" && job.ID != "acceptance/semantic-report" {
						t.Fatalf("Linux identity closure released before coverage: %s", job.ID)
					}
				}
			}
			for name, found := range expected {
				if !found {
					t.Fatalf("last consumer does not release %s", name)
				}
			}
		})
	}
}
