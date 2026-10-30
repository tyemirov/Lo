//go:build lo_process_native_host_qualification && darwin

package processhost_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/processhost"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

var hostCases = []string{"normal", "nonzero", "signaled", "output-to-file", "output-limit", "wall-timeout", "denied-command", "denied-cwd", "protected-command-alias", "protected-helper-alias", "helper-alias-as-command", "helper-mismatch", "digest-mismatch"}

type hostLifetime struct {
	Parallelism      uint32  `json:"maximum_parallelism"`
	Seconds          float64 `json:"admission_through_release_seconds"`
	WorkBoundSeconds float64 `json:"conditional_cpu_time_upper_bound_seconds"`
	CleanupComplete  bool    `json:"cleanup_complete"`
}

type hostArtifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type hostManifest struct {
	Launcher  string                  `json:"launcher"`
	Target    string                  `json:"target"`
	Directory string                  `json:"directory"`
	RunToken  string                  `json:"run_token"`
	Result    string                  `json:"result"`
	Cases     []string                `json:"cases"`
	Artifacts map[string]hostArtifact `json:"artifacts"`
}

func hostInputs(t *testing.T) (hostManifest, string) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	inside := func(path string) string {
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || !filepath.IsLocal(rel) {
			t.Fatal("host qualification path outside Lo")
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != path {
			t.Fatal("host qualification artifact is not canonical")
		}
		return path
	}
	path, reviewed := os.Getenv("LO_PROCESS_HOST_MANIFEST"), os.Getenv("LO_PROCESS_HOST_REVIEWED_SHA256")
	if path == "" || len(reviewed) != 64 {
		t.Fatal("exact reviewed native host manifest required")
	}
	encoded, err := os.ReadFile(inside(path))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	if hex.EncodeToString(digest[:]) != reviewed {
		t.Fatal("native host manifest changed")
	}
	var manifest hostManifest
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(manifest.Cases) != fmt.Sprint(hostCases) {
		t.Fatal("native host finite case matrix changed")
	}
	sealed := map[string]bool{}
	for _, artifact := range manifest.Artifacts {
		path := inside(artifact.Path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != artifact.SHA256 {
			t.Fatal("native host artifact changed")
		}
		sealed[path] = true
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{manifest.Launcher, manifest.Target, binary} {
		if !sealed[inside(path)] {
			t.Fatal("native host binary is not sealed")
		}
	}
	inside(manifest.Directory)
	for _, path := range []string{manifest.RunToken, manifest.Result} {
		if filepath.Dir(path) != manifest.Directory {
			t.Fatal("native host output path differs")
		}
	}
	if _, err := os.Lstat(manifest.Result); !os.IsNotExist(err) {
		t.Fatal("native host result exists")
	}
	token, err := os.ReadFile(inside(manifest.RunToken))
	if err != nil || string(token) != reviewed+"\n" {
		t.Fatal("native host one-use permit absent")
	}
	if _, err := os.Lstat(manifest.RunToken + ".consumed"); !os.IsNotExist(err) {
		t.Fatal("native host permit already consumed")
	}
	if err := os.Rename(manifest.RunToken, manifest.RunToken+".consumed"); err != nil {
		t.Fatal(err)
	}
	return manifest, reviewed
}

func hostProgram(t *testing.T, root, code string) execution.Program {
	t.Helper()
	text, err := source.New(filepath.Join(root, "acceptance.lo"), code)
	if err != nil {
		t.Fatal(err)
	}
	checked, diagnostic := compiler.Check(text)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	program, err := eval.Program(checked)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func TestNativeProcessHostReviewedLane(t *testing.T) {
	manifest, reviewed := hostInputs(t)
	controlFD, err := strconv.Atoi(os.Getenv("LO_PROCESS_HOST_CONTROL_FD"))
	if err != nil || controlFD < 3 {
		t.Fatal("owned controller channel absent")
	}
	syscall.CloseOnExec(controlFD)
	if err := syscall.SetNonblock(controlFD, true); err != nil {
		t.Fatal(err)
	}
	control := os.NewFile(uintptr(controlFD), "host-controller")
	var admission [1]byte
	if _, err := io.ReadFull(control, admission[:]); err != nil || admission[0] != 'A' {
		t.Fatal("controller witness not armed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	joined := make(chan struct{})
	go func() { defer close(joined); var data [1]byte; _, _ = control.Read(data[:]); cancel() }()
	defer func() { _ = control.Close(); <-joined }()
	results := map[string]string{}
	lifetimes := map[string]hostLifetime{}
	cleanupComplete := true
	t.Cleanup(func() {
		unchanged := true
		root, _ := filepath.Abs("../..")
		for _, artifact := range manifest.Artifacts {
			path := artifact.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, path)
			}
			data, err := os.ReadFile(path)
			digest := sha256.Sum256(data)
			if err != nil || hex.EncodeToString(digest[:]) != artifact.SHA256 {
				unchanged = false
				t.Error("native host artifact changed")
			}
		}
		encoded, err := json.Marshal(map[string]any{"reviewed_manifest_sha256": reviewed, "cases": results, "lifetimes": lifetimes, "cleanup_complete": cleanupComplete, "reviewed_artifacts_unchanged": unchanged, "qualified_process_backend": false})
		if err != nil {
			t.Error(err)
			return
		}
		file, err := os.Create(manifest.Result)
		if err != nil {
			t.Error(err)
			return
		}
		_, writeErr := file.Write(encoded)
		if err := errors.Join(writeErr, file.Close()); err != nil {
			t.Error(err)
		}
	})
	for _, name := range hostCases {
		if ctx.Err() != nil {
			t.Fatal("controller cancelled; no further admission")
		}
		if !t.Run(name, func(t *testing.T) {
			results[name] = "attempted"
			t.Cleanup(func() {
				if t.Failed() {
					results[name] = "failed"
				}
			})
			commandPath, launcherPath := manifest.Target, manifest.Launcher
			if name == "helper-alias-as-command" {
				commandPath = filepath.Join(manifest.Directory, "protected-helper-alias")
			}
			if name == "helper-mismatch" {
				launcherPath = manifest.Target
			}
			data, err := os.ReadFile(commandPath)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(data)
			if name == "digest-mismatch" {
				digest[0] ^= 1
			}
			if name == "helper-mismatch" {
				commandPath = filepath.Join(manifest.Directory, "protected-command-alias")
			}
			policy, err := adapter.NewProcessPolicy(adapter.ProcessPolicyInput{Grants: []string{fmt.Sprintf("%x,%s", digest, commandPath)}, Launcher: launcherPath, ReadRoots: []string{manifest.Directory}})
			if err != nil {
				t.Fatal(err)
			}
			limits := adapter.DefaultProcessLimits()
			if name == "output-limit" {
				limits, err = adapter.NewProcessLimits(128, 5*time.Second, time.Second)
				if err != nil {
					t.Fatal(err)
				}
			}
			configuration, err := processhost.NewConfig(policy, limits)
			if err != nil {
				t.Fatal(err)
			}
			creator, err := processhost.NewDarwinCreator(ctx, configuration)
			if name == "helper-alias-as-command" || name == "helper-mismatch" {
				var host execution.HostError
				if !errors.As(err, &host) || host.Code() != diagnostic.RuntimeHost {
					t.Fatalf("helper identity failure class: %v", err)
				}
				results[name] = "passed"
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			parallelism := creator.(interface{ MaximumParallelism() uint32 }).MaximumParallelism()
			if parallelism == 0 {
				t.Fatal("OS processor bound absent")
			}
			session, err := processhost.OpenTrusted(ctx, configuration, adapter.FileRoots{Write: []string{manifest.Directory}}, creator)
			if name == "digest-mismatch" {
				if err == nil {
					_ = session.Close(context.Background())
					t.Fatal("wrong digest admitted")
				}
				var host execution.HostError
				if !errors.As(err, &host) || host.Code() != diagnostic.RuntimeHost {
					t.Fatalf("command identity failure class: %v", err)
				}
				results[name] = "passed"
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			admitted := time.Now()
			defer func() {
				deadline, stop := context.WithTimeout(context.Background(), time.Second)
				defer stop()
				closeErr := session.Close(deadline)
				elapsed := time.Since(admitted)
				lifetime := hostLifetime{Parallelism: parallelism, Seconds: elapsed.Seconds(), CleanupComplete: closeErr == nil}
				if closeErr == nil {
					lifetime.WorkBoundSeconds = float64(parallelism) * elapsed.Seconds()
				}
				lifetimes[name] = lifetime
				if closeErr != nil {
					cleanupComplete = false
					t.Error(closeErr)
					_ = session.WaitRetained()
				}
				if elapsed > limits.Timeout()+limits.CleanupTimeout() {
					t.Error("complete admitted lifetime exceeded selected limits")
				}
			}()
			code := hostSource(name, manifest)
			output, runErr := hostProgram(t, manifest.Directory, code).Run(ctx, session.Boundary(), execution.DefaultBudget(), device.DefaultConfig())
			switch name {
			case "denied-command", "denied-cwd", "protected-command-alias", "protected-helper-alias":
				var host execution.HostError
				want := diagnostic.RuntimeHost
				if name == "denied-cwd" {
					want = diagnostic.CapabilityDenied
				}
				if !errors.As(runErr, &host) || host.Code() != want {
					t.Fatalf("authority failure class: %v", runErr)
				}
				if name == "denied-command" || name == "denied-cwd" {
					progress := execution.ProcessProgress(runErr)
					if progress == nil || progress.Field("start_confirmed").(value.Boolean).Bool() || !progress.Field("cleanup_complete").(value.Boolean).Bool() {
						t.Fatal("denied authority lost precreation cleanup evidence")
					}
				}
			case "output-limit", "wall-timeout":
				var host execution.HostError
				want := diagnostic.RuntimeLimit
				if name == "wall-timeout" {
					want = diagnostic.RuntimeCancelled
				}
				if !errors.As(runErr, &host) || host.Code() != want {
					t.Fatalf("execution failure classification: %v", runErr)
				}
				progress := execution.ProcessProgress(runErr)
				if progress == nil || !progress.Field("cleanup_complete").(value.Boolean).Bool() {
					t.Fatal("resource failure lost complete cleanup")
				}
			case "output-to-file":
				if runErr != nil {
					t.Fatal(runErr)
				}
				captured, err := os.ReadFile(filepath.Join(manifest.Directory, "captured.bin"))
				if err != nil || string(captured) != "stdout" {
					t.Fatal("Process output did not reach File")
				}
			default:
				if runErr != nil {
					t.Fatal(runErr)
				}
				result, failure := output.(value.Fallible).Outcome()
				if failure != nil {
					t.Fatal(failure)
				}
				process := result.(value.ProcessResult)
				if name == "signaled" {
					if process.Termination().Kind().Text() != "signaled" {
						t.Fatal("signal was not returned as data")
					}
					signal, err := process.Termination().Signal()
					if err != nil || signal.Big().Int64() != int64(syscall.SIGKILL) {
						t.Fatal("termination signal differs")
					}
				} else {
					code, err := process.Termination().ExitCode()
					want := int64(0)
					if name == "nonzero" {
						want = 7
					}
					if err != nil || code.Big().Int64() != want {
						t.Fatal("termination differs")
					}
					if name == "normal" && (string(process.Stdout().Bytes()) != "stdout" || string(process.Stderr().Bytes()) != "stderr") {
						t.Fatal("native channel bytes differ")
					}
				}
			}
			results[name] = "passed"
		}) {
			return
		}
	}
}

func hostSource(name string, manifest hostManifest) string {
	selected := manifest.Target
	directory := manifest.Directory
	if name == "denied-cwd" {
		directory = filepath.Dir(manifest.Launcher)
	}
	if name == "denied-command" {
		selected = filepath.Join(manifest.Directory, "protected-command-alias")
	}
	input := ""
	if name == "normal" || name == "output-to-file" {
		input = "stdin"
	}
	command := fmt.Sprintf(`executable = Process.Executable.new(File.Path.new(%q)); command = Process.Command.new(executable,List.new(%q),Map.new(Record.new(key:"NAME",value:"explicit")),File.Path.new(%q),UTF8.Bytes.new(%q));`, selected, name, directory, input)
	code := `flow Main() -> Process.Result! { ` + command + ` result = commit command; return result }; return Main()`
	if name == "output-to-file" {
		code = fmt.Sprintf(`flow Main() -> File.WriteReceipt! { %s result = commit command; target = File.Write.new(File.Path.new(%q),result.stdout); receipt = commit target; return receipt }; return Main()`, command, filepath.Join(manifest.Directory, "captured.bin"))
	}
	if name == "protected-command-alias" || name == "protected-helper-alias" {
		code = fmt.Sprintf(`flow Main() -> File.WriteReceipt! { target = File.Write.new(File.Path.new(%q),UTF8.Bytes.new("changed")); result = commit target; return result }; return Main()`, filepath.Join(manifest.Directory, name))
	}
	return code
}

func TestNativeProcessHostFixturePrograms(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "_scratch/adapter-implementation-x921/host-final")
	manifest := hostManifest{Target: filepath.Join(directory, "target"), Launcher: filepath.Join(root, "bin/lo-process-launcher"), Directory: directory}
	for _, name := range hostCases {
		t.Run(name, func(t *testing.T) { _ = hostProgram(t, directory, hostSource(name, manifest)) })
	}
}
