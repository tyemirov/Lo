package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(tests *testing.M) {
	if mode := os.Getenv("LO_SESSION_HELPER"); mode != "" {
		if filename := os.Getenv("LO_SESSION_WRITE"); filename != "" {
			file, err := os.Create(filename)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			content := strings.Repeat("x", 4096)
			if mode == "worker-path" {
				content = os.Getenv("TMPDIR")
			}
			_, err = file.WriteString(content)
			if closeErr := file.Close(); err != nil || closeErr != nil {
				os.Exit(2)
			}
		}
		if mode == "wait" {
			time.Sleep(20 * time.Second)
			os.Exit(0)
		}
		if mode == "oversized" {
			_, _ = io.WriteString(os.Stdout, strings.Repeat("x", 17<<20))
			time.Sleep(20 * time.Second)
			os.Exit(0)
		}
		if mode == "assert-absent" {
			if _, err := os.Stat(os.Getenv("LO_SESSION_CHECK")); !os.IsNotExist(err) {
				fmt.Fprintln(os.Stderr, "completed producer retained disposable output")
				os.Exit(2)
			}
		}
		if mode == "report-directory" {
			if err := os.Mkdir(os.Getenv("LO_SESSION_REPORT"), os.ModePerm); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		}
		if mode == "requires-file" {
			if _, err := os.Stat(os.Getenv("LO_SESSION_CHECK")); err != nil {
				fmt.Fprintln(os.Stderr, "required dependency did not complete")
				os.Exit(2)
			}
		}
		if mode == "fail" {
			name := os.Getenv("LO_SESSION_CASE")
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"Action": "run", "Test": name})
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"Action": "fail", "Test": name})
			os.Exit(2)
		}
		if mode == "unix-fixture" {
			directory, err := os.MkdirTemp(os.Getenv("TMPDIR"), "po-")
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			listener, err := net.Listen("unix", filepath.Join(directory, "events.owner"))
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			if err := listener.Close(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		}
		if mode == "fixed-endpoint" {
			listener, err := net.Listen("unix", os.Getenv("LO_SESSION_ENDPOINT"))
			if err != nil {
				fmt.Fprintln(os.Stderr, "fixed endpoint unavailable:", err)
				os.Exit(2)
			}
			until := time.Now().Add(time.Second)
			for {
				if _, err := os.Stat(os.Getenv("LO_SESSION_PEER")); err == nil {
					break
				}
				if time.Now().After(until) {
					fmt.Fprintln(os.Stderr, "blocked HTTP work occupied the host worker")
					os.Exit(2)
				}
				time.Sleep(5 * time.Millisecond)
			}
			time.Sleep(100 * time.Millisecond)
			if err := listener.Close(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		}
		if mode == "barrier" {
			until := time.Now().Add(3 * time.Second)
			for {
				if _, err := os.Stat(os.Getenv("LO_SESSION_PEER")); err == nil {
					break
				}
				if time.Now().After(until) {
					fmt.Fprintln(os.Stderr, "peer did not start")
					os.Exit(2)
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
		name := os.Getenv("LO_SESSION_CASE")
		if mode == "skip" {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"Action": "run", "Test": name})
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"Action": "output", "Test": name, "Output": "    fixture_test.go:1: " + os.Getenv("LO_SESSION_REASON") + "\n"})
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"Action": "skip", "Test": name})
			os.Exit(0)
		}
		if mode != "missing" {
			fmt.Printf("{\"Action\":\"run\",\"Test\":%q}\n", name)
			fmt.Printf("{\"Action\":\"pass\",\"Test\":%q}\n", name)
			if mode == "duplicate" {
				fmt.Printf("{\"Action\":\"pass\",\"Test\":%q}\n", name)
			}
		}
		os.Exit(0)
	}
	status := tests.Run()
	if runnerBuild.binary != "" {
		if err := os.RemoveAll(filepath.Dir(runnerBuild.binary)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			status = 1
		}
	}
	os.Exit(status)
}

