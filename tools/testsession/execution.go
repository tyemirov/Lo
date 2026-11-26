package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type jobSpec struct {
	ID          string              `json:"id"`
	Argv        []string            `json:"argv"`
	Cwd         string              `json:"cwd,omitempty"`
	Environment map[string]string   `json:"environment,omitempty"`
	Format      string              `json:"format,omitempty"`
	Package     string              `json:"package,omitempty"`
	Resource    string              `json:"resource,omitempty"`
	Expected    []string            `json:"expected,omitempty"`
	AllowSkips  map[string][]string `json:"allow_skips,omitempty"`
	Release     []string            `json:"release,omitempty"`
	After       []string            `json:"after,omitempty"`
}

type manifest struct {
	Prepare  []jobSpec `json:"prepare,omitempty"`
	Groups   []jobSpec `json:"groups"`
	Finalize []jobSpec `json:"finalize,omitempty"`
}

const (
	resourceHost     = "host"
	resourceGuest    = "guest"
	resourceHostHTTP = "host-http"
)

type testEvent struct{ Action, Test, Output, Package string }

type reporter struct {
	sync.Mutex
	started        time.Time
	structured     bool
	stdout, stderr io.Writer
	active         map[string]time.Time
	durations      map[string]float64
	markers        map[string]int
	semantic       *os.File
	fingerprint    string
	resources      map[string]int
	monitorFailure error
	nextWorker     uint64
	storage        *storageTracker
}

func newReporter(started time.Time, structured bool, stdout, stderr io.Writer) *reporter {
	return &reporter{started: started, structured: structured, stdout: stdout, stderr: stderr, active: map[string]time.Time{}, durations: map[string]float64{}, markers: map[string]int{}}
}

func (r *reporter) event(kind, id, action string) {
	r.Lock()
	defer r.Unlock()
	if kind == "group" {
		if action == "start" {
			r.active[id] = time.Now()
		} else {
			if started, ok := r.active[id]; ok {
				r.durations[id] = time.Since(started).Seconds()
			}
			delete(r.active, id)
		}
	}
	if r.structured {
		_ = json.NewEncoder(r.stdout).Encode(map[string]any{"type": kind, "id": id, "action": action, "elapsed_seconds": time.Since(r.started).Seconds()})
	} else {
		fmt.Fprintf(r.stderr, "[%6.1fs] %s %s %s\n", time.Since(r.started).Seconds(), kind, action, id)
	}
}

func (r *reporter) heartbeat() {
	r.Lock()
	defer r.Unlock()
	names := []string{}
	for name := range r.active {
		names = append(names, name)
	}
	fmt.Fprintf(r.stderr, "[%6.1fs] active=%d %s\n", time.Since(r.started).Seconds(), len(names), strings.Join(names, ", "))
}

func (r *reporter) workerDirectory(work string) string {
	r.Lock()
	ordinal := r.nextWorker
	r.nextWorker++
	r.Unlock()
	return filepath.Join(work, "w", strconv.FormatUint(ordinal, 36))
}

func (r *reporter) startHeartbeat(cancel context.CancelFunc) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				r.heartbeat()
				if err := r.storage.check(); err != nil {
					r.Lock()
					if r.monitorFailure == nil {
						r.monitorFailure = err
					}
					r.Unlock()
					cancel()
				}
			}
		}
	}()
	return func() { close(stop); <-done }
}

func (r *reporter) monitorError() error {
	r.Lock()
	defer r.Unlock()
	return r.monitorFailure
}
func (r *reporter) summary(status string, completed, groups int, elapsed time.Duration) map[string]any {
	r.Lock()
	defer r.Unlock()
	report := map[string]any{"type": "summary", "status": status, "completed": completed, "groups": groups, "elapsed_seconds": elapsed.Seconds(), "group_seconds": r.durations, "markers": r.markers}
	report["source_sha256"] = r.fingerprint
	report["max_bytes"] = r.storage.limit
	report["observed_storage_bytes"] = r.storage.observedPeak()
	for key, value := range r.resources {
		report[key] = value
	}
	return report
}

