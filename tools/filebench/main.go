// Command filebench measures bounded File workloads through public Lo runners.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

const (
	reportName = "report.json"
	maxSamples = 20
	maxEntries = execution.MaxFileEntries + 1
	maxPayload = execution.MaxBytes + 1
	stepBudget = "100000000"
	maxCapture = 8 << 20
)

var knownCases = []string{"startup", "list", "scan", "remove", "checked-remove", "read", "write", "append", "append-many", "directories", "open-paths"}

type configuration struct {
	work, compiler, toolchain, backend, cases, sizes, ioSizes, directories, label string
	samples, appendBytes                                                          int
	timeout                                                                       time.Duration
}

type allocation struct {
	Count            uint64 `json:"count"`
	Bytes            uint64 `json:"bytes"`
	RunNS            int64  `json:"runner_wall_ns"`
	WorkerSHA        string `json:"worker_executable_sha256"`
	RuntimeSourceSHA string `json:"worker_runtime_source_sha256"`
}

type settings struct {
	Cases           []string `json:"cases"`
	Backends        []string `json:"backends"`
	EntrySizes      []int    `json:"entry_sizes"`
	PayloadSizes    []int    `json:"payload_sizes"`
	DirectoryCounts []int    `json:"directory_counts"`
	Samples         int      `json:"samples"`
	AppendBytes     int      `json:"append_bytes"`
	MaxSteps        string   `json:"max_steps"`
	ChildTimeoutNS  int64    `json:"child_timeout_ns"`
}

type sample struct {
	Case          string      `json:"case"`
	Backend       string      `json:"backend"`
	Size          int         `json:"size"`
	Index         int         `json:"sample"`
	SourceSHA     string      `json:"source_sha256"`
	TemplateSHA   string      `json:"template_sha256"`
	ProcessWallNS int64       `json:"process_wall_ns"`
	PeakRSS       uint64      `json:"peak_rss_bytes"`
	Status        int         `json:"exit_status"`
	Expected      string      `json:"expected"`
	Verified      bool        `json:"verified"`
	Allocations   *allocation `json:"allocations"`
	Pipeline      string      `json:"pipeline"`
	Error         string      `json:"error,omitempty"`
}

type summary struct {
	Case        string `json:"case"`
	Backend     string `json:"backend"`
	Size        int    `json:"size"`
	Samples     int    `json:"samples"`
	MedianNS    int64  `json:"median_process_wall_ns"`
	P95NS       int64  `json:"p95_process_wall_ns"`
	MaxRSSBytes uint64 `json:"max_peak_rss_bytes"`
}

type report struct {
	Label       string            `json:"label"`
	Work        string            `json:"work"`
	Environment map[string]string `json:"environment"`
	Methods     map[string]string `json:"methods"`
	Identities  map[string]string `json:"identities"`
	Samples     []sample          `json:"samples"`
	Summary     []summary         `json:"summary"`
	Status      string            `json:"status"`
	Error       string            `json:"error,omitempty"`
	Requested   settings          `json:"requested"`
}

type boundedCapture struct {
	buffer   bytes.Buffer
	cancel   context.CancelFunc
	overflow bool
}

func (capture *boundedCapture) Write(data []byte) (int, error) {
	if len(data) > maxCapture-capture.buffer.Len() {
		capture.overflow = true
		capture.cancel()
		return 0, fmt.Errorf("benchmark output exceeds %d bytes", maxCapture)
	}
	return capture.buffer.Write(data)
}

func (capture *boundedCapture) Bytes() []byte { return capture.buffer.Bytes() }