var runnerBuild struct {
	sync.Once
	binary string
	err    error
	output []byte
}

func runner(t *testing.T) string {
	t.Helper()
	runnerBuild.Do(func() {
		root, err := filepath.Abs("../..")
		if err != nil {
			runnerBuild.err = err
			return
		}
		directory, err := os.MkdirTemp(os.Getenv("TMPDIR"), "session-cli-")
		if err != nil {
			runnerBuild.err = err
			return
		}
		runnerBuild.binary = filepath.Join(directory, "runner")
		command := exec.Command("go", "build", "-o", runnerBuild.binary, "./tools/testsession")
		command.Dir = root
		runnerBuild.output, runnerBuild.err = command.CombinedOutput()
	})
	if runnerBuild.err != nil {
		t.Fatalf("build session CLI: %v\n%s", runnerBuild.err, runnerBuild.output)
	}
	return runnerBuild.binary
}

func job(t *testing.T, id, mode string) map[string]any {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	values := isolatedCacheEnvironment()
	values["LO_SESSION_HELPER"], values["LO_SESSION_CASE"] = mode, id
	return map[string]any{"id": id, "argv": []string{self}, "format": "events", "expected": []string{id}, "environment": values}
}

func isolatedCacheEnvironment() map[string]string {
	values := map[string]string{}
	for _, name := range []string{"GOCACHE", "GOMODCACHE", "GOPATH", "CARGO_TARGET_DIR", "UV_CACHE_DIR", "LO_NATIVE_PREPARATION", "LO_DISTRIBUTION_BUILD_CACHE"} {
		values[name] = "{work}/private-cache/" + name
	}
	return values
}

func TestSessionUsesUnindexedTemporaryWorkspace(t *testing.T) {
	command := exec.Command(runner(t), "--describe", "--group", "^static/")
	command.Dir = "../.."
	data, err := command.Output()
	if err != nil {
		t.Fatalf("describe temporary workspace: %v", err)
	}
	var descriptor struct {
		Prepare []struct {
			ID   string
			Argv []string
		}
	}
	if err := json.Unmarshal(data, &descriptor); err != nil {
		t.Fatal(err)
	}
	for _, entry := range descriptor.Prepare {
		if entry.ID == "prepare/frontend" {
			work := filepath.Dir(filepath.Dir(entry.Argv[3]))
			if !strings.HasSuffix(work, ".noindex") {
				t.Fatalf("temporary compiler output can enter search indexing: %s", work)
			}
			if _, err := os.Stat(work); !os.IsNotExist(err) {
				t.Fatalf("described temporary workspace was retained: %v", err)
			}
			return
		}
	}
	t.Fatal("frontend workspace is absent")
}

func invoke(t *testing.T, spec map[string]any, options ...string) (string, string, error) {
	t.Helper()
	directory := t.TempDir()
	manifest := filepath.Join(directory, "manifest.json")
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--manifest", manifest, "--work", filepath.Join(directory, "run"), "--json", "--max-bytes", "134217728"}, options...)
	command := exec.Command(runner(t), args...)
	command.Dir = "../.."
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	return stdout.String(), stderr.String(), err
}

func TestSessionRunsFreshCasesConcurrently(t *testing.T) {
	directory := t.TempDir()
	first, second := job(t, "first", "barrier"), job(t, "second", "barrier")
	first["environment"].(map[string]string)["LO_SESSION_WRITE"] = filepath.Join(directory, "first")
	first["environment"].(map[string]string)["LO_SESSION_PEER"] = filepath.Join(directory, "second")
	second["environment"].(map[string]string)["LO_SESSION_WRITE"] = filepath.Join(directory, "second")
	second["environment"].(map[string]string)["LO_SESSION_PEER"] = filepath.Join(directory, "first")
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{first, second}}, "--workers", "2")
	if err != nil || !strings.Contains(stdout, `"completed":2`) || !strings.Contains(stdout, `"status":"pass"`) {
		t.Fatalf("session: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
}

func TestSessionRejectsIncompleteAndDuplicateResults(t *testing.T) {
	for _, mode := range []string{"missing", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			stdout, stderr, err := invoke(t, map[string]any{"groups": []any{job(t, "required", mode)}})
			if err == nil || !strings.Contains(stderr, mode) || strings.Contains(stdout, `"status":"pass"`) {
				t.Fatalf("invalid results accepted: %v\n%s\n%s", err, stdout, stderr)
			}
		})
	}
}