func (r *reporter) marker(text string) {
	r.Lock()
	defer r.Unlock()
	for _, marker := range []string{"LO_RUNTIME_OBJECT=built:", "LO_RUNTIME_OBJECT=reused:", "LO_NATIVE_RUNTIME="} {
		if strings.Contains(text, marker) {
			r.markers[marker] += strings.Count(text, marker)
		}
	}
	for _, field := range strings.Fields(text) {
		for _, name := range []string{"LO_GUEST_CASE_NS", "LO_GUEST_TOTAL_NS"} {
			if value, ok := strings.CutPrefix(field, name+"="); ok {
				if ns, err := strconv.ParseInt(value, 10, 64); err == nil && ns > 0 {
					r.markers[name] += int(ns)
				}
			}
		}
	}
}

const integrationPackage = "github.com/MarcoPoloResearchLab/Lo/tests/integration"

func (r *reporter) record(event testEvent) error {
	if event.Package != integrationPackage || event.Test == "" {
		return nil
	}
	r.Lock()
	defer r.Unlock()
	if r.semantic == nil {
		return nil
	}
	return json.NewEncoder(r.semantic).Encode(event)
}

func (r *reporter) semanticStart(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	r.semantic = file
	return json.NewEncoder(file).Encode(testEvent{Action: "start", Package: integrationPackage})
}
func (r *reporter) semanticFinish(status string) error {
	r.Lock()
	defer r.Unlock()
	if r.semantic == nil {
		return nil
	}
	file := r.semantic
	r.semantic = nil
	return errors.Join(json.NewEncoder(file).Encode(testEvent{Action: status, Package: integrationPackage}), file.Close())
}

func readManifest(root, path string) (manifest, error) {
	var result manifest
	path, err := localPath(root, path)
	if err != nil {
		return result, err
	}
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 16<<20))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("read session manifest: %w", err)
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return result, errors.New("session manifest contains trailing data")
	}
	return result, nil
}

func validateJob(entry jobSpec) error {
	if entry.Resource != "" && entry.Resource != resourceHost && entry.Resource != resourceGuest && entry.Resource != resourceHostHTTP {
		return fmt.Errorf("group %s has unknown resource %s", entry.ID, entry.Resource)
	}
	if entry.ID == "" || len(entry.Argv) == 0 || entry.Argv[0] == "" {
		return errors.New("session jobs require an id and command")
	}
	if entry.Format != "" && entry.Format != "command" && entry.Format != "go" && entry.Format != "events" && entry.Format != "pytest" && entry.Format != "rust" {
		return fmt.Errorf("group %s has unknown format %s", entry.ID, entry.Format)
	}
	seen := map[string]bool{}
	for _, name := range entry.Expected {
		if name == "" || seen[name] {
			return fmt.Errorf("group %s has duplicate or empty expected case", entry.ID)
		}
		seen[name] = true
	}
	dependencies := map[string]bool{}
	for _, name := range entry.After {
		if name == "" || dependencies[name] {
			return fmt.Errorf("group %s has duplicate or empty dependency", entry.ID)
		}
		dependencies[name] = true
	}
	return nil
}

