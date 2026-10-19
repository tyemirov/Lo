//go:build lo_process_public_qualification && darwin

package public_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var publicCases = []string{"normal", "nonzero", "signaled", "pipeline-zero", "pipeline-nonzero", "denied-command", "denied-cwd", "helper-identity", "parent-read", "parent-write", "wall-timeout", "server-callback"}

type publicProgram struct {
	Source   string `json:"source"`
	Artifact string `json:"artifact"`
}
type publicReview struct {
	Phase       string                              `json:"phase"`
	SourcesRoot string                              `json:"sources_root"`
	CLI         string                              `json:"cli"`
	Helper      string                              `json:"helper"`
	Target      string                              `json:"target"`
	BadHelper   string                              `json:"bad_helper"`
	ReadRoot    string                              `json:"read_root"`
	WriteRoot   string                              `json:"write_root"`
	Result      string                              `json:"result"`
	Token       string                              `json:"token"`
	Programs    map[string]map[string]publicProgram `json:"programs"`
	Artifacts   map[string]string                   `json:"artifacts"`
}
type publicValue struct {
	Type   string          `json:"type"`
	Status string          `json:"status"`
	Value  json.RawMessage `json:"value"`
}
type publicDiagnostic struct {
	Code     string       `json:"code"`
	Message  string       `json:"message"`
	Progress *publicValue `json:"process_progress"`
}
type publicEvidence struct {
	Backend          string `json:"backend"`
	Case             string `json:"case"`
	Passed           bool   `json:"passed"`
	Joined           bool   `json:"cli_joined"`
	ExitCode         int    `json:"exit_code"`
	Code             string `json:"diagnostic_code,omitempty"`
	ProgressPresent  bool   `json:"process_progress_present"`
	StartConfirmed   bool   `json:"start_confirmed"`
	CleanupConfirmed bool   `json:"cleanup_confirmed"`
	Elapsed          int64  `json:"elapsed_ns"`
}

func publicInside(t *testing.T, root, path string) string {
	t.Helper()
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	relative, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatal("public fixture outside Lo")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	relative, err = filepath.Rel(root, resolved)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatal("public fixture parent outside Lo")
	}
	if resolved, err = filepath.EvalSymlinks(path); err == nil && resolved != path {
		t.Fatal("public fixture is a symlink")
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return path
}

func publicInputs(t *testing.T) (publicReview, string, string) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	path, expected := os.Getenv("LO_PROCESS_PUBLIC_MANIFEST"), os.Getenv("LO_PROCESS_PUBLIC_REVIEWED_SHA256")
	if path == "" || len(expected) != 64 {
		t.Fatal("public Process acceptance requires exact reviewed manifest; no activation authorized")
	}
	encoded, err := os.ReadFile(publicInside(t, root, path))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	if hex.EncodeToString(digest[:]) != expected {
		t.Fatal("public manifest differs from review")
	}
	var review publicReview
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&review); err != nil {
		t.Fatal(err)
	}
	release, kernelErr := unix.Sysctl("kern.osrelease")
	if kernelErr != nil || strings.SplitN(release, ".", 2)[0] != "27" || runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" || (review.Phase != "gate-red" && review.Phase != "qualified") {
		t.Fatal("public Process lane requires selected Darwin27 arm64 phase")
	}
	sealed := map[string]bool{}
	for path, expected := range review.Artifacts {
		path = publicInside(t, root, path)
		encoded, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(encoded)
		if hex.EncodeToString(digest[:]) != expected {
			t.Fatal("public sealed artifact differs: ", path)
		}
		sealed[path] = true
	}
	self, err := os.Executable()
	if err != nil || !sealed[self] {
		t.Fatal("public test executable is not sealed")
	}
	for _, path := range []string{review.CLI, review.Helper, review.Target, review.BadHelper, filepath.Join(review.SourcesRoot, "experiments/process-public/public_test.go"), filepath.Join(review.SourcesRoot, "experiments/process-public/probe.py"), filepath.Join(review.SourcesRoot, "experiments/process-public/probe.mk"), filepath.Join(review.SourcesRoot, "experiments/process-public/native/target.c")} {
		if !sealed[publicInside(t, root, path)] {
			t.Fatal("public input is not sealed: ", path)
		}
	}
	for _, backend := range []string{"interpreter", "native"} {
		if len(review.Programs[backend]) != len(publicCases) {
			t.Fatal("public program matrix differs")
		}
		for _, name := range publicCases {
			program, ok := review.Programs[backend][name]
			if !ok || !sealed[program.Source] || !sealed[program.Artifact] {
				t.Fatal("public program missing from seal")
			}
		}
	}
	review.Result, review.Token = publicInside(t, root, review.Result), publicInside(t, root, review.Token)
	if _, err := os.Stat(review.Result); !os.IsNotExist(err) {
		t.Fatal("public result already exists")
	}
	if _, err := os.Stat(review.Token + ".consumed"); !os.IsNotExist(err) {
		t.Fatal("public permit consumed")
	}
	token, err := os.ReadFile(review.Token)
	if err != nil || string(token) != expected+"\n" {
		t.Fatal("public one-use permit absent")
	}
	if err := os.Rename(review.Token, review.Token+".consumed"); err != nil {
		t.Fatal(err)
	}
	return review, expected, root
}