func TestSessionCancelsOwnedWork(t *testing.T) {
	start := time.Now()
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{job(t, "blocked", "wait")}}, "--budget", "200ms")
	if err == nil || time.Since(start) > 5*time.Second || !strings.Contains(stderr, "deadline") || strings.Contains(stdout, `"status":"pass"`) {
		t.Fatalf("deadline not enforced: %v\n%s\n%s", err, stdout, stderr)
	}
}

func TestSessionRejectsInvalidManifestBeforeWork(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "unexpected")
	first := job(t, "one", "write")
	first["environment"].(map[string]string)["LO_SESSION_WRITE"] = marker
	_, stderr, err := invoke(t, map[string]any{"groups": []any{first}, "undeclared": true})
	if err == nil || !strings.Contains(stderr, "unknown field") {
		t.Fatalf("unknown input accepted: %v %s", err, stderr)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("work started for invalid manifest: %v", err)
	}
}

func TestSessionReportsStorageLimit(t *testing.T) {
	entry := job(t, "large", "write")
	entry["environment"].(map[string]string)["LO_SESSION_WRITE"] = "{work}/large"
	_, stderr, err := invoke(t, map[string]any{"groups": []any{entry}}, "--max-bytes", "512")
	if err == nil || !strings.Contains(stderr, "storage") {
		t.Fatalf("session storage not bounded: %v %s", err, stderr)
	}
}

func TestSessionCleansPythonFixturesBetweenCases(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "previous")
	source := filepath.Join(directory, "test_storage.py")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(file, "from pathlib import Path\nMARKER=Path(%q)\ndef test_first(tmp_path,monkeypatch):\n    MARKER.write_text(str(tmp_path))\n    monkeypatch.setenv('LO_TEST_SESSION_DIRECTORY',str(tmp_path/'application-state'))\n    (tmp_path/'data').write_bytes(b'x'*4096)\ndef test_second():\n    assert not Path(MARKER.read_text()).exists(), 'previous case storage leaked'\n", marker)
	if err = errors.Join(err, file.Close()); err != nil {
		t.Fatal(err)
	}
	entry := map[string]any{"id": "python-cleanup", "format": "pytest", "argv": []string{"uv", "run", "--offline", "--with", "pytest==8.3.5", "python", "-B", "-m", "pytest", "-p", "no:cacheprovider", "-p", "tools.testsession.pytest_plugin", "--basetemp", "{work}/pytest", source}}
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{entry}})
	if err != nil {
		t.Fatalf("Python case cleanup: %v\n%s\n%s", err, stdout, stderr)
	}
}

func TestSessionRejectsUndeclaredSkipReasons(t *testing.T) {
	for _, reason := range []string{"declared platform condition", "new omitted coverage"} {
		t.Run(reason, func(t *testing.T) {
			entry := job(t, "conditional", "skip")
			entry["environment"].(map[string]string)["LO_SESSION_REASON"] = reason
			entry["allow_skips"] = map[string][]string{"conditional": {"declared platform condition"}}
			stdout, stderr, err := invoke(t, map[string]any{"groups": []any{entry}})
			if reason == "declared platform condition" && err != nil {
				t.Fatalf("declared skip rejected: %v %s %s", err, stdout, stderr)
			}
			if reason != "declared platform condition" && (err == nil || !strings.Contains(stderr, "skip")) {
				t.Fatalf("new skip accepted: %v %s %s", err, stdout, stderr)
			}
		})
	}
}