func validateDependencies(groups []jobSpec) error {
	degrees := make(map[string]int, len(groups))
	consumers := make(map[string][]string, len(groups))
	for _, entry := range groups {
		degrees[entry.ID] = len(entry.After)
	}
	for _, entry := range groups {
		for _, dependency := range entry.After {
			if _, exists := degrees[dependency]; !exists {
				return fmt.Errorf("group %s dependency %s is not a selected group", entry.ID, dependency)
			}
			consumers[dependency] = append(consumers[dependency], entry.ID)
		}
	}
	ready := []string{}
	for name, degree := range degrees {
		if degree == 0 {
			ready = append(ready, name)
		}
	}
	completed := 0
	for len(ready) > 0 {
		name := ready[len(ready)-1]
		ready = ready[:len(ready)-1]
		completed++
		for _, consumer := range consumers[name] {
			degrees[consumer]--
			if degrees[consumer] == 0 {
				ready = append(ready, consumer)
			}
		}
	}
	if completed != len(groups) {
		blocked := []string{}
		for name, degree := range degrees {
			if degree != 0 {
				blocked = append(blocked, name)
			}
		}
		slices.Sort(blocked)
		return fmt.Errorf("group dependency cycle prevents execution: %s", strings.Join(blocked, ", "))
	}
	return nil
}

var stateVariables = map[string]bool{
	"HOME": true, "TMPDIR": true, "TMP": true, "TEMP": true, "GOTMPDIR": true,
	"GOCACHE": true, "GOMODCACHE": true, "GOPATH": true,
	"CARGO_HOME": true, "CARGO_TARGET_DIR": true, "RUSTUP_HOME": true,
	"CLANG_MODULE_CACHE_PATH": true, "CCACHE_DIR": true, "PIP_CACHE_DIR": true,
	"PYTHONPYCACHEPREFIX": true, "PYTHONUSERBASE": true,
	"UV_CACHE_DIR": true, "UV_PYTHON_INSTALL_DIR": true, "UV_TOOL_DIR": true, "UV_TOOL_BIN_DIR": true,
	"XDG_CACHE_HOME": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_STATE_HOME": true,
	"LO_BUILD_ROOT": true, "LO_TEST_SESSION_DIRECTORY": true,
}

func validateJobPaths(root, work string, entry jobSpec) error {
	if entry.Cwd != "" {
		if _, err := localPath(root, strings.ReplaceAll(entry.Cwd, "{work}", work)); err != nil {
			return fmt.Errorf("group %s working directory: %w", entry.ID, err)
		}
	}
	for key, value := range entry.Environment {
		if !stateVariables[key] {
			continue
		}
		value = strings.ReplaceAll(value, "{work}", work)
		if !filepath.IsAbs(value) {
			return fmt.Errorf("group %s %s must name an absolute repository-local state path", entry.ID, key)
		}
		if _, err := localPath(root, value); err != nil {
			return fmt.Errorf("group %s %s state path: %w", entry.ID, key, err)
		}
	}
	for _, path := range entry.Release {
		if _, err := ownedReleasePath(root, work, path); err != nil {
			return fmt.Errorf("group %s release path: %w", entry.ID, err)
		}
	}
	return nil
}

func ownedReleasePath(root, work, path string) (string, error) {
	selected, err := localPath(root, strings.ReplaceAll(path, "{work}", work))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(work, selected)
	if err != nil || !filepath.IsLocal(relative) || relative == "." {
		return "", fmt.Errorf("release must name disposable state strictly inside the current session: %s", path)
	}
	return selected, nil
}

func localPath(root, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	existing := path
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", errors.New("path has no existing ancestor")
		}
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	if resolved != existing {
		return "", fmt.Errorf("session path traverses a symlink: %s", path)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(relative) {
		return "", fmt.Errorf("session path leaves Lo: %s", path)
	}
	return path, nil
}