func main() {
	if adapter.HandleFileProbe(os.Args[1:]) {
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "worker" {
		if err := runWorker(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := execute(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parse(args []string) (configuration, error) {
	var c configuration
	flags := flag.NewFlagSet("filebench", flag.ContinueOnError)
	flags.StringVar(&c.work, "work", "", "existing owned directory inside Lo/_scratch")
	flags.StringVar(&c.compiler, "compiler", "bin/lo", "compiler inside Lo")
	flags.StringVar(&c.toolchain, "toolchain", "", "native toolchain manifest inside Lo")
	flags.StringVar(&c.backend, "backend", "all", "all, interpreter, or native")
	flags.StringVar(&c.cases, "cases", "startup,list,scan,remove,checked-remove,read,write,append,append-many,directories", "comma-separated workloads")
	flags.StringVar(&c.sizes, "sizes", "100,1000,5000", "entry or append-commit counts; at most 10001")
	flags.StringVar(&c.ioSizes, "io-sizes", "4096,65536,1048576", "payload bytes; at most 16777217")
	flags.StringVar(&c.directories, "directory-counts", "1,32,128,129", "retained directory counts; at most 129")
	flags.StringVar(&c.label, "label", "measurement", "report phase label")
	flags.IntVar(&c.samples, "samples", 3, "fresh verified samples per workload")
	flags.IntVar(&c.appendBytes, "append-bytes", 64, "bytes per append-many commit")
	flags.DurationVar(&c.timeout, "timeout", 2*time.Minute, "maximum duration of each child process")
	if err := flags.Parse(args); err != nil {
		return c, err
	}
	if flags.NArg() != 0 {
		return c, fmt.Errorf("unexpected arguments")
	}
	seen := map[string]bool{}
	for _, name := range strings.Split(c.cases, ",") {
		if !slices.Contains(knownCases, name) {
			return c, fmt.Errorf("unknown case %q", name)
		}
		if seen[name] {
			return c, fmt.Errorf("duplicate case %q", name)
		}
		seen[name] = true
	}
	if c.backend != "all" && c.backend != "interpreter" && c.backend != "native" {
		return c, fmt.Errorf("unknown backend %q", c.backend)
	}
	if c.samples < 1 || c.samples > maxSamples || c.appendBytes < 1 || c.appendBytes > 4096 || c.timeout <= 0 || c.timeout > 10*time.Minute {
		return c, fmt.Errorf("sample, append, or timeout bound exceeded")
	}
	return c, nil
}

func rootDirectory() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		data, err := os.ReadFile(filepath.Join(current, "go.mod"))
		if err == nil && strings.HasPrefix(string(data), "module github.com/MarcoPoloResearchLab/Lo\n") {
			return filepath.EvalSymlinks(current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("filebench requires the Lo checkout")
		}
		current = parent
	}
}

func contained(root, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	relative, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil || !filepath.IsLocal(relative) || relative == "." {
		return "", fmt.Errorf("path must stay inside Lo: %q", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}
	relative, err = filepath.Rel(root, resolved)
	if err != nil || !filepath.IsLocal(relative) || relative == "." {
		return "", fmt.Errorf("path must stay inside Lo: %q", path)
	}
	return resolved, nil
}

func digest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, readErr := io.Copy(hash, file)
	if err := errors.Join(readErr, file.Close()); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func numbers(text string, maximum int) ([]int, error) {
	var values []int
	for _, part := range strings.Split(text, ",") {
		value, err := strconv.Atoi(part)
		if err != nil || value < 1 || value > maximum || slices.Contains(values, value) {
			return nil, fmt.Errorf("sizes require unique positive integers at most %d", maximum)
		}
		values = append(values, value)
	}
	return values, nil
}

func write(path string, data []byte) (resultErr error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".filebench-write-")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, os.Remove(file.Name())) }()
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Link(file.Name(), path)
}

func child(ctx context.Context, directory, executable string, args ...string) ([]byte, []byte, int, int64, uint64, error) {
	owned, cancel := context.WithCancel(ctx)
	defer cancel()
	command := exec.CommandContext(owned, executable, args...)
	command.Dir = directory
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = time.Second
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	stdout, stderr := boundedCapture{cancel: cancel}, boundedCapture{cancel: cancel}
	command.Stdout, command.Stderr = &stdout, &stderr
	start := time.Now()
	err := command.Run()
	elapsed := time.Since(start).Nanoseconds()
	if stdout.overflow || stderr.overflow {
		return nil, nil, -1, elapsed, 0, fmt.Errorf("benchmark output exceeds %d bytes", maxCapture)
	}
	if ctx.Err() != nil {
		return nil, nil, -1, elapsed, 0, ctx.Err()
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return nil, nil, 0, elapsed, 0, err
	}
	usage, ok := command.ProcessState.SysUsage().(*syscall.Rusage)
	if !ok {
		return nil, nil, 0, elapsed, 0, fmt.Errorf("per-child resource usage unavailable")
	}
	rss := uint64(usage.Maxrss)
	if runtime.GOOS == "linux" {
		rss *= 1024
	}
	return stdout.Bytes(), stderr.Bytes(), command.ProcessState.ExitCode(), elapsed, rss, nil
}

func execute(args []string, stdout io.Writer) error {
	c, err := parse(args)
	if err != nil {
		return err
	}
	root, err := rootDirectory()
	if err != nil {
		return err
	}
	work, err := contained(root, c.work)
	if err != nil {
		return err
	}
	if relative, err := filepath.Rel(filepath.Join(root, "_scratch"), work); err != nil || !filepath.IsLocal(relative) {
		return fmt.Errorf("work must stay inside Lo/_scratch")
	}
	c.compiler, err = contained(root, c.compiler)
	if err != nil {
		return err
	}
	if c.backend != "interpreter" {
		c.toolchain, err = contained(root, c.toolchain)
		if err != nil {
			return err
		}
	}
	entrySizes, err := numbers(c.sizes, maxEntries)
	if err != nil {
		return err
	}
	ioSizes, err := numbers(c.ioSizes, maxPayload)
	if err != nil {
		return err
	}
	directoryCounts, err := numbers(c.directories, execution.MaxFileDirectories+1)
	if err != nil {
		return err
	}
	run, err := os.MkdirTemp(work, "filebench-")
	if err != nil {
		return err
	}
	r := report{Label: c.label, Work: run, Status: "running", Environment: map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version(), "logical_cpus": strconv.Itoa(runtime.NumCPU())}, Methods: map[string]string{
		"process_wall_ns":         "fresh public CLI process including startup, runtime admission, evaluation, cleanup, and scalar JSON result; native compilation excluded; interpreter CLI includes frontend compilation",
		"peak_rss_bytes":          "per-child getrusage maximum resident set; macOS bytes, Linux KiB converted to bytes; includes CLI admission and startup",
		"interpreter_allocations": "separate fresh worker linked from current checkout; compile before observation; runtime.ReadMemStats deltas across runner.Execute including host setup, execution, cleanup, and scalar serialization; worker executable/runtime-source hashes identify its implementation, which may differ from configured CLI compiler; no cross-implementation allocator ratio is claimed; worker result and filesystem independently verified",
		"native_allocations":      "not collected by generic CLI runner; commit-scoped native allocator regression supplies separate removal allocation evidence",
		"cache":                   "freshly prepared fixtures before each invocation; filesystem cache uncontrolled and normally warm after fixture writes; no global cache purge; first sample retained; no subtraction or discarded samples",
		"bounds":                  "current Lo resource limits retained; directory and entry overflow require runtime.limit; fixed read/write/append pipelines model ordered value charges, including path/target/receipt headers and cumulative budget; child stdout/stderr capped at 8 MiB each",
		"summary":                 "median uses the midpoint for even sample counts; p95 uses nearest rank; all samples retained",
	}, Identities: map[string]string{}}
	inputs := map[string]string{"compiler_sha256": c.compiler}
	runtimeIdentity, runtimeInputs, err := runtimeSourceIdentity(root)
	if err != nil {
		return err
	}
	r.Identities["worker_runtime_source_sha256"] = runtimeIdentity
	for key, path := range runtimeInputs {
		inputs[key] = path
		r.Identities[key], err = digest(path)
		if err != nil {
			return err
		}
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, err = contained(root, self)
	if err != nil {
		return err
	}
	r.Identities["producer_executable_sha256"], err = digest(self)
	if err != nil {
		return err
	}
	inputs["producer_executable_sha256"] = self
	for _, name := range []string{"main.go", "workloads.go"} {
		path := filepath.Join(root, "tools/filebench", name)
		key := "producer_" + name + "_sha256"
		r.Identities[key], err = digest(path)
		if err != nil {
			return err
		}
		inputs[key] = path
	}
	r.Identities["compiler_sha256"], err = digest(c.compiler)
	if err != nil {
		return err
	}
	if c.backend != "interpreter" {
		r.Identities["toolchain_sha256"], err = digest(c.toolchain)
		if err != nil {
			return err
		}
		inputs["toolchain_sha256"] = c.toolchain
		var manifest struct {
			Targets map[string]struct{ Runtime struct{ Path string } }
		}
		data, err := os.ReadFile(c.toolchain)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			return err
		}
		target, ok := manifest.Targets[runtime.GOOS+"/"+runtime.GOARCH]
		if !ok {
			return fmt.Errorf("toolchain lacks host target")
		}
		archive, err := contained(root, filepath.Join(filepath.Dir(c.toolchain), target.Runtime.Path))
		if err != nil {
			return err
		}
		r.Identities["runtime_sha256"], err = digest(archive)
		if err != nil {
			return err
		}
		inputs["runtime_sha256"] = archive
	}
	backends := []string{"interpreter", "native"}
	if c.backend != "all" {
		backends = []string{c.backend}
	}
	r.Requested = settings{Cases: strings.Split(c.cases, ","), Backends: backends, EntrySizes: entrySizes, PayloadSizes: ioSizes, DirectoryCounts: directoryCounts, Samples: c.samples, AppendBytes: c.appendBytes, MaxSteps: stepBudget, ChildTimeoutNS: c.timeout.Nanoseconds()}
	for _, name := range strings.Split(c.cases, ",") {
		sizes := entrySizes
		switch name {
		case "startup", "open-paths":
			sizes = []int{1}
		case "read", "write", "append":
			sizes = ioSizes
		case "directories":
			sizes = directoryCounts
		}
		for _, size := range sizes {
			for _, backend := range backends {
				if err := measure(c, root, run, name, size, backend, &r); err != nil {
					r.Status, r.Error = "failed", fmt.Sprintf("%s/%d/%s: %v", name, size, backend, err)
					if reportErr := saveReport(run, r, stdout); reportErr != nil {
						return errors.Join(err, reportErr)
					}
					return err
				}
			}
		}
	}
	for key, path := range inputs {
		observed, err := digest(path)
		if err != nil {
			r.Status, r.Error = "invalid", err.Error()
			return errors.Join(err, saveReport(run, r, stdout))
		}
		if observed != r.Identities[key] {
			err := fmt.Errorf("benchmark input changed during execution: %s", path)
			r.Status, r.Error = "invalid", err.Error()
			return errors.Join(err, saveReport(run, r, stdout))
		}
	}
	r.Status = "complete"
	return saveReport(run, r, stdout)
}

func saveReport(directory string, r report, stdout io.Writer) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := write(filepath.Join(directory, reportName), data); err != nil {
		return err
	}
	_, err = stdout.Write(data)
	return err
}