func TestSessionKeepsHostWorkMovingWhileGuestsWait(t *testing.T) {
	directory := t.TempDir()
	first, second, host := job(t, "guest-one", "barrier"), job(t, "guest-two", "barrier"), job(t, "host", "barrier")
	for _, entry := range []map[string]any{first, second} {
		entry["resource"] = "guest"
		entry["environment"].(map[string]string)["LO_SESSION_WRITE"] = filepath.Join(directory, entry["id"].(string))
		entry["environment"].(map[string]string)["LO_SESSION_PEER"] = filepath.Join(directory, "host")
	}
	host["environment"].(map[string]string)["LO_SESSION_WRITE"] = filepath.Join(directory, "host")
	host["environment"].(map[string]string)["LO_SESSION_PEER"] = filepath.Join(directory, "guest-one")
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{first, second, host}}, "--workers", "2", "--guest-workers", "1")
	if err != nil || !strings.Contains(stdout, `"completed":3`) {
		t.Fatalf("guest wait starved host work: %v\n%s\n%s", err, stdout, stderr)
	}
}

func TestSessionRejectsInvalidReportBeforeExecutingJobs(t *testing.T) {
	for _, destination := range []string{"go.mod", t.TempDir()} {
		t.Run(filepath.Base(destination), func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "unexpected-job")
			entry := job(t, "should-not-start", "write")
			entry["environment"].(map[string]string)["LO_SESSION_WRITE"] = marker
			stdout, stderr, err := invoke(t, map[string]any{"groups": []any{entry}}, "--report", destination)
			if err == nil || strings.Contains(stdout, `"status":"pass"`) {
				t.Fatalf("invalid report path passed: %v %s %s", err, stdout, stderr)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("job executed before report boundary validation: %v", err)
			}
		})
	}
}

func TestSessionDescriptionIsOneReadOnlyDocument(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "unexpected-job")
	entry := job(t, "planned", "write")
	entry["environment"].(map[string]string)["LO_SESSION_WRITE"] = marker
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{entry}}, "--describe")
	if err != nil {
		t.Fatalf("describe manifest: %v %s %s", err, stdout, stderr)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &document); err != nil || document["groups"] == nil {
		t.Fatalf("description is not one manifest document: %v %s", err, stdout)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("description executed a job: %v", err)
	}
}

func TestSessionDoesNotCollideWorkerDirectories(t *testing.T) {
	directory := t.TempDir()
	first, second := job(t, "same/name", "worker-path"), job(t, "same_name", "worker-path")
	paths := []string{filepath.Join(directory, "first"), filepath.Join(directory, "second")}
	first["environment"].(map[string]string)["LO_SESSION_WRITE"] = paths[0]
	second["environment"].(map[string]string)["LO_SESSION_WRITE"] = paths[1]
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{first, second}})
	if err != nil {
		t.Fatalf("worker isolation: %v %s %s", err, stdout, stderr)
	}
	left, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	right, err := os.ReadFile(paths[1])
	if err != nil {
		t.Fatal(err)
	}
	if string(left) == string(right) {
		t.Fatalf("distinct groups shared mutable worker state: %s", left)
	}
}

func TestSessionStopsOwnedJobOnOversizedOutput(t *testing.T) {
	_ = runner(t)
	started := time.Now()
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{job(t, "oversized", "oversized")}}, "--budget", "5s")
	if err == nil || time.Since(started) > 3*time.Second || !strings.Contains(stderr, "read group oversized events") || strings.Contains(stdout, `"status":"pass"`) {
		t.Fatalf("oversized output did not stop promptly: %v %s %s", err, stdout, stderr)
	}
}