func environment(base []string, changes map[string]string, work string) []string {
	values := map[string]string{}
	for _, item := range base {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	for key, value := range changes {
		values[key] = strings.ReplaceAll(value, "{work}", work)
	}
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}

func sessionEnvironment(root, work string) ([]string, error) {
	state := filepath.Join(root, "_scratch/build-state")
	changes := map[string]string{"CI": "1", "LO_TEST_SESSION_DIRECTORY": work, "LO_TEST_COMPILER_KIND": "development", "LO_TEST_COMPILER": filepath.Join(work, "bin/lo"), "LO_TEST_FILEBENCH": filepath.Join(work, "bin/filebench"), "LO_TEST_PERFORMANCE_RUNNER": filepath.Join(work, "bin/performance-runner"), "LO_BUILD_ROOT": state, "TMPDIR": filepath.Join(work, "tmp"), "TMP": filepath.Join(work, "tmp"), "TEMP": filepath.Join(work, "tmp"), "GOCACHE": filepath.Join(state, "go-build"), "GOMODCACHE": filepath.Join(state, "modules"), "GOPATH": filepath.Join(state, "gopath"), "GOENV": "off", "GOWORK": "off", "GOTELEMETRY": "off", "GOTOOLCHAIN": "local", "PYTHONDONTWRITEBYTECODE": "1", "PYTHONPYCACHEPREFIX": filepath.Join(work, "python-cache"), "UV_CACHE_DIR": filepath.Join(state, "uv-cache"), "UV_PYTHON_DOWNLOADS": "never", "UV_NO_CONFIG": "1", "CARGO_HOME": filepath.Join(work, "cargo-home"), "CARGO_TARGET_DIR": filepath.Join(state, "runtime-test-target"), "RUSTUP_HOME": filepath.Join(work, "rustup-home")}
	changes["LO_DISTRIBUTION_BUILD_CACHE"] = filepath.Join(state, "distribution-go-build")
	changes["HOME"] = filepath.Join(work, "home")
	changes["LO_BUILD_ROOT"] = filepath.Join(work, "build")
	for _, name := range []string{"CACHE", "CONFIG", "DATA", "STATE"} {
		changes["XDG_"+name+"_HOME"] = filepath.Join(work, "xdg-"+strings.ToLower(name))
	}
	changes["CLANG_MODULE_CACHE_PATH"] = filepath.Join(work, "clang")
	changes["CCACHE_DIR"] = filepath.Join(work, "ccache")
	changes["PIP_CACHE_DIR"] = filepath.Join(work, "pip-cache")
	changes["UV_PYTHON_INSTALL_DIR"] = filepath.Join(work, "uv-python")
	changes["UV_TOOL_DIR"] = filepath.Join(work, "uv-tools")
	changes["UV_TOOL_BIN_DIR"] = filepath.Join(work, "uv-bin")
	changes["GOTMPDIR"] = filepath.Join(work, "tmp")
	changes["PYTHONUSERBASE"] = filepath.Join(work, "python-user")
	changes["PYTHONPATH"] = root
	changes["PYTHONHOME"] = ""
	changes["PYTHONNOUSERSITE"] = "1"
	changes["PYTEST_ADDOPTS"] = ""
	changes["PYTEST_PLUGINS"] = ""
	changes["PYTEST_DISABLE_PLUGIN_AUTOLOAD"] = "1"
	if os.Getenv("LO_NATIVE_PREPARATION") == "" {
		changes["LO_NATIVE_PREPARATION"] = filepath.Join(root, "_scratch/native-runtime")
	}
	if os.Getenv("LO_LLVM_TOOLCHAIN") == "" {
		changes["LO_LLVM_TOOLCHAIN"] = filepath.Join(root, "_scratch/native-runtime/distribution/toolchain.json")
	}
	for _, directory := range []string{"tmp", "bin", "logs", "workers", "home", "cargo-home", "rustup-home", "build"} {
		if err := os.MkdirAll(filepath.Join(work, directory), os.ModePerm); err != nil {
			return nil, err
		}
	}
	return environment(os.Environ(), changes, work), nil
}

func ownedCommand(ctx context.Context, argv []string) *exec.Cmd {
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = time.Second
	return command
}

func runJob(ctx context.Context, root, work string, base []string, entry jobSpec, output *reporter) (int, error) {
	output.event("group", entry.ID, "start")
	argv := append([]string{}, entry.Argv...)
	for i := range argv {
		argv[i] = strings.ReplaceAll(argv[i], "{work}", work)
	}
	if entry.Format == "go" {
		pkg := entry.Package
		if pkg == "" {
			pkg = entry.ID
		}
		argv = append([]string{"go", "tool", "test2json", "-t", "-p", pkg}, argv...)
	}
	command := ownedCommand(ctx, argv)
	command.Dir = root
	if entry.Cwd != "" {
		directory, err := localPath(root, strings.ReplaceAll(entry.Cwd, "{work}", work))
		if err != nil {
			return 0, err
		}
		command.Dir = directory
	}
	worker := output.workerDirectory(work)
	if err := os.MkdirAll(worker, os.ModePerm); err != nil {
		return 0, err
	}
	changes := map[string]string{"TMPDIR": worker, "TMP": worker, "TEMP": worker}
	for key, value := range entry.Environment {
		changes[key] = value
	}
	command.Env = environment(base, changes, work)
	pipe, err := command.StdoutPipe()
	if err != nil {
		return 0, err
	}
	diagnostics := newTailBuffer(64 << 10)
	command.Stderr = diagnostics
	if err = command.Start(); err != nil {
		return 0, fmt.Errorf("start %s: %w", entry.ID, err)
	}
	results := map[string]string{}
	skipEvidence := map[string]string{}
	runs := map[string]bool{}
	collected := map[string]bool{}
	var resultErr error
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for scanner.Scan() {
		line := scanner.Text()
		var event testEvent
		if entry.Format == "go" || entry.Format == "events" {
			if decodeErr := json.Unmarshal([]byte(line), &event); decodeErr != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("group %s malformed event: %w", entry.ID, decodeErr))
				continue
			}
		} else if strings.HasPrefix(line, "LO_TEST_EVENT:") {
			if decodeErr := json.Unmarshal([]byte(strings.TrimPrefix(line, "LO_TEST_EVENT:")), &event); decodeErr != nil {
				resultErr = errors.Join(resultErr, decodeErr)
				continue
			}
		} else if entry.Format == "rust" && strings.HasPrefix(line, "test ") {
			name, status, ok := strings.Cut(strings.TrimPrefix(line, "test "), " ... ")
			if !ok {
				diagnostics.Write([]byte(line + "\n"))
				continue
			}
			event.Test = name
			runs[name] = true
			switch status {
			case "ok":
				event.Action = "pass"
			case "FAILED":
				event.Action = "fail"
			case "ignored":
				event.Action = "skip"
			default:
				resultErr = errors.Join(resultErr, fmt.Errorf("unknown Rust result %q", line))
				continue
			}
		} else {
			diagnostics.Write([]byte(line + "\n"))
			continue
		}
		if event.Output != "" {
			diagnostics.Write([]byte(event.Output))
			output.marker(event.Output)
			skipEvidence[event.Test] += event.Output
		}
		if err := output.record(event); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("record semantic evidence: %w", err))
		}
		if event.Test == "" {
			continue
		}
		switch event.Action {
		case "collect":
			if collected[event.Test] {
				resultErr = errors.Join(resultErr, fmt.Errorf("duplicate collected case %s/%s", entry.ID, event.Test))
			}
			collected[event.Test] = true
		case "run":
			runs[event.Test] = true
		case "deselect":
			allowed := false
			for index, argument := range entry.Argv {
				if argument == "--deselect="+event.Test || argument == "--deselect" && index+1 < len(entry.Argv) && entry.Argv[index+1] == event.Test {
					allowed = true
				}
			}
			for _, expected := range entry.Expected {
				if pythonDefinition(event.Test) == expected {
					allowed = false
				}
			}
			if !allowed {
				resultErr = errors.Join(resultErr, fmt.Errorf("undeclared deselected case %s/%s", entry.ID, event.Test))
			}
		case "pass", "fail", "skip":
			if !runs[event.Test] {
				resultErr = errors.Join(resultErr, fmt.Errorf("case completed without execution start %s/%s", entry.ID, event.Test))
			}
			if _, exists := results[event.Test]; exists {
				resultErr = errors.Join(resultErr, fmt.Errorf("duplicate case result %s/%s", entry.ID, event.Test))
			}
			results[event.Test] = event.Action
			output.event("case", entry.ID+"/"+event.Test, event.Action)
		}
	}
	if err := scanner.Err(); err != nil {
		_ = command.Cancel()
		_ = pipe.Close()
	}
	waitErr := command.Wait()
	if command.Process != nil {
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			resultErr = errors.Join(resultErr, fmt.Errorf("clean owned process group %s: %w", entry.ID, err))
		}
	}
	if err = scanner.Err(); err != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("read group %s events: %w", entry.ID, err))
	}
	for name, status := range results {
		allowed := false
		if status == "skip" {
			for _, reason := range entry.AllowSkips[strings.Split(name, "/")[0]] {
				if strings.Contains(skipEvidence[name], ": "+reason+"\n") {
					allowed = true
					break
				}
			}
		}
		if status == "fail" || status == "skip" && !allowed {
			resultErr = errors.Join(resultErr, fmt.Errorf("case %s/%s: %s", entry.ID, name, status))
		}
	}
	if waitErr != nil {
		unfinished := make(map[string]bool)
		for _, name := range entry.Expected {
			unfinished[name] = true
		}
		for name := range collected {
			unfinished[name] = true
		}
		for name := range runs {
			unfinished[name] = true
		}
		for name := range results {
			delete(unfinished, name)
		}
		names := make([]string, 0, len(unfinished))
		for name := range unfinished {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			output.event("case", entry.ID+"/"+name, "aborted")
		}
	} else {
		for _, name := range entry.Expected {
			found := false
			if entry.Format == "pytest" {
				for collectedName := range collected {
					if pythonDefinition(collectedName) == name {
						found = true
						break
					}
				}
			} else {
				_, found = results[name]
			}
			if !found {
				resultErr = errors.Join(resultErr, fmt.Errorf("missing case result %s/%s", entry.ID, name))
			}
		}
		for name := range collected {
			if _, ok := results[name]; !ok {
				resultErr = errors.Join(resultErr, fmt.Errorf("missing collected case result %s/%s", entry.ID, name))
			}
		}
		if (entry.Format == "go" || entry.Format == "events" || entry.Format == "pytest" || entry.Format == "rust") && len(results) == 0 {
			resultErr = errors.Join(resultErr, fmt.Errorf("missing case results for %s", entry.ID))
		}
	}
	if entry.Format == "pytest" {
		for name := range results {
			if !collected[name] {
				resultErr = errors.Join(resultErr, fmt.Errorf("uncollected case result %s/%s", entry.ID, name))
			}
		}
	}
	if waitErr == nil {
		for name := range runs {
			if _, ok := results[name]; !ok {
				resultErr = errors.Join(resultErr, fmt.Errorf("incomplete case %s/%s", entry.ID, name))
			}
		}
	}
	if waitErr != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("command %s: %s: %w\n%s", entry.ID, strings.Join(argv, " "), waitErr, diagnostics.String()))
	}
	if resultErr != nil {
		output.event("group", entry.ID, "fail")
		return 0, resultErr
	}
	for _, path := range entry.Release {
		selected, err := ownedReleasePath(root, work, path)
		if err != nil {
			return 0, fmt.Errorf("release successful group %s output: %w", entry.ID, err)
		}
		if err := os.RemoveAll(selected); err != nil {
			return 0, fmt.Errorf("release successful group %s output %s: %w", entry.ID, selected, err)
		}
	}
	if err = os.RemoveAll(worker); err != nil {
		return 0, fmt.Errorf("remove worker %s: %w", entry.ID, err)
	}
	output.event("group", entry.ID, "pass")
	count := len(entry.Expected)
	if count == 0 || entry.Format == "pytest" {
		count = len(results)
	}
	return max(1, count), nil
}