type publicCapture struct {
	mu       sync.Mutex
	bytes    []byte
	overflow bool
}

func (capture *publicCapture) Write(input []byte) (int, error) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	remaining := 65536 - len(capture.bytes)
	count := len(input)
	if count > remaining {
		count = remaining
		capture.overflow = true
	}
	capture.bytes = append(capture.bytes, input[:count]...)
	return len(input), nil
}
func (capture *publicCapture) snapshot() (string, bool) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return string(capture.bytes), capture.overflow
}

type publicController struct {
	mu      sync.Mutex
	active  *exec.Cmd
	ctx     context.Context
	watcher int
	exited  bool
}

func (controller *publicController) interrupt() {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.active != nil && !controller.exited {
		_ = controller.active.Process.Signal(os.Interrupt)
	}
}
func (controller *publicController) start(t *testing.T, command *exec.Cmd) {
	t.Helper()
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.ctx.Err() != nil {
		t.Fatal("public controller cancelled before CLI admission")
	}
	if controller.active != nil {
		t.Fatal("public CLI already active")
	}
	watcher, err := unix.Kqueue()
	if err != nil {
		t.Fatal(err)
	}
	syscall.CloseOnExec(watcher)
	if err := command.Start(); err != nil {
		_ = unix.Close(watcher)
		t.Fatal(err)
	}
	controller.active, controller.watcher, controller.exited = command, watcher, false
	event := unix.Kevent_t{Ident: uint64(command.Process.Pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ENABLE | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT}
	if _, err := unix.Kevent(watcher, []unix.Kevent_t{event}, nil, nil); err != nil {
		t.Fatalf("CLI exit witness unavailable; ownership retained: %v", err)
	}
}

// Only this observer reaps. Exit observation precedes Wait, and interrupt shares
// the exited lock: every signal therefore targets a still-unreaped owned PID.
func (controller *publicController) wait(command *exec.Cmd) error {
	events := make([]unix.Kevent_t, 1)
	for {
		count, err := unix.Kevent(controller.watcher, nil, events, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if count == 1 && events[0].Fflags&unix.NOTE_EXIT != 0 {
			break
		}
	}
	controller.mu.Lock()
	controller.exited = true
	controller.mu.Unlock()
	err := command.Wait()
	controller.mu.Lock()
	_ = unix.Close(controller.watcher)
	controller.active = nil
	controller.mu.Unlock()
	return err
}
func (controller *publicController) idle() bool {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.active == nil
}

func publicCommand(review publicReview, backend, name, command string) *exec.Cmd {
	program := review.Programs[backend][name]
	executable := review.CLI
	args := []string{command}
	if backend == "interpreter" {
		args = append(args, program.Source)
	} else {
		executable = program.Artifact
	}
	helper := review.Helper
	if name == "helper-identity" {
		helper = review.BadHelper
	}
	var targetDigest string
	for path, digest := range review.Artifacts {
		if path == review.Target {
			targetDigest = digest
		}
	}
	args = append(args, "--format", "json", "--process-allow", targetDigest+","+review.Target, "--process-launcher", helper, "--process-read-root", review.ReadRoot)
	if name == "pipeline-zero" || name == "pipeline-nonzero" {
		args = append(args, "--write-root", review.WriteRoot)
	}
	if command == "serve" {
		args = append(args, "--listen", "0")
	}
	result := exec.Command(executable, args...)
	result.Dir = filepath.Dir(review.CLI)
	return result
}

func publicFields(t *testing.T, value publicValue) map[string]publicValue {
	t.Helper()
	var fields map[string]publicValue
	if err := json.Unmarshal(value.Value, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}
func publicBool(t *testing.T, value publicValue) bool {
	t.Helper()
	var flag bool
	if err := json.Unmarshal(value.Value, &flag); err != nil {
		t.Fatal(err)
	}
	return flag
}
func publicString(t *testing.T, value publicValue) string {
	t.Helper()
	var text string
	if err := json.Unmarshal(value.Value, &text); err != nil {
		t.Fatal(err)
	}
	return text
}

func TestProcessPublicReviewedLane(t *testing.T) {
	review, manifest, root := publicInputs(t)
	controlFD, err := strconv.Atoi(os.Getenv("LO_PROCESS_PUBLIC_CONTROL_FD"))
	if err != nil || controlFD < 3 {
		t.Fatal("public controller pipe absent")
	}
	syscall.CloseOnExec(controlFD)
	if err := syscall.SetNonblock(controlFD, true); err != nil {
		t.Fatal(err)
	}
	control := os.NewFile(uintptr(controlFD), "public-process-control")
	if control == nil {
		t.Fatal("public control file absent")
	}
	defer control.Close()
	var admission [1]byte
	if _, err := io.ReadFull(control, admission[:]); err != nil || admission[0] != 'A' {
		t.Fatal("public controller did not arm its exit witness")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controller := &publicController{ctx: ctx}
	controlJoined := make(chan struct{})
	go func() {
		defer close(controlJoined)
		var message [1]byte
		_, _ = control.Read(message[:])
		cancel()
		controller.interrupt()
	}()
	defer func() { _ = control.Close(); <-controlJoined }()
	evidence := []*publicEvidence{}
	t.Cleanup(func() {
		unchanged := true
		for path, expected := range review.Artifacts {
			encoded, err := os.ReadFile(publicInside(t, root, path))
			digest := sha256.Sum256(encoded)
			if err != nil || hex.EncodeToString(digest[:]) != expected {
				unchanged = false
				t.Errorf("public artifact changed: %s", path)
			}
		}
		cleanup := controller.idle()
		for _, entry := range evidence {
			cleanup = cleanup && entry.Joined && entry.CleanupConfirmed
		}
		file, err := os.Create(review.Result)
		if err != nil {
			t.Error(err)
			return
		}
		err = json.NewEncoder(file).Encode(struct {
			Manifest  string            `json:"reviewed_manifest_sha256"`
			Phase     string            `json:"phase"`
			Cases     []*publicEvidence `json:"cases"`
			Unchanged bool              `json:"artifacts_unchanged"`
			Cleanup   bool              `json:"cleanup_complete"`
			Passed    bool              `json:"acceptance_passed"`
		}{manifest, review.Phase, evidence, unchanged, cleanup, !t.Failed()})
		if closeErr := file.Close(); err != nil || closeErr != nil {
			t.Errorf("public result write: %v %v", err, closeErr)
		}
	})
	backends, names := []string{"interpreter", "native"}, publicCases
	if review.Phase == "gate-red" {
		backends = backends[:1]
		names = names[:1]
	}
	for _, backend := range backends {
		for _, name := range names {
			entry := &publicEvidence{Backend: backend, Case: name}
			evidence = append(evidence, entry)
			if !t.Run(backend+"/"+name, func(t *testing.T) {
				started := time.Now()
				defer func() { entry.Elapsed = int64(time.Since(started)) }()
				if name == "server-callback" {
					publicServer(t, review, backend, controller, entry)
					entry.Passed = true
					return
				}
				command := publicCommand(review, backend, name, "run")
				var stdout, stderr publicCapture
				command.Stdout, command.Stderr = &stdout, &stderr
				controller.start(t, command)
				err := controller.wait(command)
				if command.ProcessState == nil {
					t.Fatalf("CLI ownership unresolved: %v", err)
				}
				entry.Joined = true
				entry.ExitCode = command.ProcessState.ExitCode()
				out, outOverflow := stdout.snapshot()
				diagnostic, errOverflow := stderr.snapshot()
				if outOverflow || errOverflow {
					t.Fatal("public CLI exceeded bounded capture")
				}
				if err != nil {
					var exited *exec.ExitError
					if !errors.As(err, &exited) {
						t.Fatal(err)
					}
				}
				var report struct {
					Diagnostics []publicDiagnostic `json:"diagnostics"`
				}
				if diagnostic != "" {
					if err := json.Unmarshal([]byte(diagnostic), &report); err != nil || len(report.Diagnostics) != 1 {
						t.Fatalf("public diagnostics: %s", diagnostic)
					}
					current := report.Diagnostics[0]
					entry.Code = current.Code
					if current.Progress != nil {
						fields := publicFields(t, *current.Progress)
						entry.ProgressPresent = true
						entry.StartConfirmed = publicBool(t, fields["start_confirmed"])
						entry.CleanupConfirmed = publicBool(t, fields["cleanup_complete"])
					}
				}
				if entry.Code == "runtime.process-unavailable" {
					entry.CleanupConfirmed = true
				}
				switch name {
				case "normal", "nonzero", "signaled":
					if entry.ExitCode != 0 || diagnostic != "" {
						t.Fatalf("expected canonical Process success, got exit=%d diagnostic=%s", entry.ExitCode, diagnostic)
					}
					var result struct {
						Result publicValue `json:"result"`
					}
					if err := json.Unmarshal([]byte(out), &result); err != nil {
						t.Fatal(err)
					}
					value := result.Result
					if value.Status == "ok" {
						if err := json.Unmarshal(value.Value, &value); err != nil {
							t.Fatal(err)
						}
					}
					if value.Type != "Process.Result" {
						t.Fatal("Process result type missing")
					}
					fields := publicFields(t, value)
					termination := publicFields(t, fields["termination"])
					variant, payload, key := "exited", "0", "exit_code"
					if name == "nonzero" {
						payload = "7"
					}
					if name == "signaled" {
						variant, payload, key = "signaled", "9", "signal"
					}
					if len(termination) != 2 || publicString(t, termination["kind"]) != variant || publicString(t, termination[key]) != payload {
						t.Fatal("Process termination differs")
					}
					var output, errors []byte
					if json.Unmarshal(fields["stdout"].Value, &output) != nil || json.Unmarshal(fields["stderr"].Value, &errors) != nil || !bytes.Equal(output, []byte{0, 255, 'A'}) || !bytes.Equal(errors, []byte{128, 'e'}) {
						t.Fatal("Process result lost raw bytes")
					}
					entry.CleanupConfirmed = true
				case "pipeline-zero", "pipeline-nonzero":
					if entry.ExitCode != 0 || diagnostic != "" {
						t.Fatalf("pipeline failed: %s", diagnostic)
					}
					path := filepath.Join(review.WriteRoot, backend+"-"+name+".bin")
					contents, readErr := os.ReadFile(path)
					if name == "pipeline-zero" {
						if readErr != nil || !bytes.Equal(contents, []byte{0, 255, 'A'}) {
							t.Fatal("pipeline did not preserve bytes")
						}
					} else if !os.IsNotExist(readErr) {
						t.Fatal("nonzero result wrote a file")
					}
					entry.CleanupConfirmed = true
				case "denied-command":
					if entry.ExitCode != 2 || entry.Code != "runtime.host-failure" || !entry.ProgressPresent || publicString(t, publicFields(t, *report.Diagnostics[0].Progress)["stage"]) != "start" || entry.StartConfirmed || !entry.CleanupConfirmed || out != "" {
						t.Fatalf("command denial differs: %s", diagnostic)
					}
				case "denied-cwd":
					if entry.ExitCode != 2 || entry.Code != "runtime.capability-denied" || !entry.ProgressPresent || entry.StartConfirmed || !entry.CleanupConfirmed || out != "" {
						t.Fatalf("cwd denial differs: %s", diagnostic)
					}
				case "helper-identity":
					if entry.ExitCode != 2 || entry.Code != "runtime.host-failure" || !strings.Contains(report.Diagnostics[0].Message, "native helper identity mismatch") || out != "" {
						t.Fatalf("helper identity denial differs: %s", diagnostic)
					}
					entry.CleanupConfirmed = true
				case "parent-read", "parent-write":
					if entry.ExitCode != 2 || entry.Code != "runtime.capability-denied" || out != "" {
						t.Fatalf("Process authority escaped to parent File: %s", diagnostic)
					}
					if name == "parent-write" {
						if _, err := os.Stat(filepath.Join(review.WriteRoot, backend+"-denied-write.bin")); !os.IsNotExist(err) {
							t.Fatal("ungranted parent write happened")
						}
					}
					entry.CleanupConfirmed = true
				case "wall-timeout":
					if entry.ExitCode != 2 || entry.Code != "runtime.cancelled" || !entry.ProgressPresent || publicString(t, publicFields(t, *report.Diagnostics[0].Progress)["primary_class"]) != "cancelled" || !entry.StartConfirmed || !entry.CleanupConfirmed || out != "" {
						t.Fatalf("wall timeout was recovered or lost cleanup: %s", diagnostic)
					}
				default:
					t.Fatal("unknown finite public case")
				}
				entry.Passed = true
			}) {
				return
			}
		}
	}
}

func publicServer(t *testing.T, review publicReview, backend string, controller *publicController, entry *publicEvidence) {
	t.Helper()
	command := publicCommand(review, backend, "server-callback", "serve")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr publicCapture
	command.Stderr = &stderr
	controller.start(t, command)
	ready := make(chan string, 1)
	finished := make(chan struct{})
	stopped := false
	go func() {
		defer close(finished)
		decoder := json.NewDecoder(stdout)
		for {
			var event struct {
				Status  string `json:"status"`
				Servers []struct {
					Address string `json:"address"`
				} `json:"servers"`
			}
			if err := decoder.Decode(&event); err != nil {
				break
			}
			if event.Status == "ready" && len(event.Servers) == 1 {
				select {
				case ready <- event.Servers[0].Address:
				default:
				}
			}
			if event.Status == "stopped" {
				stopped = true
			}
		}
		_ = controller.wait(command)
	}()
	t.Cleanup(func() {
		select {
		case <-finished:
			return
		default:
			controller.interrupt()
		}
		select {
		case <-finished:
		case <-time.After(7 * time.Second):
			t.Error("public server cleanup unconfirmed")
		}
	})
	var address string
	select {
	case address = <-ready:
	case <-finished:
		t.Fatal("public server exited before readiness")
	case <-time.After(7 * time.Second):
		t.Fatal("public server readiness missing")
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	response, err := client.Get("http://" + address + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	closeErr := response.Body.Close()
	client.CloseIdleConnections()
	if err != nil || closeErr != nil || response.StatusCode != 200 || !bytes.Equal(body, []byte{0, 255, 'A'}) {
		t.Fatal("public server Process callback lost bytes")
	}
	controller.interrupt()
	select {
	case <-finished:
	case <-time.After(7 * time.Second):
		t.Fatal("public server did not stop")
	}
	if command.ProcessState == nil || !controller.idle() {
		t.Fatal("public server ownership unresolved")
	}
	entry.Joined = true
	entry.ExitCode = command.ProcessState.ExitCode()
	diagnostic, overflow := stderr.snapshot()
	if entry.ExitCode != 0 || diagnostic != "" || overflow || !stopped {
		t.Fatalf("public server shutdown failed: %s", diagnostic)
	}
	entry.CleanupConfirmed = true
}
