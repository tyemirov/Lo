package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type options struct {
	manifest, work, group, report, goRun string
	count                                int
	verbose                              bool
	workers, builds, guests              int
	linuxSemanticTimeout                 int
	budget                               time.Duration
	maxBytes                             int64
	json, inventory, describe, ci        bool
}

func main() {
	settings := options{}
	flag.StringVar(&settings.manifest, "manifest", "", "Execute an explicit session manifest")
	flag.StringVar(&settings.work, "work", "", "Fresh repository-local session directory")
	flag.StringVar(&settings.group, "group", "", "Select focused groups by regular expression")
	flag.StringVar(&settings.report, "report", "", "Write measured results inside repository scratch")
	flag.StringVar(&settings.goRun, "run", "", "Select Go test names within focused groups")
	flag.IntVar(&settings.count, "count", 1, "Fresh execution count (must be one)")
	flag.BoolVar(&settings.verbose, "v", false, "Show named case progress")
	flag.IntVar(&settings.workers, "workers", min(16, runtime.NumCPU()), "Concurrent isolated test workers")
	flag.IntVar(&settings.builds, "build-slots", min(8, runtime.NumCPU()), "Concurrent backend builds")
	flag.IntVar(&settings.guests, "guest-workers", min(4, max(1, runtime.NumCPU()/4)), "Concurrent guest-consuming test groups")
	flag.IntVar(&settings.linuxSemanticTimeout, "linux-semantic-timeout", 240, "Linux semantic guest test deadline in seconds")
	flag.DurationVar(&settings.budget, "budget", 270*time.Second, "Whole-session deadline")
	flag.Int64Var(&settings.maxBytes, "max-bytes", 10<<30, "Maximum session storage bytes")
	flag.BoolVar(&settings.json, "json", false, "Emit structured progress and results")
	flag.BoolVar(&settings.inventory, "inventory", false, "Print the selected case inventory without executing tests")
	flag.BoolVar(&settings.describe, "describe", false, "Print the complete execution manifest without running commands")
	flag.BoolVar(&settings.ci, "ci", false, "Include formatting and static analysis")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "unexpected session arguments: %q\n", flag.Args())
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := execute(ctx, settings, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func execute(parent context.Context, settings options, stdout, stderr io.Writer) (failure error) {
	started := time.Now()
	if settings.workers < 1 || settings.builds < 1 || settings.guests < 1 || settings.budget <= 0 || settings.maxBytes <= 0 {
		return errors.New("session resource limits must be positive")
	}
	if settings.linuxSemanticTimeout < 1 || settings.linuxSemanticTimeout > 7200 {
		return errors.New("Linux semantic timeout must be 1..7200 seconds")
	}
	if settings.count != 1 {
		return errors.New("session tests execute exactly once with fresh results")
	}
	if settings.ci && (settings.manifest != "" || settings.group != "" || settings.goRun != "") {
		return errors.New("CI requires the complete canonical manifest without focused selections")
	}
	if settings.inventory && settings.manifest != "" {
		return errors.New("source inventory cannot execute an explicit manifest")
	}
	root, err := filepath.EvalSymlinks(".")
	if err != nil {
		return fmt.Errorf("resolve repository: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return fmt.Errorf("session requires the Lo repository root: %w", err)
	}
	if !strings.HasPrefix(string(module), "module github.com/MarcoPoloResearchLab/Lo\n") {
		return errors.New("session requires the Lo repository root module")
	}
	var reportPath string
	if settings.report != "" {
		reportPath, err = measurementPath(root, settings.report)
		if err != nil {
			return err
		}
	}
	selected := regexp.MustCompile(".")
	if settings.group != "" {
		selected, err = regexp.Compile(settings.group)
		if err != nil {
			return fmt.Errorf("select groups: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(parent, settings.budget)
	defer cancel()
	var spec manifest
	if settings.manifest != "" {
		spec, err = readManifest(root, settings.manifest)
		if err != nil {
			return err
		}
	}
	if settings.inventory && settings.manifest == "" {
		inventory, err := discover(root)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(inventory)
	}
	work := settings.work
	if work == "" {
		scratch, err := localPath(root, filepath.Join(root, "_scratch"))
		if err != nil {
			return fmt.Errorf("session scratch root: %w", err)
		}
		work, err = os.MkdirTemp(scratch, "*.noindex")
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
	} else {
		work, err = localPath(root, work)
		if err != nil {
			return fmt.Errorf("session directory: %w", err)
		}
		relative, _ := filepath.Rel(filepath.Join(root, "_scratch"), work)
		if !filepath.IsLocal(relative) {
			return errors.New("session directory must stay inside repository _scratch")
		}
		if err = os.Mkdir(work, os.ModePerm); err != nil {
			return fmt.Errorf("create fresh session %s: %w", work, err)
		}
	}
	output := newReporter(started, settings.json, stdout, stderr)
	output.storage = &storageTracker{work: work, limit: settings.maxBytes}
	stopHeartbeat := output.startHeartbeat(cancel)
	completed := 0
	var stop func() error
	defer func() {
		failure = errors.Join(failure, output.semanticFinish("fail"))
		if stop != nil {
			failure = errors.Join(failure, stop())
		}
		failure = errors.Join(failure, removeSession(work))
		stopHeartbeat()
		failure = errors.Join(failure, output.monitorError())
		if ctx.Err() != nil || time.Since(started) >= settings.budget {
			failure = errors.Join(failure, errors.New("session deadline exceeded during execution or cleanup"))
		}
		status := "pass"
		if settings.describe {
			status = "planned"
		}
		if failure != nil {
			status = "fail"
		}
		report := output.summary(status, completed, len(spec.Groups)+len(spec.Finalize), time.Since(started))
		if reportPath != "" {
			if err := writeMeasurement(root, reportPath, report); err != nil {
				failure = errors.Join(failure, err)
				report["status"] = "fail"
			}
		}
		if !settings.describe {
			failure = errors.Join(failure, json.NewEncoder(stdout).Encode(report))
		}
	}()
	if reportPath != "" {
		relative, _ := filepath.Rel(work, reportPath)
		if filepath.IsLocal(relative) {
			return errors.New("measurement report must stay outside the disposable session directory")
		}
		if err := os.MkdirAll(filepath.Dir(reportPath), os.ModePerm); err != nil {
			return fmt.Errorf("create measurement report directory: %w", err)
		}
	}
	base, err := sessionEnvironment(root, work)
	if err != nil {
		return err
	}
	if settings.manifest == "" {
		spec, err = canonicalManifest(ctx, root, work, base, settings.ci, selected, settings.goRun, settings.group, settings.linuxSemanticTimeout)
		if err != nil {
			return err
		}
	}
	if settings.manifest == "" {
		data, err := commandOutput(ctx, root, base, "python3", "-B", "tools/testsession/preflight.py", "fingerprint")
		if err != nil {
			return err
		}
		var source struct {
			SHA string `json:"source_sha256"`
		}
		if err = json.Unmarshal(data, &source); err != nil {
			return fmt.Errorf("session source fingerprint: %w", err)
		}
		if source.SHA == "" {
			return errors.New("session source fingerprint is missing")
		}
		output.fingerprint = source.SHA
	}
	output.resources = map[string]int{"workers": settings.workers, "build_slots": settings.builds, "guest_workers": settings.guests}
	if settings.goRun != "" {
		pattern, err := regexp.Compile(strings.Split(settings.goRun, "/")[0])
		if err != nil {
			return err
		}
		groups := []jobSpec{}
		for _, entry := range spec.Groups {
			if entry.Format == "go" && len(entry.Expected) == 1 && pattern.MatchString(entry.Expected[0]) {
				groups = append(groups, entry)
			}
		}
		spec.Groups = groups
		spec.Finalize = nil
	}
	for _, entry := range append(append([]jobSpec{}, spec.Prepare...), spec.Groups...) {
		if err = validateJob(entry); err != nil {
			return err
		}
		if err = validateJobPaths(root, work, entry); err != nil {
			return err
		}
	}
	for _, entry := range spec.Finalize {
		if err = validateJob(entry); err != nil {
			return err
		}
		if err = validateJobPaths(root, work, entry); err != nil {
			return err
		}
	}
	if settings.group != "" {
		groups := []jobSpec{}
		for _, entry := range spec.Groups {
			if selected.MatchString(entry.ID) {
				groups = append(groups, entry)
			}
		}
		spec.Groups = groups
	}
	if len(spec.Groups) == 0 {
		return errors.New("session selection contains no test groups")
	}
	seen := map[string]bool{}
	for _, entry := range append(append(append([]jobSpec{}, spec.Prepare...), spec.Groups...), spec.Finalize...) {
		if seen[entry.ID] {
			return fmt.Errorf("duplicate group %s", entry.ID)
		}
		seen[entry.ID] = true
	}
	for _, entry := range append(append([]jobSpec{}, spec.Prepare...), spec.Finalize...) {
		if len(entry.After) != 0 {
			return fmt.Errorf("only group jobs can declare dependencies: %s", entry.ID)
		}
	}
	if err := validateDependencies(spec.Groups); err != nil {
		return err
	}
	if settings.describe {
		return json.NewEncoder(stdout).Encode(map[string]any{"source_sha256": output.fingerprint, "prepare": spec.Prepare, "groups": spec.Groups, "finalize": spec.Finalize})
	}
	if err = output.storage.declare(root, base, spec); err != nil {
		return err
	}
	if err = storageAvailable(work, settings.maxBytes); err != nil {
		return err
	}
	for _, entry := range spec.Prepare {
		var count int
		if count, err = runJob(ctx, root, work, base, entry, output); err != nil {
			return err
		}
		if entry.Format == "go" || entry.Format == "pytest" || entry.Format == "events" {
			completed += count
		}
		if err = output.storage.check(); err != nil {
			return err
		}
	}
	if settings.manifest == "" {
		runtimeManifest := filepath.Join(work, "native-runtime/toolchain.json")
		if _, err := os.Stat(runtimeManifest); err == nil {
			base = environment(base, map[string]string{"LO_LLVM_TOOLCHAIN": runtimeManifest}, work)
		}
	}
	if settings.manifest == "" {
		base, stop, err = startServices(ctx, root, work, base, settings.builds, settings.guests, spec.Groups, output)
		if err != nil {
			return err
		}
	}
	if len(spec.Finalize) > 0 {
		if err = output.semanticStart(filepath.Join(work, "semantic-host.jsonl")); err != nil {
			return err
		}
	}
	var groupCompleted int
	groupCompleted, err = runGroups(ctx, root, work, base, spec.Groups, settings.workers, settings.guests, output)
	completed += groupCompleted
	if ctx.Err() != nil {
		err = errors.Join(err, fmt.Errorf("session deadline or cancellation: %w", ctx.Err()))
	}
	if err == nil {
		err = output.storage.check()
	}
	if err == nil {
		err = output.semanticFinish("pass")
	}
	if err == nil {
		for _, entry := range spec.Finalize {
			var count int
			count, err = runJob(ctx, root, work, base, entry, output)
			completed += count
			if err != nil {
				break
			}
			if err = output.storage.check(); err != nil {
				break
			}
		}
	}
	if err == nil && settings.manifest == "" {
		data, measureErr := commandOutput(ctx, root, base, "python3", "-B", "tools/testsession/preflight.py", "fingerprint")
		var current struct {
			SHA string `json:"source_sha256"`
		}
		if measureErr == nil {
			measureErr = json.Unmarshal(data, &current)
		}
		if measureErr != nil {
			err = measureErr
		} else if current.SHA != output.fingerprint {
			err = errors.New("source changed during the session; timing and results do not establish one source identity")
		}
	}
	return err
}

func removeSession(work string) error {
	if err := os.RemoveAll(work); err != nil {
		return fmt.Errorf("remove session %s: %w", work, err)
	}
	return nil
}

func measurementPath(root, path string) (string, error) {
	selected, err := localPath(root, path)
	if err != nil {
		return "", fmt.Errorf("measurement report: %w", err)
	}
	relative, _ := filepath.Rel(filepath.Join(root, "_scratch"), selected)
	if !filepath.IsLocal(relative) || relative == "." {
		return "", errors.New("measurement report must stay inside repository scratch")
	}
	if info, err := os.Stat(selected); err == nil && info.IsDir() {
		return "", fmt.Errorf("measurement report must name a file: %s", selected)
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect measurement report: %w", err)
	}
	return selected, nil
}

func writeMeasurement(root, path string, report map[string]any) (failure error) {
	if _, err := measurementPath(root, path); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".session-report-")
	if err != nil {
		return fmt.Errorf("create measurement report: %w", err)
	}
	staged := file.Name()
	defer func() {
		if staged != "" {
			failure = errors.Join(failure, os.Remove(staged))
		}
	}()
	if err := errors.Join(json.NewEncoder(file).Encode(report), file.Close()); err != nil {
		return fmt.Errorf("write measurement report: %w", err)
	}
	if err := os.Rename(staged, path); err != nil {
		return fmt.Errorf("publish measurement report: %w", err)
	}
	staged = ""
	return nil
}