func runGroups(parent context.Context, root, work string, base []string, groups []jobSpec, workers, guests int, output *reporter) (int, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	type outcome struct {
		entry     jobSpec
		completed int
		err       error
	}
	done := make(chan outcome, len(groups))
	pending := append([]jobSpec{}, groups...)
	passed := make(map[string]bool, len(groups))
	active, activeGuests, activeHTTP, completed := 0, 0, 0, 0
	var failure error
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for len(pending) > 0 || active > 0 {
		if ctx.Err() != nil {
			for _, entry := range pending {
				output.event("group", entry.ID, "aborted")
			}
			pending = nil
			if active == 0 {
				return completed, errors.Join(failure, ctx.Err())
			}
		}
		for ctx.Err() == nil && active < workers && len(pending) > 0 {
			chosen := -1
			for index, entry := range pending {
				ready := true
				for _, dependency := range entry.After {
					if !passed[dependency] {
						ready = false
						break
					}
				}
				if !ready {
					continue
				}
				if entry.Resource == resourceGuest && activeGuests >= guests || entry.Resource == resourceHostHTTP && activeHTTP >= 1 {
					continue
				}
				chosen = index
				break
			}
			if chosen < 0 {
				break
			}
			entry := pending[chosen]
			pending = append(pending[:chosen], pending[chosen+1:]...)
			active++
			if entry.Resource == resourceGuest {
				activeGuests++
			}
			if entry.Resource == resourceHostHTTP {
				activeHTTP++
			}
			go func(entry jobSpec) {
				count, err := runJob(ctx, root, work, base, entry, output)
				done <- outcome{entry, count, err}
			}(entry)
		}
		if active == 0 {
			if len(pending) != 0 {
				return completed, errors.Join(failure, errors.New("group dependency schedule cannot make progress"))
			}
			break
		}
		select {
		case result := <-done:
			active--
			if result.entry.Resource == resourceGuest {
				activeGuests--
			}
			if result.entry.Resource == resourceHostHTTP {
				activeHTTP--
			}
			completed += result.completed
			if result.err != nil {
				failure = errors.Join(failure, result.err)
				cancel()
			} else {
				passed[result.entry.ID] = true
			}
		case <-ticker.C:
			if err := output.storage.check(); err != nil {
				failure = errors.Join(failure, err)
				cancel()
			}
		}
	}
	return completed, failure
}

