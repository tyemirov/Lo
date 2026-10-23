// Command performance-runner measures declared fixtures without a development toolchain.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const warmup = 3
const streamLimit = 1 << 20
const buildTimeout = 180 * time.Second
const steps = "10000000"

var benchmarkPattern = regexp.MustCompile(`BenchmarkExecution\s+\d+\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op`)

type fixture struct {
	Name       string `json:"name"`
	Source     string `json:"source"`
	Expected   string `json:"expected"`
	SourceSHA  string `json:"source_sha256"`
	Reference  string `json:"reference"`
	Native     string `json:"native"`
	Allocation string `json:"allocation"`
	Artifact   string `json:"artifact"`
	Work       string `json:"work"`
}
type config struct {
	SamplePlan map[string]int `json:"sample_plan"`
	Warmup     int            `json:"warmup"`
	Compiler   string         `json:"compiler"`
	Output     string         `json:"output"`
	Execution  []fixture      `json:"execution"`
	Process    []fixture      `json:"process"`
	HTTP       []fixture      `json:"http"`
	Build      []fixture      `json:"build"`
}
type observation struct {
	Status  int
	Output  []byte
	Elapsed int64
	Peak    uint64
}
type limitedBuffer struct {
	sync.Mutex
	bytes.Buffer
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	if b.Len()+len(data) > streamLimit {
		return 0, fmt.Errorf("diagnostics exceed %d bytes", streamLimit)
	}
	return b.Buffer.Write(data)
}
func absolute(path string) bool { return filepath.IsAbs(path) && filepath.Clean(path) == path }
func readConfig(path string) (config, error) {
	var c config
	file, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&c); err != nil {
		return c, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return c, fmt.Errorf("configuration must contain one JSON object")
	}
	if c.Warmup != warmup || !absolute(c.Compiler) || !absolute(c.Output) {
		return c, fmt.Errorf("configuration requires at least ten pairs, three warmups, and absolute compiler/output paths")
	}
	plan := map[string]int{"execution": 10, "process": 10, "http": 100, "build": 10}
	if len(c.SamplePlan) != len(plan) {
		return c, fmt.Errorf("configuration requires the complete canonical sample_plan")
	}
	for lane, count := range plan {
		if c.SamplePlan[lane] != count {
			return c, fmt.Errorf("configuration sample_plan %s requires %d pairs", lane, count)
		}
	}
	names := map[string]bool{}
	for lane, fixtures := range map[string][]fixture{"execution": c.Execution, "process": c.Process, "http": c.HTTP, "build": c.Build} {
		for _, f := range fixtures {
			if f.Name == "" || names[lane+"/"+f.Name] || !absolute(f.Source) || !absolute(f.Work) || len(f.SourceSHA) != 64 {
				return c, fmt.Errorf("invalid %s fixture %q", lane, f.Name)
			}
			names[lane+"/"+f.Name] = true
			if lane == "execution" && (!absolute(f.Reference) || !absolute(f.Native) || !absolute(f.Allocation)) {
				return c, fmt.Errorf("execution fixture %s requires paired binaries", f.Name)
			}
			if !absolute(f.Artifact) {
				return c, fmt.Errorf("fixture %s requires an absolute artifact", f.Name)
			}
		}
	}
	if len(names) == 0 {
		return c, fmt.Errorf("configuration requires a measurement lane")
	}
	return c, nil
}
func order(sample int) []string {
	if sample%2 == 0 {
		return []string{"interpreter", "native"}
	}
	return []string{"native", "interpreter"}
}
func rssTree(parent int) (uint64, error) {
	directories, err := os.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("read process inventory: %w", err)
	}
	type process struct {
		pid, parent int
		rss         uint64
	}
	var processes []process
	for _, directory := range directories {
		pid, err := strconv.Atoi(directory.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", directory.Name(), "status"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("read process %d: %w", pid, err)
		}
		p := process{pid: pid}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			switch fields[0] {
			case "PPid:":
				p.parent, err = strconv.Atoi(fields[1])
			case "VmRSS:":
				var kb uint64
				kb, err = strconv.ParseUint(fields[1], 10, 64)
				p.rss = kb * 1024
			}
			if err != nil {
				return 0, fmt.Errorf("parse process %d: %w", pid, err)
			}
		}
		processes = append(processes, p)
	}
	selected := map[int]bool{parent: true}
	for {
		changed := false
		for _, p := range processes {
			if selected[p.parent] && !selected[p.pid] {
				selected[p.pid] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	var total uint64
	for _, p := range processes {
		if selected[p.pid] {
			total += p.rss
		}
	}
	return total, nil
}
func execute(argv []string, work string, timeout time.Duration, memory bool) (observation, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Dir = work
	command.Stdin = nil
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// The installed compiler owns private tool groups and cancels them on SIGINT.
	command.Cancel = func() error { return command.Process.Signal(os.Interrupt) }
	command.WaitDelay = 5 * time.Second
	var output limitedBuffer
	command.Stdout = &output
	command.Stderr = &output
	started := time.Now()
	if err := command.Start(); err != nil {
		return observation{}, fmt.Errorf("start %s: %w", argv[0], err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var peak uint64
	var sampleErr error
	sample := func() {
		if memory {
			size, err := rssTree(command.Process.Pid)
			if err != nil {
				sampleErr = err
				_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			}
			if size > peak {
				peak = size
			}
		}
	}
	sample()
	for {
		select {
		case err := <-done:
			elapsed := time.Since(started).Nanoseconds()
			observed := observation{Status: command.ProcessState.ExitCode(), Output: append([]byte{}, output.Bytes()...), Elapsed: elapsed, Peak: peak}
			if sampleErr != nil {
				return observed, sampleErr
			}
			if ctx.Err() != nil {
				return observed, fmt.Errorf("command deadline %s: %w", argv[0], ctx.Err())
			}
			if err != nil {
				return observed, fmt.Errorf("command %v failed: %w: %s", argv, err, observed.Output)
			}
			if memory && peak == 0 {
				return observed, fmt.Errorf("RSS sampler did not observe %s", argv[0])
			}
			return observed, nil
		case <-ticker.C:
			sample()
		}
	}
}
func checked(argv []string, work string) (observation, error) {
	return execute(argv, work, 60*time.Second, false)
}
func build(c config, f fixture) (observation, error) {
	return execute([]string{c.Compiler, "build", f.Source, "--output", f.Artifact, "--format", "json"}, f.Work, buildTimeout, true)
}
func command(c config, f fixture, backend, verb string) []string {
	args := []string{f.Artifact, verb}
	if backend == "interpreter" {
		args = []string{c.Compiler, verb, f.Source}
	}
	return args
}
func verifyNativeBenchmark(f fixture, expected string) error {
	result, err := checked([]string{f.Native, "--verify"}, f.Work)
	if err != nil {
		return fmt.Errorf("verify native benchmark %s: %w", f.Name, err)
	}
	if strings.TrimSpace(string(result.Output)) != expected {
		return fmt.Errorf("native benchmark result differs %s: %s", f.Name, result.Output)
	}
	return nil
}

func execution(c config, f fixture) (map[string]any, error) {
	if _, err := build(c, f); err != nil {
		return nil, err
	}
	var expected string
	for _, backend := range []string{"interpreter", "native"} {
		observed, err := checked(append(command(c, f, backend, "run"), "--max-steps", steps), f.Work)
		if err != nil {
			return nil, err
		}
		result := strings.TrimSpace(string(observed.Output))
		if backend == "interpreter" {
			expected = result
			if f.Expected != "" && result != f.Expected {
				return nil, fmt.Errorf("independent result differs %s", f.Name)
			}
		} else if result != expected {
			return nil, fmt.Errorf("native result differs %s", f.Name)
		}
	}

	if err := verifyNativeBenchmark(f, expected); err != nil {
		return nil, err
	}

	var samples []map[string]any
	for index := 0; index < c.SamplePlan["execution"]+warmup; index++ {
		results := map[string]any{}
		for _, backend := range order(index) {
			argv := []string{f.Native}
			if backend == "interpreter" {
				argv = []string{f.Reference, "-test.run=^$", "-test.bench=BenchmarkExecution", "-test.benchmem", "-test.benchtime=200ms"}
			}
			measured, err := checked(argv, f.Work)
			if err != nil {
				return nil, err
			}
			if backend == "interpreter" {
				match := benchmarkPattern.FindSubmatch(measured.Output)
				if match == nil {
					return nil, fmt.Errorf("incomplete reference benchmark %s: %s", f.Name, measured.Output)
				}
				ns, err := strconv.ParseFloat(string(match[1]), 64)
				if err != nil || ns <= 0 || math.IsNaN(ns) || math.IsInf(ns, 0) {
					return nil, fmt.Errorf("invalid reference timing %s", f.Name)
				}
				size, err := strconv.ParseUint(string(match[2]), 10, 64)
				if err != nil {
					return nil, fmt.Errorf("reference allocation bytes %s: %w", f.Name, err)
				}
				allocations, err := strconv.ParseUint(string(match[3]), 10, 64)
				if err != nil {
					return nil, fmt.Errorf("reference allocation count %s: %w", f.Name, err)
				}
				results[backend] = map[string]any{"ns": ns, "bytes": size, "allocations": allocations}
			} else {
				var result map[string]any
				if err = json.Unmarshal(measured.Output, &result); err != nil {
					return nil, fmt.Errorf("parse native benchmark %s: %w", f.Name, err)
				}
				ns, ok := result["ns"].(float64)
				if !ok || ns <= 0 || math.IsNaN(ns) || math.IsInf(ns, 0) {
					return nil, fmt.Errorf("invalid native timing %s", f.Name)
				}
				results[backend] = result
			}
		}
		if index >= warmup {
			samples = append(samples, map[string]any{"sample": index - warmup, "order": order(index), "results": results})
		}
	}
	record := map[string]any{"name": f.Name, "source_sha256": f.SourceSHA, "native_benchmark_verified": true, "samples": samples}
	if f.Allocation != "" {
		result, err := checked([]string{f.Allocation}, f.Work)
		if err != nil {
			return nil, err
		}
		var metrics map[string]any
		if err = json.Unmarshal(result.Output, &metrics); err != nil {
			return nil, fmt.Errorf("parse native allocation metrics: %w", err)
		}
		record["native_allocation_volume"] = metrics
	}
	return record, nil
}
func process(c config, f fixture) (map[string]any, error) {
	if _, err := build(c, f); err != nil {
		return nil, err
	}
	var pairs []map[string]any
	for index := 0; index < c.SamplePlan["process"]+warmup; index++ {
		pair := map[string]any{}
		for _, backend := range order(index) {
			work := filepath.Join(f.Work, backend)
			if err := os.MkdirAll(work, os.ModePerm); err != nil {
				return nil, err
			}
			args := append(command(c, f, backend, "run"), "--max-steps", steps)
			if f.Name == "file" {
				if err := os.WriteFile(filepath.Join(work, "input.txt"), []byte(strings.Repeat("abc", 10000)), 0o600); err != nil {
					return nil, err
				}
				args = append(args, "--read-root", work, "--write-root", work)
			}
			result, err := checked(args, work)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(string(result.Output)) != f.Expected {
				return nil, fmt.Errorf("process result differs: %s %s: %s", f.Name, backend, result.Output)
			}
			if f.Name == "file" {
				data, err := os.ReadFile(filepath.Join(work, "output.txt"))
				if err != nil || string(data) != strings.Repeat("abc", 10000) {
					return nil, fmt.Errorf("file output differs: %v", err)
				}
			}
			pair[backend] = result.Elapsed
		}
		if index >= warmup {
			pair["sample"] = index - warmup
			pair["order"] = order(index)
			pairs = append(pairs, pair)
		}
	}
	return map[string]any{"name": f.Name, "source_sha256": f.SourceSHA, "samples_ns": pairs}, nil
}

type server struct {
	command *exec.Cmd
	reader  *bufio.Reader
	stderr  *limitedBuffer
	client  *http.Client
	url     string
}

func startServer(c config, f fixture, backend string) (*server, error) {
	argv := append(command(c, f, backend, "serve"), "--listen", "0", "--format", "json")
	child := exec.Command(argv[0], argv[1:]...)
	child.Dir = f.Work
	child.Stdin = nil
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stderr := new(limitedBuffer)
	child.Stderr = stderr
	stdout, err := child.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = child.Start(); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(stdout)
	ready := make(chan []byte, 1)
	go func() { line, _ := reader.ReadBytes('\n'); ready <- line }()
	var line []byte
	select {
	case line = <-ready:
	case <-time.After(10 * time.Second):
		_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
		_ = child.Wait()
		return nil, fmt.Errorf("HTTP readiness deadline %s", backend)
	}
	var envelope struct {
		Command, Status string
		Servers         []struct{ Address string }
	}
	if err = json.Unmarshal(line, &envelope); err != nil || envelope.Status != "ready" || len(envelope.Servers) != 1 {
		_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
		_ = child.Wait()
		return nil, fmt.Errorf("HTTP readiness %s: %s %s", backend, line, stderr.Bytes())
	}
	transport := &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 1, ForceAttemptHTTP2: false}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	return &server{command: child, reader: reader, stderr: stderr, client: client, url: "http://" + envelope.Servers[0].Address + "/benchmark"}, nil
}
func (s *server) alive() error {
	if err := s.command.Process.Signal(syscall.Signal(0)); err != nil {
		return fmt.Errorf("HTTP server is not alive: %w", err)
	}
	state, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(s.command.Process.Pid), "stat"))
	if err != nil {
		return fmt.Errorf("read HTTP server state: %w", err)
	}
	end := bytes.LastIndexByte(state, ')')
	if end < 0 || len(state) <= end+2 || state[end+2] == 'Z' || state[end+2] == 'X' {
		return fmt.Errorf("HTTP server is not running")
	}
	return nil
}
func serversAlive(servers map[string]*server) error {
	if len(servers) != 2 {
		return fmt.Errorf("HTTP comparison requires both servers")
	}
	for _, backend := range []string{"interpreter", "native"} {
		if servers[backend] == nil {
			return fmt.Errorf("HTTP comparison lacks %s server", backend)
		}
		if err := servers[backend].alive(); err != nil {
			return fmt.Errorf("%s: %w", backend, err)
		}
	}
	return nil
}

func (s *server) close() error {
	s.client.CloseIdleConnections()
	if err := s.command.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	output := make(chan []byte, 1)
	go func() { data, _ := io.ReadAll(s.reader); output <- data }()
	done := make(chan error, 1)
	go func() { done <- s.command.Wait() }()
	select {
	case err := <-done:
		if err != nil || s.stderr.Len() != 0 {
			return fmt.Errorf("HTTP shutdown: %v: %s", err, s.stderr.Bytes())
		}
		<-output
		return nil
	case <-time.After(10 * time.Second):
		_ = syscall.Kill(-s.command.Process.Pid, syscall.SIGKILL)
		<-done
		return fmt.Errorf("HTTP shutdown deadline")
	}
}
func httpBenchmark(c config, f fixture) (record map[string]any, returned error) {
	if _, err := build(c, f); err != nil {
		return nil, err
	}
	servers := map[string]*server{}
	defer func() {
		for _, name := range []string{"interpreter", "native"} {
			if child := servers[name]; child != nil {
				if err := child.close(); err != nil && returned == nil {
					returned = err
				}
			}
		}
	}()
	for _, backend := range []string{"interpreter", "native"} {
		child, err := startServer(c, f, backend)
		if err != nil {
			return nil, err
		}
		servers[backend] = child
	}
	var samples []map[string]any
	for index := 0; index < c.SamplePlan["http"]+warmup; index++ {
		if err := serversAlive(servers); err != nil {
			return nil, err
		}
		pair := map[string]any{}
		for _, backend := range order(index) {
			child := servers[backend]
			times := make([]int64, 0, 200)
			started := time.Now()
			for request := 0; request < 200; request++ {
				before := time.Now()
				response, err := child.client.Post(child.url, "application/octet-stream", strings.NewReader("fixed varied payload 123"))
				if err != nil {
					return nil, err
				}
				body, err := io.ReadAll(response.Body)
				closeErr := response.Body.Close()
				if err != nil || closeErr != nil || response.StatusCode != 200 || string(body) != f.Expected {
					return nil, fmt.Errorf("HTTP response differs %s: %d %q %v %v", backend, response.StatusCode, body, err, closeErr)
				}
				times = append(times, time.Since(before).Nanoseconds())
			}
			elapsed := time.Since(started).Nanoseconds()
			ordered := append([]int64{}, times...)
			sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
			pair[backend] = map[string]any{"samples_ns": times, "elapsed_ns": elapsed, "p95_ns": ordered[189], "requests_per_second": 200e9 / float64(elapsed)}
		}
		if err := serversAlive(servers); err != nil {
			return nil, err
		}
		if index >= warmup {
			pair["sample"] = index - warmup
			pair["order"] = order(index)
			samples = append(samples, pair)
		}
	}
	return map[string]any{"name": f.Name, "source_sha256": f.SourceSHA, "samples": samples, "requests_per_sample": 200, "concurrency": 1, "both_servers_alive": true}, nil
}
func builds(c config, f fixture) ([]map[string]any, error) {
	var records []map[string]any
	for sample := 0; sample < c.SamplePlan["build"]+warmup+1; sample++ {
		result, err := build(c, f)
		if err != nil {
			return records, err
		}
		info, err := os.Stat(f.Artifact)
		if err != nil {
			return records, err
		}
		phase, index := "measurement", sample-warmup-1
		if sample == 0 {
			phase, index = "first-build", 0
		} else if sample <= warmup {
			phase, index = "warmup", sample-1
		}
		records = append(records, map[string]any{"name": f.Name, "source_sha256": f.SourceSHA, "phase": phase, "sample": index, "elapsed_ns": result.Elapsed, "peak_process_tree_rss_bytes": result.Peak, "artifact_bytes": info.Size(), "diagnostic_bytes": len(result.Output)})

		fmt.Fprintf(os.Stderr, "build %s %d: %d ns %d RSS bytes\n", f.Name, sample, result.Elapsed, result.Peak)
	}
	return records, nil
}
func writeReport(path string, report map[string]any) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(data, '\n'))
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
func otherBenchmarkJobs(c config) ([]string, error) {
	executablePaths := map[string]bool{c.Compiler: true}
	for _, fixtures := range [][]fixture{c.Execution, c.Process, c.HTTP, c.Build} {
		for _, fixture := range fixtures {
			for _, path := range []string{fixture.Reference, fixture.Native, fixture.Allocation, fixture.Artifact} {
				if path != "" {
					executablePaths[path] = true
				}
			}
		}
	}
	directories, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("read benchmark job inventory: %w", err)
	}
	jobs := []string{}
	for _, directory := range directories {
		pid, err := strconv.Atoi(directory.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		command, err := os.ReadFile(filepath.Join("/proc", directory.Name(), "cmdline"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read benchmark job %d: %w", pid, err)
		}
		executable := string(bytes.SplitN(command, []byte{0}, 2)[0])
		if executablePaths[executable] || filepath.Base(executable) == filepath.Base(os.Args[0]) {
			jobs = append(jobs, fmt.Sprintf("pid %d %s", pid, filepath.Base(executable)))
		}
	}
	sort.Strings(jobs)
	return jobs, nil
}

func executionEnvironment() (map[string]string, error) {
	var system []string
	for _, name := range []string{"ostype", "osrelease", "version"} {
		data, err := os.ReadFile(filepath.Join("/proc/sys/kernel", name))
		if err != nil {
			return nil, fmt.Errorf("read actual guest kernel %s: %w", name, err)
		}
		value := strings.TrimSpace(string(data))
		if value == "" {
			return nil, fmt.Errorf("empty guest kernel %s", name)
		}
		system = append(system, value)
	}
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return nil, fmt.Errorf("read actual guest processor: %w", err)
	}
	fields := map[string]string{}
	for _, line := range strings.Split(strings.SplitN(string(data), "\n\n", 2)[0], "\n") {
		key, value, found := strings.Cut(line, ":")
		if found {
			fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	var processor []string
	for _, name := range []string{"model name", "CPU implementer", "CPU architecture", "CPU variant", "CPU part", "CPU revision", "Hardware"} {
		if fields[name] != "" {
			processor = append(processor, name+"="+fields[name])
		}
	}
	if len(processor) == 0 {
		return nil, fmt.Errorf("guest processor has no stable identity fields")
	}
	return map[string]string{"system": strings.Join(system, " "), "machine": runtime.GOARCH,
		"processor": strings.Join(processor, ", "), "execution": "qemu-guest"}, nil
}

func loadAverages() ([]float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil, fmt.Errorf("read load context: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return nil, fmt.Errorf("load context lacks three averages")
	}
	values := make([]float64, 3)
	for index := range values {
		value, err := strconv.ParseFloat(fields[index], 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return nil, fmt.Errorf("invalid load context average")
		}
		values[index] = value
	}
	return values, nil
}

func run(c config) (map[string]any, error) {
	return runWithObservations(c, executionEnvironment, otherBenchmarkJobs, loadAverages)
}

// runWithObservations keeps the real runner with injected environment I/O boundaries.
func runWithObservations(c config, environmentSnapshot func() (map[string]string, error), inventory func(config) ([]string, error), loadSnapshot func() ([]float64, error)) (map[string]any, error) {
	environment, environmentErr := environmentSnapshot()
	report := map[string]any{"other_benchmark_jobs": []string{}, "benchmark_job_observation": "Informational /proc process and load snapshots before and after sequential measurement; no load admission.", "measurement_complete": false, "target": "linux/arm64", "sample_plan": c.SamplePlan, "warmup": warmup, "max_steps": 10000000, "concurrency": 1, "order": "alternating paired samples", "clock": "Go monotonic time; native C CLOCK_MONOTONIC", "checked_execution": "program admission before timing; session run, context close, and returned value release inside timing"}
	if environmentErr != nil {
		return report, environmentErr
	}
	report["environment"] = environment

	jobs, err := inventory(c)
	report["other_benchmark_jobs"] = jobs
	if err != nil {
		return report, err
	}
	load, err := loadSnapshot()
	if err != nil {
		return report, err
	}
	snapshots := map[string]any{"before": map[string]any{"other_benchmark_jobs": jobs, "load_average_1_5_15": load}}
	report["benchmark_context"] = snapshots

	for _, lane := range []struct {
		name     string
		fixtures []fixture
		measure  func(config, fixture) (map[string]any, error)
	}{{"execution", c.Execution, execution}, {"process", c.Process, process}, {"http", c.HTTP, httpBenchmark}} {
		if len(lane.fixtures) == 0 {
			continue
		}
		var records []map[string]any
		for _, fixture := range lane.fixtures {
			record, err := lane.measure(c, fixture)
			if err != nil {
				return report, fmt.Errorf("%s %s: %w", lane.name, fixture.Name, err)
			}
			records = append(records, record)
			report[lane.name] = records
			if err = writeReport(c.Output, report); err != nil {
				return report, err
			}
			fmt.Fprintf(os.Stderr, "%s %s complete\n", lane.name, fixture.Name)
		}
	}
	if len(c.Build) > 0 {
		var records []map[string]any
		for _, fixture := range c.Build {
			current, err := builds(c, fixture)
			records = append(records, current...)
			report["build"] = records
			if err != nil {
				return report, err
			}
			if err = writeReport(c.Output, report); err != nil {
				return report, err
			}
		}
	}
	jobs, err = inventory(c)
	if err != nil {
		return report, err
	}
	load, err = loadSnapshot()
	if err != nil {
		return report, err
	}
	snapshots["after"] = map[string]any{"other_benchmark_jobs": jobs, "load_average_1_5_15": load}
	report["other_benchmark_jobs"] = append(report["other_benchmark_jobs"].([]string), jobs...)
	report["measurement_complete"] = true
	return report, writeReport(c.Output, report)
}
func main() {
	path := flag.String("config", "", "declared performance configuration")
	flag.Parse()
	if *path == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "--config is required")
		os.Exit(2)
	}
	c, err := readConfig(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	runtime.GOMAXPROCS(1)
	if err = os.Setenv("GOMAXPROCS", "1"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	report, err := run(c)
	if err != nil {
		report["error"] = err.Error()
		_ = writeReport(c.Output, report)
		fmt.Fprintln(os.Stderr, err)
	}
	if encodeErr := json.NewEncoder(os.Stdout).Encode(report); encodeErr != nil {
		fmt.Fprintln(os.Stderr, encodeErr)
		os.Exit(1)
	}
	if err != nil {
		os.Exit(1)
	}
}