func measure(c configuration, root, run, name string, size int, backend string, r *report) (resultErr error) {
	directory, err := os.MkdirTemp(run, fmt.Sprintf("%s-%d-%s-", name, size, backend))
	if err != nil {
		return err
	}
	w, err := newWorkload(directory, name, size, c.appendBytes)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, w.close()) }()
	sourcePath := filepath.Join(directory, "program.lo")
	if err := write(sourcePath, []byte(w.source)); err != nil {
		return err
	}
	sourceHash, err := digest(sourcePath)
	if err != nil {
		return err
	}
	templateHash := sha256.Sum256([]byte(w.canonicalSource()))
	executable, arguments := c.compiler, append([]string{"run", sourcePath}, w.args...)
	if backend == "native" {
		executable = filepath.Join(directory, "program")
		ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
		out, errOut, status, _, _, err := child(ctx, root, c.compiler, "build", sourcePath, "--toolchain", c.toolchain, "--output", executable, "--format", "json")
		cancel()
		if err != nil || status != 0 {
			return fmt.Errorf("build %s/%d: %v status=%d stdout=%s stderr=%s", name, size, err, status, out, errOut)
		}
		arguments = append([]string{"run"}, w.args...)
	}
	var wall []int64
	var maxRSS uint64
	for index := range c.samples {
		if err := w.prepare(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
		out, errOut, status, elapsed, rss, err := child(ctx, root, executable, arguments...)
		cancel()
		s := sample{Case: name, Backend: backend, Size: size, Index: index, SourceSHA: sourceHash, TemplateSHA: hex.EncodeToString(templateHash[:]), ProcessWallNS: elapsed, PeakRSS: rss, Status: status, Expected: w.expected, Pipeline: w.pipeline}
		if err == nil {
			err = w.verify(out, errOut, status)
		}
		if err == nil && backend == "interpreter" {
			s.Allocations, err = measureAllocations(c, root, directory, sourcePath, index, status, w)
		}
		s.Verified = err == nil
		if err != nil {
			s.Error = err.Error()
		}
		r.Samples = append(r.Samples, s)
		if err != nil {
			return fmt.Errorf("verify %s/%d/%s sample %d: %w", name, size, backend, index, err)
		}
		wall = append(wall, elapsed)
		maxRSS = max(maxRSS, rss)
	}
	slices.Sort(wall)
	finalSource, err := digest(sourcePath)
	if err != nil {
		return err
	}
	if finalSource != sourceHash {
		return fmt.Errorf("workload source changed during execution: %s", sourcePath)
	}
	r.Summary = append(r.Summary, summary{Case: name, Backend: backend, Size: size, Samples: len(wall), MedianNS: (wall[len(wall)/2] + wall[(len(wall)-1)/2]) / 2, P95NS: wall[int(math.Ceil(float64(len(wall))*0.95))-1], MaxRSSBytes: maxRSS})
	return nil
}