type tailBuffer struct {
	sync.Mutex
	data  []byte
	limit int
}

func newTailBuffer(limit int) *tailBuffer { return &tailBuffer{limit: limit} }
func (b *tailBuffer) Write(data []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	count := len(data)
	if len(data) >= b.limit {
		b.data = append(b.data[:0], data[len(data)-b.limit:]...)
	} else {
		b.data = append(b.data, data...)
		if len(b.data) > b.limit {
			b.data = b.data[len(b.data)-b.limit:]
		}
	}
	return count, nil
}
func (b *tailBuffer) String() string { b.Lock(); defer b.Unlock(); return string(b.data) }
func safeName(name string) string {
	return strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_").Replace(name)
}

func workerName(name string) string {
	digest := sha256.Sum256([]byte(name))
	return fmt.Sprintf("%s-%x", safeName(name), digest[:8])
}

func pythonDefinition(name string) string {
	parts := strings.Split(name, "::")
	for index := 1; index < len(parts); index++ {
		parts[index] = strings.SplitN(parts[index], "[", 2)[0]
	}
	return strings.Join(parts, "::")
}

func storageAvailable(path string, reserve int64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return fmt.Errorf("measure session storage %s: %w", path, err)
	}
	available := uint64(stat.Bavail) * uint64(stat.Bsize)
	if available < uint64(reserve) {
		return fmt.Errorf("session storage at %s: need %d bytes, available %d", path, reserve, available)
	}
	return nil
}