func TestSessionReportsPreparationProgress(t *testing.T) {
	stdout, stderr, err := invoke(t, map[string]any{"prepare": []any{job(t, "prepare/blocked", "wait")}, "groups": []any{job(t, "unused", "pass")}}, "--budget", "5200ms")
	if err == nil || !strings.Contains(stderr, "active=1 prepare/blocked") || strings.Contains(stdout, `"status":"pass"`) {
		t.Fatalf("preparation progress was silent: %v %s %s", err, stdout, stderr)
	}
}

func TestSessionRejectsEmptyEventExecution(t *testing.T) {
	entry := job(t, "empty", "missing")
	delete(entry, "expected")
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{entry}})
	if err == nil || !strings.Contains(stderr, "missing case results") || strings.Contains(stdout, `"status":"pass"`) {
		t.Fatalf("empty test stream passed: %v %s %s", err, stdout, stderr)
	}
}

func TestSessionRequiresEveryPythonDefinitionAndParameter(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "test_parameters.py")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := io.WriteString(file, "import pytest\n@pytest.mark.parametrize('value',[1,2],ids=['one','two'])\ndef test_values(value):\n    assert value > 0\ndef test_other():\n    assert True\n")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(root, source)
	if err != nil {
		t.Fatal(err)
	}
	definition := filepath.ToSlash(relative)
	for _, selection := range []string{"", "not two", "not other"} {
		t.Run(selection, func(t *testing.T) {
			arguments := []string{"uv", "run", "--offline", "--with", "pytest==8.3.5", "python", "-B", "-m", "pytest", "-p", "no:cacheprovider", "-p", "tools.testsession.pytest_plugin", "--basetemp", "{work}/pytest", source}
			if selection != "" {
				arguments = append(arguments, "-k", selection)
			}
			entry := map[string]any{"id": "parameters", "format": "pytest", "argv": arguments, "expected": []string{definition + "::test_values", definition + "::test_other"}}
			stdout, stderr, err := invoke(t, map[string]any{"groups": []any{entry}})
			if selection == "" && err != nil {
				t.Fatalf("complete Python definitions rejected: %v %s %s", err, stdout, stderr)
			}
			if selection != "" && (err == nil || strings.Contains(stdout, `"status":"pass"`)) {
				t.Fatalf("omitted Python cases passed: %v %s %s", err, stdout, stderr)
			}
		})
	}
}

func TestSessionReleasesOwnedOutputsAfterSuccessfulProducer(t *testing.T) {
	producer := job(t, "prepare/producer", "write")
	producer["environment"].(map[string]string)["LO_SESSION_WRITE"] = "{work}/large"
	producer["release"] = []string{"{work}/large"}
	consumer := job(t, "consumer", "assert-absent")
	consumer["environment"].(map[string]string)["LO_SESSION_CHECK"] = "{work}/large"
	stdout, stderr, err := invoke(t, map[string]any{"prepare": []any{producer}, "groups": []any{consumer}}, "--max-bytes", "512")
	if err != nil || !strings.Contains(stdout, `"status":"pass"`) {
		t.Fatalf("owned producer output was not released: %v %s %s", err, stdout, stderr)
	}
}

func TestSessionRejectsUnownedReleaseBeforeExecutingJobs(t *testing.T) {
	for _, target := range []string{"go.mod", "{work}", t.TempDir()} {
		t.Run(filepath.Base(target), func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "unexpected-job")
			entry := job(t, "invalid-release", "write")
			entry["environment"].(map[string]string)["LO_SESSION_WRITE"] = marker
			entry["release"] = []string{target}
			stdout, stderr, err := invoke(t, map[string]any{"groups": []any{entry}})
			if err == nil || !strings.Contains(stderr, "release") || strings.Contains(stdout, `"status":"pass"`) {
				t.Fatalf("unowned release was accepted: %v %s %s", err, stdout, stderr)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("job started before release ownership validation: %v", err)
			}
		})
	}
}

