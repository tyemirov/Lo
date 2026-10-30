//go:build lo_process_native_qualification && darwin

package startup_test

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
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/experiments/process-startup"
	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

var nativeCaseNames = []string{"normal", "immediate-zero", "immediate-nonzero", "launcher-input-rejection", "eof-without-permit", "cancel-before-permit", "cancel-after-permit", "rapid-exit"}

type nativeArtifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type nativeReview struct {
	Launcher  string                    `json:"launcher"`
	Target    string                    `json:"target"`
	Directory string                    `json:"directory"`
	RunToken  string                    `json:"run_token"`
	Result    string                    `json:"result"`
	Cases     []string                  `json:"cases"`
	Artifacts map[string]nativeArtifact `json:"artifacts"`
}

func nativeInside(t *testing.T, root, path string) string {
	t.Helper()
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	relative, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatalf("native probe path outside Lo: %q", path)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if relative, err = filepath.Rel(root, parent); err != nil || !filepath.IsLocal(relative) {
		t.Fatal("native probe parent resolves outside Lo")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil && resolved != path {
		t.Fatal("native probe endpoint is a symlink")
	}
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return path
}

func nativeReviewedInputs(t *testing.T) (nativeReview, string) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	path, reviewed := os.Getenv("LO_PROCESS_STARTUP_NATIVE_MANIFEST"), os.Getenv("LO_PROCESS_STARTUP_NATIVE_REVIEWED_SHA256")
	if path == "" || reviewed == "" {
		t.Fatal("native lane requires an explicit reviewed manifest and SHA-256; no launch authorized")
	}
	encoded, err := os.ReadFile(nativeInside(t, root, path))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	if hex.EncodeToString(digest[:]) != reviewed {
		t.Fatal("native manifest differs from reviewed SHA-256")
	}
	var review nativeReview
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&review); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(review.Cases, nativeCaseNames) || len(review.Artifacts) == 0 {
		t.Fatal("native manifest does not select the exact finite test lane")
	}
	sealed := map[string]bool{}
	for name, artifact := range review.Artifacts {
		path := nativeInside(t, root, artifact.Path)
		artifact.Path = path
		review.Artifacts[name] = artifact
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != path {
			t.Fatal("reviewed artifact must be an exact repository-contained file")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		actual := sha256.Sum256(data)
		if hex.EncodeToString(actual[:]) != artifact.SHA256 {
			t.Fatalf("reviewed artifact differs: %s", path)
		}
		sealed[path] = true
	}
	review.Launcher = nativeInside(t, root, review.Launcher)
	review.Target = nativeInside(t, root, review.Target)
	review.Directory = nativeInside(t, root, review.Directory)
	for _, binary := range []string{review.Launcher, review.Target} {
		if !sealed[binary] {
			t.Fatal("native binary is absent from reviewed artifacts")
		}
	}
	testBinary, err := os.Executable()
	if err != nil || !sealed[nativeInside(t, root, testBinary)] {
		t.Fatal("running native test binary is not a reviewed artifact")
	}
	for _, name := range []string{"startup.go", "pending.go", "native_darwin.go", "startup_test.go", "native_test.go", "native_probe.mk", "native_probe.py", "native/launcher.c", "native/target.c"} {
		if !sealed[filepath.Join(root, "experiments/process-startup", name)] {
			t.Fatalf("native source is not sealed: %s", name)
		}
	}
	tests, err := filepath.Glob(filepath.Join(root, "experiments/process-startup", "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range tests {
		if !sealed[path] {
			t.Fatalf("candidate test source is not sealed: %s", path)
		}
	}
	review.RunToken = nativeInside(t, root, review.RunToken)
	review.Result = nativeInside(t, root, review.Result)
	if sealed[review.Result] {
		t.Fatal("native result cannot replace a sealed artifact")
	}
	for _, path := range []string{review.Result, review.RunToken + ".consumed"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("native run record already exists")
		}
	}
	token, err := os.ReadFile(review.RunToken)
	if err != nil || string(token) != reviewed+"\n" {
		t.Fatal("missing exact one-use native run permit")
	}
	if err := os.Rename(review.RunToken, review.RunToken+".consumed"); err != nil {
		t.Fatalf("native run permit already consumed: %v", err)
	}
	return review, reviewed
}

type nativeObservedPrimary struct {
	startup.Primary
	mode                               string
	cancel                             context.CancelFunc
	waited                             chan struct{}
	waitOnce                           sync.Once
	permits                            atomic.Int32
	eofTerminated                      atomic.Bool
	events                             chan startup.Event
	nativeWaited                       chan struct{}
	eventsJoined                       chan struct{}
	executed                           atomic.Bool
	inputRejected                      atomic.Bool
	eventOverflow                      atomic.Bool
	published                          atomic.Bool
	permitSucceeded                    atomic.Bool
	permissionRejected                 atomic.Bool
	releaseComplete                    atomic.Bool
	telemetryMu                        sync.Mutex
	admitted                           time.Time
	terminationKind                    string
	terminationElapsed, releaseElapsed time.Duration
	stopCalls                          int
}