func measureAllocations(c configuration, root, directory, sourcePath string, index, status int, w *workload) (*allocation, error) {
	if err := w.prepare(); err != nil {
		return nil, fmt.Errorf("prepare allocation worker fixture: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate allocation worker: %w", err)
	}
	statsPath := filepath.Join(directory, fmt.Sprintf("allocation-%d.json", index))
	workerArgs := append([]string{"worker", sourcePath, statsPath}, append([]string{"run"}, w.args...)...)
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	workerOut, workerErr, workerStatus, _, _, err := child(ctx, root, self, workerArgs...)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("run allocation worker: %w", err)
	}
	data, err := os.ReadFile(statsPath)
	if err != nil {
		return nil, fmt.Errorf("read worker allocation metrics: %w; stderr=%s", err, workerErr)
	}
	var stats struct {
		Allocation allocation `json:"allocation"`
		Status     int        `json:"status"`
	}
	if err := json.Unmarshal(data, &stats); err != nil {
		return nil, fmt.Errorf("decode worker allocation metrics: %w", err)
	}
	if workerStatus != 0 || stats.Status != status {
		return nil, fmt.Errorf("allocation worker status=%d execution=%d expected=%d stderr=%s", workerStatus, stats.Status, status, workerErr)
	}
	if err := w.verify(workerOut, workerErr, stats.Status); err != nil {
		return nil, fmt.Errorf("verify allocation worker: %w", err)
	}
	return &stats.Allocation, nil
}