func TestSessionReportPublicationFailureCannotAnnounceSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	entry := job(t, "report-write-failure", "report-directory")
	entry["environment"].(map[string]string)["LO_SESSION_REPORT"] = path
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{entry}}, "--report", path)
	if err == nil || !strings.Contains(stderr, "measurement report") || !strings.Contains(stdout, `"status":"fail"`) || strings.Contains(stdout, `"status":"pass"`) {
		t.Fatalf("report publication failure announced success: %v %s %s", err, stdout, stderr)
	}
}

func TestSessionPythonCleanupRejectsChangedParent(t *testing.T) {
	directory := t.TempDir()
	external := filepath.Join(directory, "other-owner")
	victim := filepath.Join(external, "test_case0")
	if err := os.MkdirAll(victim, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(victim, "preserve")
	for path, content := range map[string]string{
		marker:                                   "preserved",
		filepath.Join(directory, "test_case.py"): "def test_case(tmp_path):\n    assert tmp_path.is_dir()\n",
		filepath.Join(directory, "swap_parent.py"): fmt.Sprintf("import os\nfrom pathlib import Path\ndef pytest_runtest_logreport(report):\n    if report.when == 'teardown':\n        parent = Path(os.environ['LO_TEST_SESSION_DIRECTORY']) / 'pytest'\n        parent.rename(parent.with_name('pytest-preserved'))\n        parent.symlink_to(Path(%q), target_is_directory=True)\n", external),
	} {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := io.WriteString(file, content)
		if err := errors.Join(writeErr, file.Close()); err != nil {
			t.Fatal(err)
		}
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	entry := map[string]any{
		"id": "python-parent-swap", "format": "pytest",
		"argv":        []string{"uv", "run", "--offline", "--with", "pytest==8.3.5", "python", "-B", "-m", "pytest", "-p", "no:cacheprovider", "-p", "tools.testsession.pytest_plugin", "-p", "swap_parent", "--basetemp", "{work}/pytest", filepath.Join(directory, "test_case.py")},
		"environment": map[string]string{"PYTHONPATH": directory + string(os.PathListSeparator) + root},
	}
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{entry}})
	if err == nil || !strings.Contains(stderr, "temporary parent changed") || strings.Contains(stdout, `"status":"pass"`) {
		t.Fatalf("changed cleanup parent accepted: %v %s %s", err, stdout, stderr)
	}
	if contents, err := os.ReadFile(marker); err != nil || string(contents) != "preserved" {
		t.Fatalf("cleanup followed changed parent into unrelated fixture: %q %v", contents, err)
	}
}

func TestSessionSchedulesDependenciesBeforeTheirConsumers(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "dependency-output")
	producer := job(t, "producer", "write")
	producer["environment"].(map[string]string)["LO_SESSION_WRITE"] = marker
	consumer := job(t, "consumer", "requires-file")
	consumer["environment"].(map[string]string)["LO_SESSION_CHECK"] = marker
	consumer["after"] = []string{"producer"}
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{consumer, producer}}, "--workers", "1")
	if err != nil || !strings.Contains(stdout, `"completed":2`) || !strings.Contains(stdout, `"status":"pass"`) {
		t.Fatalf("dependency was not scheduled before consumer: %v %s %s", err, stdout, stderr)
	}
}

func TestSessionRejectsInvalidDependenciesBeforeExecution(t *testing.T) {
	for _, problem := range []string{"missing", "cycle", "duplicate", "prepare"} {
		t.Run(problem, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "unexpected-job")
			first, second := job(t, "first", "write"), job(t, "second", "write")
			first["environment"].(map[string]string)["LO_SESSION_WRITE"] = marker
			first["after"] = []string{"second"}
			spec := map[string]any{"groups": []any{first, second}}
			switch problem {
			case "missing":
				first["after"] = []string{"absent"}
			case "cycle":
				second["after"] = []string{"first"}
			case "duplicate":
				first["after"] = []string{"second", "second"}
			case "prepare":
				delete(first, "after")
				second["after"] = []string{"first"}
				spec["prepare"] = []any{second}
				spec["groups"] = []any{first}
			}
			stdout, stderr, err := invoke(t, spec)
			if err == nil || !strings.Contains(stderr, "dependenc") || strings.Contains(stdout, `"status":"pass"`) {
				t.Fatalf("invalid dependencies accepted: %v %s %s", err, stdout, stderr)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("job started before dependency validation: %v", err)
			}
		})
	}
}