type nativeCaseEvidence struct {
	Status                  string `json:"status"`
	PrimaryCreated          bool   `json:"primary_created"`
	PermitPublished         bool   `json:"permit_published"`
	PermitSucceeded         bool   `json:"permit_succeeded"`
	EOFTerminatedBeforeStop bool   `json:"eof_termination_observed_before_stop"`
	Executed                bool   `json:"executed_observed"`
	Termination             string `json:"termination"`
	CleanupComplete         bool   `json:"cleanup_complete"`
	StopSignalAttempted     bool   `json:"stop_signal_attempted"`
	StopSignalFailed        bool   `json:"stop_signal_failed"`
	TerminationNanoseconds  int64  `json:"admission_to_termination_ns"`
	ReleaseNanoseconds      int64  `json:"admission_to_release_ns"`
}

func (p *nativeObservedPrimary) Stop(ctx context.Context) error {
	p.telemetryMu.Lock()
	p.stopCalls++
	p.telemetryMu.Unlock()
	return p.Primary.Stop(ctx)
}

func (p *nativeObservedPrimary) Release(ctx context.Context) error {
	err := p.Primary.Release(ctx)
	if err == nil {
		p.telemetryMu.Lock()
		p.releaseElapsed = time.Since(p.admitted)
		p.telemetryMu.Unlock()
		p.releaseComplete.Store(true)
	}
	return err
}

func (p *nativeObservedPrimary) Events() <-chan startup.Event { return p.events }

func (p *nativeObservedPrimary) observe(event startup.Event) {
	if event.Kind == startup.Executed {
		p.executed.Store(true)
	}
	if event.Kind == startup.Failed && event.Stage == startup.Input {
		p.inputRejected.Store(true)
	}
	if event.Kind == startup.Failed && event.Stage == startup.Permission {
		p.permissionRejected.Store(true)
	}
	select {
	case p.events <- event:
	default:
		p.eventOverflow.Store(true)
	}
}

func (p *nativeObservedPrimary) forwardEvents() {
	defer close(p.eventsJoined)
	defer close(p.events)
	events := p.Primary.Events()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			p.observe(event)
		case <-p.nativeWaited:
			for {
				select {
				case event, ok := <-events:
					if !ok {
						return
					}
					p.observe(event)
				default:
					return
				}
			}
		}
	}
}

func (p *nativeObservedPrimary) Wait(ctx context.Context) (value.ProcessTermination, error) {
	termination, err := p.Primary.Wait(ctx)
	close(p.nativeWaited)
	<-p.eventsJoined
	if termination != nil {
		p.telemetryMu.Lock()
		p.terminationKind = termination.Kind().Text()
		p.terminationElapsed = time.Since(p.admitted)
		p.telemetryMu.Unlock()
	}
	p.waitOnce.Do(func() { close(p.waited) })
	return termination, err
}

