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
)

const (
	reportName     = "report.json"
	maxSamples     = 20
	maxEntries     = 10_001
	maxPayload     = (16 << 20) + 1
	maxDirectories = 129
	stepBudget     = "100000000"
	maxCapture     = 8 << 20
)

var knownCases = []string{"startup", "list", "scan", "remove", "checked-remove", "read", "write", "append", "append-many", "directories", "open-paths"}

type configuration struct {
	work, compiler, toolchain, cases, sizes, ioSizes, directories, label string
	samples, appendBytes                                                 int
	timeout                                                              time.Duration
}

type settings struct {
	Cases           []string `json:"cases"`
	EntrySizes      []int    `json:"entry_sizes"`
	PayloadSizes    []int    `json:"payload_sizes"`
	DirectoryCounts []int    `json:"directory_counts"`
	Samples         int      `json:"samples"`
	AppendBytes     int      `json:"append_bytes"`
	MaxSteps        string   `json:"max_steps"`
	ChildTimeoutNS  int64    `json:"child_timeout_ns"`
}

type sample struct {
	Case          string `json:"case"`
	Size          int    `json:"size"`
	Index         int    `json:"sample"`
	SourceSHA     string `json:"source_sha256"`
	TemplateSHA   string `json:"template_sha256"`
	ProcessWallNS int64  `json:"process_wall_ns"`
	PeakRSS       uint64 `json:"peak_rss_bytes"`
	Status        int    `json:"exit_status"`
	Expected      string `json:"expected"`
	Verified      bool   `json:"verified"`
	Pipeline      string `json:"pipeline"`
	Error         string `json:"error,omitempty"`
}

type summary struct {
	Case        string `json:"case"`
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
	flags.StringVar(&c.cases, "cases", "startup,list,scan,remove,checked-remove,read,write,append,append-many,directories", "comma-separated workloads")
	flags.StringVar(&c.sizes, "sizes", "100,1000,5000", "entry or append-commit counts; at most 10001")
	flags.StringVar(&c.ioSizes, "io-sizes", "4096,65536,1048576", "payload bytes; at most 16777217")
	flags.StringVar(&c.directories, "directory-counts", "1,32,128,129", "sequential directory observations; at most 129")
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

func toolPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absolute)
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
	c.compiler, err = toolPath(c.compiler)
	if err != nil {
		return err
	}
	{
		c.toolchain, err = toolPath(c.toolchain)
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
	directoryCounts, err := numbers(c.directories, maxDirectories)
	if err != nil {
		return err
	}
	run, err := os.MkdirTemp(work, "filebench-")
	if err != nil {
		return err
	}
	r := report{Label: c.label, Work: run, Status: "running", Environment: map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version(), "logical_cpus": strconv.Itoa(runtime.NumCPU())}, Methods: map[string]string{
		"process_wall_ns":    "fresh public CLI process including startup, runtime admission, evaluation, cleanup, and scalar JSON result; native compilation excluded",
		"peak_rss_bytes":     "per-child getrusage maximum resident set; macOS bytes, Linux KiB converted to bytes; includes CLI admission and startup",
		"native_allocations": "not collected by generic CLI runner; commit-scoped native allocator regression supplies separate removal allocation evidence",
		"cache":              "freshly prepared fixtures before each invocation; filesystem cache uncontrolled and normally warm after fixture writes; no global cache purge; first sample retained; no subtraction or discarded samples",
		"bounds":             "default host resource quotas; sequential directory observations release after each callback; fixed read/write/append pipelines model distinct live values and their headers; child stdout/stderr capped at 8 MiB each",
		"summary":            "median uses the midpoint for even sample counts; p95 uses nearest rank; all samples retained",
	}, Identities: map[string]string{}}
	inputs := map[string]string{"compiler_sha256": c.compiler}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, err = toolPath(self)
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
	{
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
		archive, err := toolPath(filepath.Join(filepath.Dir(c.toolchain), target.Runtime.Path))
		if err != nil {
			return err
		}
		r.Identities["runtime_sha256"], err = digest(archive)
		if err != nil {
			return err
		}
		inputs["runtime_sha256"] = archive
	}
	r.Requested = settings{Cases: strings.Split(c.cases, ","), EntrySizes: entrySizes, PayloadSizes: ioSizes, DirectoryCounts: directoryCounts, Samples: c.samples, AppendBytes: c.appendBytes, MaxSteps: stepBudget, ChildTimeoutNS: c.timeout.Nanoseconds()}
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
			{
				if err := measure(c, root, run, name, size, &r); err != nil {
					r.Status, r.Error = "failed", fmt.Sprintf("%s/%d: %v", name, size, err)
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

func measure(c configuration, root, run, name string, size int, r *report) (resultErr error) {
	directory, err := os.MkdirTemp(run, fmt.Sprintf("%s-%d-", name, size))
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
	executable := filepath.Join(directory, "program")
	arguments := append([]string{"run"}, w.args...)
	{
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
		s := sample{Case: name, Size: size, Index: index, SourceSHA: sourceHash, TemplateSHA: hex.EncodeToString(templateHash[:]), ProcessWallNS: elapsed, PeakRSS: rss, Status: status, Expected: w.expected, Pipeline: w.pipeline}
		if err == nil {
			err = w.verify(out, errOut, status)
		}
		s.Verified = err == nil
		if err != nil {
			s.Error = err.Error()
		}
		r.Samples = append(r.Samples, s)
		if err != nil {
			return fmt.Errorf("verify %s/%d sample %d: %w", name, size, index, err)
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
	r.Summary = append(r.Summary, summary{Case: name, Size: size, Samples: len(wall), MedianNS: (wall[len(wall)/2] + wall[(len(wall)-1)/2]) / 2, P95NS: wall[int(math.Ceil(float64(len(wall))*0.95))-1], MaxRSSBytes: maxRSS})
	return nil
}