func TestSessionDoesNotReleaseConsumersAfterDependencyFailure(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "unexpected-consumer")
	producer := job(t, "producer", "fail")
	consumer := job(t, "consumer", "write")
	consumer["environment"].(map[string]string)["LO_SESSION_WRITE"] = marker
	consumer["after"] = []string{"producer"}
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{consumer, producer}})
	if err == nil || !strings.Contains(stderr, "producer") || strings.Contains(stdout, `"status":"pass"`) {
		t.Fatalf("failed dependency session passed: %v %s %s", err, stdout, stderr)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("consumer ran after dependency failure: %v", err)
	}
}

func TestSessionCanDescribeFocusedStaticQualification(t *testing.T) {
	command := exec.Command(runner(t), "--describe", "--group", "^static/")
	command.Dir = "../.."
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("describe focused static qualification: %v\nstdout=%s\nstderr=%s", err, &stdout, &stderr)
	}
	type entry struct{ ID string }
	var descriptor struct {
		Prepare  []entry
		Groups   []entry
		Finalize []entry
	}
	if err := json.Unmarshal(stdout.Bytes(), &descriptor); err != nil {
		t.Fatalf("static qualification is not one manifest: %v %s", err, &stdout)
	}
	contains := func(entries []entry, expected string) bool {
		for _, item := range entries {
			if item.ID == expected {
				return true
			}
		}
		return false
	}
	if !contains(descriptor.Prepare, "format") || len(descriptor.Groups) != 1 || descriptor.Groups[0].ID != "static/go-vet" || !contains(descriptor.Finalize, "finalize/compiler") {
		t.Fatalf("focused static qualification omitted required phases: %s", &stdout)
	}
}

func TestSessionKeepsWorkerPathsIndependentOfLongIdentifiers(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(filepath.Join(root, "_scratch"), "tw-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	})
	name := "go/tests/integration/host/TestF036CleanupBoundaryOwnership/malformed-receipt-retains-freeze"
	stdout, stderr, err := invoke(t, map[string]any{"groups": []any{job(t, name, "unix-fixture")}}, "--work", filepath.Join(directory, "run"))
	if err != nil || !strings.Contains(stdout, `"status":"pass"`) || !strings.Contains(stdout, name) {
		t.Fatalf("human test identifiers prevented Unix fixture creation: %v %s %s", err, stdout, stderr)
	}
}

func TestSessionCanonicalPythonFixturesRunWithOwnedTemporaryState(t *testing.T) {
	directory := t.TempDir()
	work, report := filepath.Join(directory, "run"), filepath.Join(directory, "report.json")
	command := exec.Command(runner(t), "--group", "^python/experiments/native-runtime-abi$", "--work", work, "--report", report, "--max-bytes", "134217728", "--budget", "30s", "--json")
	command.Dir = "../.."
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("canonical Python fixture session: %v\nstdout=%s\nstderr=%s", err, &stdout, &stderr)
	}
	if strings.Contains(stdout.String(), `"id":"prepare/`) {
		t.Fatalf("standalone Python ABI fixture started unrelated preparation: %s", &stdout)
	}
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var outcome struct {
		Status    string `json:"status"`
		Source    string `json:"source_sha256"`
		Completed int    `json:"completed"`
	}
	if err := json.Unmarshal(data, &outcome); err != nil || outcome.Status != "pass" || len(outcome.Source) != 64 || outcome.Completed < 1 {
		t.Fatalf("canonical Python result is incomplete: %v %s", err, data)
	}
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Fatalf("canonical Python session state remains after completion: %v", err)
	}
}