func (p *nativeObservedPrimary) ArmExecution(ctx context.Context) error {
	if err := p.Primary.ArmExecution(ctx); err != nil {
		return err
	}
	if p.mode == "cancel-before-permit" {
		p.cancel()
		return context.Canceled
	}
	if p.mode != "eof-without-permit" {
		return nil
	}
	transport, ok := p.Primary.(interface{ ClosePermit() error })
	if !ok {
		return errors.New("native primary does not expose its candidate permit transport")
	}
	if err := transport.ClosePermit(); err != nil {
		return err
	}
	select {
	case <-p.waited:
		p.telemetryMu.Lock()
		defer p.telemetryMu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		if deadline, present := ctx.Deadline(); present && !time.Now().Before(deadline) {
			return context.DeadlineExceeded
		}
		if p.stopCalls != 0 || p.terminationKind == "" || !p.permissionRejected.Load() {
			return errors.New("permit EOF lacks pre-Stop termination evidence")
		}
		p.eofTerminated.Store(true)
		return errors.New("permit EOF terminated launcher before Stop")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *nativeObservedPrimary) Permit(ctx context.Context) (bool, error) {
	p.permits.Add(1)
	published, err := p.Primary.Permit(ctx)
	if published {
		p.published.Store(true)
	}
	if published && err == nil {
		p.permitSucceeded.Store(true)
	}
	if published && err == nil && p.mode == "cancel-after-permit" {
		p.cancel()
	}
	return published, err
}

func nativeProgram(t *testing.T, review nativeReview, target, mode, input string) execution.Program {
	t.Helper()
	text, err := source.New(filepath.Join(review.Directory, "native-case.lo"), fmt.Sprintf(`flow Main() -> Process.Result! { executable = Process.Executable.new(File.Path.new(%q)); command = Process.Command.new(executable,List.new(%q),Map.new(Record.new(key:"NAME",value:"explicit")),File.Path.new(%q),UTF8.Bytes.new(%q)); result = commit command; return result }; return Main()`, target, mode, review.Directory, input))
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

func TestNativeProcessStartupReviewedLane(t *testing.T) {
	review, manifest := nativeReviewedInputs(t)
	controlFD, err := strconv.Atoi(os.Getenv("LO_PROCESS_STARTUP_NATIVE_CONTROL_FD"))
	if err != nil || controlFD < 3 {
		t.Fatal("native lane requires an owned controller control pipe")
	}
	syscall.CloseOnExec(controlFD)
	if err := syscall.SetNonblock(controlFD, true); err != nil {
		t.Fatal(err)
	}
	control := os.NewFile(uintptr(controlFD), "native-controller")
	if control == nil {
		t.Fatal("native control pipe absent")
	}
	defer control.Close()
	var admission [1]byte
	if _, err := io.ReadFull(control, admission[:]); err != nil || admission[0] != 'A' {
		t.Fatal("native controller did not arm its non-reaping exit witness")
	}
	suiteContext, stopSuite := context.WithCancel(context.Background())
	defer stopSuite()
	controlJoined := make(chan struct{})
	go func() {
		defer close(controlJoined)
		var cancellation [1]byte
		_, _ = control.Read(cancellation[:])
		stopSuite()
	}()
	defer func() { _ = control.Close(); <-controlJoined }()
	observed := map[string]nativeCaseEvidence{}
	t.Cleanup(func() {
		unchanged := true
		for _, artifact := range review.Artifacts {
			path := artifact.Path
			data, err := os.ReadFile(path)
			digest := sha256.Sum256(data)
			if err != nil || hex.EncodeToString(digest[:]) != artifact.SHA256 {
				unchanged = false
				t.Errorf("native artifact changed after run: %s", artifact.Path)
			}
		}
		file, err := os.Create(review.Result)
		if err != nil {
			t.Error(err)
			return
		}
		err = json.NewEncoder(file).Encode(struct {
			Manifest           string                        `json:"reviewed_manifest_sha256"`
			Cases              map[string]nativeCaseEvidence `json:"cases"`
			ArtifactsUnchanged bool                          `json:"reviewed_artifacts_unchanged"`
			Qualified          bool                          `json:"qualified_process_backend"`
			CleanupComplete    bool                          `json:"cleanup_complete"`
		}{manifest, observed, unchanged, false, nativeCleanupComplete(observed)})
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			t.Errorf("write native result: %v %v", err, closeErr)
		}
	})
	for _, mode := range nativeCaseNames {
		if suiteContext.Err() != nil {
			t.Error("controller cancelled native lane; no further fixture admitted")
			return
		}
		if !t.Run(mode, func(t *testing.T) {
			observed[mode] = nativeCaseEvidence{Status: "attempted"}
			launcher, target := review.Launcher, review.Target
			if mode == "launcher-input-rejection" {
				target = filepath.Join(review.Directory, "unexpected-executable")
			}
			creator, err := startup.NewDarwinCreator(launcher)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(suiteContext, 5*time.Second)
			defer cancel()
			created := make(chan *nativeObservedPrimary, 1)
			admitted := time.Now()
			wrapped := creatorFunc(func(ctx context.Context, invocation adapter.ProcessInvocation) (startup.Primary, error) {
				primary, err := creator.Create(ctx, invocation)
				if primary == nil {
					return nil, err
				}
				observed := &nativeObservedPrimary{Primary: primary, mode: mode, cancel: cancel, admitted: admitted, waited: make(chan struct{}), events: make(chan startup.Event, 16), nativeWaited: make(chan struct{}), eventsJoined: make(chan struct{})}
				go observed.forwardEvents()
				created <- observed
				return observed, err
			})
			supervisor, err := startup.NewSupervisor(wrapped)
			if err != nil {
				t.Fatal(err)
			}
			lifecycle, err := adapter.NewProcessLifecycle(supervisor, adapter.DefaultProcessLimits())
			if err != nil {
				t.Fatal(err)
			}
			boundary, err := adapter.WithProcessLifecycle(inertBoundary{}, lifecycle)
			if err != nil {
				t.Fatal(err)
			}
			input := ""
			if mode == "normal" {
				input = "stdin"
			}
			failures := make(chan error, 1)
			output, runErr := nativeProgram(t, review, target, mode, input).Run(ctx, failureBoundary{Boundary: boundary, failures: failures}, execution.DefaultBudget(), device.DefaultConfig())
			var primary *nativeObservedPrimary
			select {
			case primary = <-created:
			default:
				t.Fatalf("no owned native primary: %v", runErr)
			}
			if primary.eventOverflow.Load() {
				t.Fatal("native primary exceeded bounded startup evidence queue")
			}
			if !primary.StartConfirmed() || !primary.releaseComplete.Load() {
				t.Fatal("native ownership was not confirmed and fully released")
			}
			primary.telemetryMu.Lock()
			evidence := nativeCaseEvidence{Status: "attempted", PrimaryCreated: primary.StartConfirmed(), PermitPublished: primary.published.Load(), PermitSucceeded: primary.permitSucceeded.Load(), EOFTerminatedBeforeStop: primary.eofTerminated.Load(), Executed: primary.executed.Load(), Termination: primary.terminationKind, CleanupComplete: primary.releaseComplete.Load(), TerminationNanoseconds: int64(primary.terminationElapsed), ReleaseNanoseconds: int64(primary.releaseElapsed)}
			primary.telemetryMu.Unlock()
			signalEvidence, ok := primary.Primary.(interface{ StopSignalEvidence() (bool, bool) })
			if !ok {
				t.Fatal("native primary lost signal-attempt evidence")
			}
			evidence.StopSignalAttempted, evidence.StopSignalFailed = signalEvidence.StopSignalEvidence()
			observed[mode] = evidence
			if evidence.Termination != "exited" && evidence.Termination != "signaled" {
				t.Fatal("native primary has no known termination variant")
			}
			switch mode {
			case "normal", "immediate-zero", "immediate-nonzero", "rapid-exit":
				if runErr != nil {
					t.Fatal(runErr)
				}
				result, failure := output.(value.Fallible).Outcome()
				if failure != nil {
					t.Fatal(failure)
				}
				completed := result.(value.ProcessResult)
				code, err := completed.Termination().ExitCode()
				want := int64(0)
				if mode == "immediate-nonzero" {
					want = 7
				}
				if err != nil || code.Big().Int64() != want || primary.permits.Load() != 1 || !primary.executed.Load() {
					t.Fatalf("native completion lost immediate-exec evidence: %v %v", code, err)
				}
				if mode == "normal" && (string(completed.Stdout().Bytes()) != "stdout" || string(completed.Stderr().Bytes()) != "stderr") {
					t.Fatal("native output channels differ")
				}
			default:
				if output != nil || runErr == nil {
					t.Fatal("failed startup published a result")
				}
				select {
				case raw := <-failures:
					var failure adapter.ProcessFailure
					if !errors.As(raw, &failure) || !failure.ProcessProgress().Field("cleanup_complete").(value.Boolean).Bool() || !failure.ProcessProgress().Field("start_confirmed").(value.Boolean).Bool() {
						t.Fatalf("negative native case lost confirmed cleanup: %v", raw)
					}
				default:
					t.Fatal("negative native case did not retain lifecycle failure evidence")
				}
				if mode == "cancel-before-permit" && primary.permits.Load() != 0 {
					t.Fatal("cancellation before permit activated target")
				}
				if (mode == "cancel-before-permit" || mode == "eof-without-permit") && primary.executed.Load() {
					t.Fatal("unpermitted target produced execution evidence")
				}
				if mode == "cancel-after-permit" {
					host := execution.BoundaryHostError(runErr)
					if primary.permits.Load() != 1 || !primary.published.Load() || !primary.permitSucceeded.Load() || !errors.Is(ctx.Err(), context.Canceled) || !errors.Is(runErr, context.Canceled) || host == nil || host.Code() != diagnostic.RuntimeCancelled {
						t.Fatalf("post-permit case did not produce expected external cancellation: %v", runErr)
					}
				}
				if mode == "eof-without-permit" {
					var failure execution.Failure
					if !primary.eofTerminated.Load() || primary.permits.Load() != 0 || evidence.StopSignalAttempted || ctx.Err() != nil || execution.BoundaryHostError(runErr) != nil || !errors.As(runErr, &failure) || failure.Value().Kind() != "Process.ExecutionFailed" {
						t.Fatalf("permit EOF did not produce ordinary startup failure before Stop/deadline: %v", runErr)
					}
				}
				if mode == "launcher-input-rejection" {
					var failure execution.Failure
					if !errors.As(runErr, &failure) || failure.Value().Kind() != "Process.ExecutionFailed" || !primary.inputRejected.Load() || primary.executed.Load() || primary.permits.Load() != 0 {
						t.Fatalf("launcher Input rejection lost closed-stage evidence: %v", runErr)
					}
				}
			}
			evidence.Status = "passed"
			observed[mode] = evidence
		}) {
			return
		}
	}
}

func nativeCleanupComplete(cases map[string]nativeCaseEvidence) bool {
	if len(cases) == 0 {
		return false
	}
	for _, evidence := range cases {
		if !evidence.PrimaryCreated || !evidence.CleanupComplete {
			return false
		}
	}
	return true
}