func runWorker(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("worker requires source, metrics path, and runtime arguments")
	}
	root, err := rootDirectory()
	if err != nil {
		return err
	}
	sourcePath, err := contained(root, args[0])
	if err != nil {
		return err
	}
	statsDirectory, err := contained(root, filepath.Dir(args[1]))
	if err != nil {
		return fmt.Errorf("worker metrics directory must stay inside Lo/_scratch: %w", err)
	}
	if relative, err := filepath.Rel(filepath.Join(root, "_scratch"), statsDirectory); err != nil || !filepath.IsLocal(relative) {
		return fmt.Errorf("worker metrics directory must stay inside Lo/_scratch")
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	input, err := source.New(sourcePath, string(data))
	if err != nil {
		return err
	}
	checked, problem := compiler.Check(input)
	if problem != nil {
		return problem
	}
	program, err := eval.Program(checked)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	workerSHA, err := digest(self)
	if err != nil {
		return err
	}
	runtimeSHA, _, err := runtimeSourceIdentity(root)
	if err != nil {
		return err
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	status := runner.Execute(program, args[2:], os.Stdout, os.Stderr)
	elapsed := time.Since(start).Nanoseconds()
	runtime.ReadMemStats(&after)
	stats := struct {
		Allocation allocation `json:"allocation"`
		Status     int        `json:"status"`
	}{allocation{Count: after.Mallocs - before.Mallocs, Bytes: after.TotalAlloc - before.TotalAlloc, RunNS: elapsed, WorkerSHA: workerSHA, RuntimeSourceSHA: runtimeSHA}, status}
	encoded, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	return write(filepath.Join(statsDirectory, filepath.Base(args[1])), encoded)
}

func runtimeSourceIdentity(root string) (string, map[string]string, error) {
	inputs := map[string]string{"go.mod_sha256": filepath.Join(root, "go.mod"), "go.sum_sha256": filepath.Join(root, "go.sum")}
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		resolved, err := contained(root, path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, resolved)
		if err != nil {
			return err
		}
		inputs[filepath.ToSlash(relative)] = resolved
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	keys := make([]string, 0, len(inputs))
	for key := range inputs {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	hash := sha256.New()
	for _, key := range keys {
		value, err := digest(inputs[key])
		if err != nil {
			return "", nil, err
		}
		if _, err := fmt.Fprintf(hash, "%s\x00%s\x00", key, value); err != nil {
			return "", nil, err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), inputs, nil
}
