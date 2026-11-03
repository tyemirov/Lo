// Command performance-runner measures declared fixtures without a development toolchain.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

type fixture struct {
	Name       string          `json:"name"`
	Source     string          `json:"source"`
	Expected   string          `json:"expected"`
	SourceSHA  string          `json:"source_sha256"`
	Oracle     *trainingOracle `json:"oracle,omitempty"`
	Native     string          `json:"native"`
	Allocation string          `json:"allocation"`
	Artifact   string          `json:"artifact"`
	Work       string          `json:"work"`
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
		return c, fmt.Errorf("configuration requires native samples, three warmups, and absolute compiler/output paths")
	}
	plan := map[string]int{"execution": 10, "process": 10, "http": 100, "build": 10}
	if len(c.SamplePlan) != len(plan) {
		return c, fmt.Errorf("configuration requires the complete canonical sample_plan")
	}
	for lane, count := range plan {
		if c.SamplePlan[lane] != count {
			return c, fmt.Errorf("configuration sample_plan %s requires %d samples", lane, count)
		}
	}
	names := map[string]bool{}
	for lane, fixtures := range map[string][]fixture{"execution": c.Execution, "process": c.Process, "http": c.HTTP, "build": c.Build} {
		for _, f := range fixtures {
			if f.Name == "" || names[lane+"/"+f.Name] || !absolute(f.Source) || !absolute(f.Work) || len(f.SourceSHA) != 64 {
				return c, fmt.Errorf("invalid %s fixture %q", lane, f.Name)
			}
			names[lane+"/"+f.Name] = true
			if lane == "execution" && (!absolute(f.Native) || !absolute(f.Allocation)) {
				return c, fmt.Errorf("execution fixture %s requires native binaries", f.Name)
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
	data, err := os.ReadFile(f.Source)
	if err != nil {
		return observation{}, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != f.SourceSHA {
		return observation{}, fmt.Errorf("fixture source identity differs")
	}
	return execute([]string{c.Compiler, "build", f.Source, "--output", f.Artifact, "--format", "json"}, f.Work, buildTimeout, true)
}

type trainingOracle struct {
	InitialLoss float64                    `json:"initial_loss"`
	Loss        float64                    `json:"loss"`
	Parameters  map[string]json.RawMessage `json:"parameters"`
}
type typedValue struct {
	Type, Status string
	Value        json.RawMessage
}

func verifyFixture(output []byte, f fixture) error {
	text := strings.TrimSpace(string(output))
	if f.Oracle == nil && text == f.Expected && f.Expected != "" {
		return nil
	}
	var envelope struct{ Result typedValue }
	if err := json.Unmarshal(output, &envelope); err != nil {
		return fmt.Errorf("independent output %s: %w", f.Name, err)
	}
	node := envelope.Result
	for node.Status == "ok" {
		var next typedValue
		if err := json.Unmarshal(node.Value, &next); err != nil {
			return err
		}
		node = next
	}
	if f.Oracle == nil {
		var value string
		if err := json.Unmarshal(node.Value, &value); err != nil {
			return err
		}
		if node.Type != "Int" || value != f.Expected {
			return fmt.Errorf("independent result differs %s", f.Name)
		}
		return nil
	}
	var fields map[string]typedValue
	if err := json.Unmarshal(node.Value, &fields); err != nil {
		return err
	}
	check := func(raw json.RawMessage, expected float64, tolerance float64) error {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return err
		}
		value, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value-expected) > tolerance+1e-5*math.Abs(expected) {
			return fmt.Errorf("training independent number differs")
		}
		return nil
	}
	if len(fields) != 3 {
		return fmt.Errorf("training result inventory differs")
	}
	for name, expected := range map[string]float64{"initial_loss": f.Oracle.InitialLoss, "loss": f.Oracle.Loss} {
		if fields[name].Type != "Float" {
			return fmt.Errorf("training loss type differs")
		}
		if err := check(fields[name].Value, expected, 1e-8); err != nil {
			return err
		}
	}
	var parameters map[string]typedValue
	if err := json.Unmarshal(fields["parameters"].Value, &parameters); err != nil {
		return err
	}
	if len(parameters) != len(f.Oracle.Parameters) {
		return fmt.Errorf("training parameter inventory differs")
	}
	for name, raw := range f.Oracle.Parameters {
		var reference []json.RawMessage
		if err := json.Unmarshal(raw, &reference); err != nil {
			return err
		}
		if len(reference) != 2 {
			return fmt.Errorf("training oracle shape differs")
		}
		var shape []int
		var expected []float64
		if err := json.Unmarshal(reference[0], &shape); err != nil {
			return err
		}
		if err := json.Unmarshal(reference[1], &expected); err != nil {
			return err
		}
		var tensor struct {
			Dtype, Device string
			Shape         []string
			Values        []json.RawMessage
		}
		if err := json.Unmarshal(parameters[name].Value, &tensor); err != nil {
			return err
		}
		if parameters[name].Type != "Tensor<FP32>" || tensor.Dtype != "fp32" || tensor.Device != "cpu" || len(tensor.Shape) != len(shape) || len(tensor.Values) != len(expected) {
			return fmt.Errorf("training tensor metadata differs")
		}
		for index, dimension := range shape {
			if tensor.Shape[index] != strconv.Itoa(dimension) {
				return fmt.Errorf("training tensor shape differs")
			}
		}
		for index, number := range expected {
			if err := check(tensor.Values[index], number, 1e-6); err != nil {
				return err
			}
		}
	}
	return nil
}
func command(f fixture, verb string) []string { return []string{f.Artifact, verb} }
func verifyNativeBenchmark(f fixture, expected string) error {
	result, err := checked([]string{f.Native, "--verify"}, f.Work)
	if err != nil {
		return fmt.Errorf("verify native benchmark %s: %w", f.Name, err)
	}
	f.Expected = expected
	if err := verifyFixture(result.Output, f); err != nil {
		return fmt.Errorf("native benchmark result differs %s: %w", f.Name, err)
	}
	return nil
}
func execution(c config, f fixture) (map[string]any, error) {
	if _, err := build(c, f); err != nil {
		return nil, err
	}
	observed, err := checked(append(command(f, "run"), "--max-steps", steps, "--format", "json"), f.Work)
	if err != nil {
		return nil, err
	}
	if err = verifyFixture(observed.Output, f); err != nil {
		return nil, err
	}
	if err = verifyNativeBenchmark(f, f.Expected); err != nil {
		return nil, err
	}
	var samples []map[string]any
	for index := 0; index < c.SamplePlan["execution"]+warmup; index++ {
		measured, err := checked([]string{f.Native}, f.Work)
		if err != nil {
			return nil, err
		}
		var timing map[string]any
		if err = json.Unmarshal(measured.Output, &timing); err != nil {
			return nil, err
		}
		if index >= warmup {
			timing["sample"] = index - warmup
			samples = append(samples, timing)
		}
	}
	result, err := checked([]string{f.Allocation}, f.Work)
	if err != nil {
		return nil, err
	}
	var metrics map[string]any
	if err = json.Unmarshal(result.Output, &metrics); err != nil {
		return nil, err
	}
	return map[string]any{"name": f.Name, "source_sha256": f.SourceSHA, "verified": true, "samples": samples, "native_allocation_volume": metrics}, nil
}
func process(c config, f fixture) (map[string]any, error) {
	if _, err := build(c, f); err != nil {
		return nil, err
	}
	var samples []map[string]any
	for index := 0; index < c.SamplePlan["process"]+warmup; index++ {
		args := append(command(f, "run"), "--max-steps", steps)
		if f.Name == "file" {
			if err := os.WriteFile(filepath.Join(f.Work, "input.txt"), []byte(strings.Repeat("abc", 10000)), 0o600); err != nil {
				return nil, err
			}
			args = append(args, "--read-root", f.Work, "--write-root", f.Work)
		}
		result, err := checked(args, f.Work)
		if err != nil {
			return nil, err
		}
		if err = verifyFixture(result.Output, f); err != nil {
			return nil, err
		}
		if f.Name == "file" {
			data, err := os.ReadFile(filepath.Join(f.Work, "output.txt"))
			if err != nil || string(data) != strings.Repeat("abc", 10000) {
				return nil, fmt.Errorf("file output differs: %v", err)
			}
		}
		if index >= warmup {
			samples = append(samples, map[string]any{"sample": index - warmup, "wall_ns": result.Elapsed})
		}
	}
	return map[string]any{"name": f.Name, "source_sha256": f.SourceSHA, "verified": true, "samples": samples}, nil
}

type server struct {
	command *exec.Cmd
	reader  *bufio.Reader
	stderr  *limitedBuffer
	client  *http.Client
	url     string
}

func validServerAddress(address string) bool {
	host, rawPort, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	port, err := strconv.Atoi(rawPort)
	return err == nil && port > 0 && port <= 65535
}

func readyAddress(data []byte) (string, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return "", err
	}
	for key := range envelope {
		if key != "command" && key != "status" && key != "servers" && key != "result" {
			return "", fmt.Errorf("unexpected ready field")
		}
	}
	var command, status string
	if err := json.Unmarshal(envelope["command"], &command); err != nil {
		return "", err
	}
	if err := json.Unmarshal(envelope["status"], &status); err != nil {
		return "", err
	}
	if command != "serve" || status != "ready" {
		return "", fmt.Errorf("serve ready envelope required")
	}
	var servers []map[string]json.RawMessage
	if err := json.Unmarshal(envelope["servers"], &servers); err != nil {
		return "", err
	}
	if len(servers) != 1 {
		return "", fmt.Errorf("one server required")
	}
	var address string
	if err := json.Unmarshal(servers[0]["address"], &address); err != nil {
		return "", err
	}
	if !validServerAddress(address) {
		return "", fmt.Errorf("loopback server address required")
	}
	return address, nil
}

func startServer(c config, f fixture) (*server, error) {
	argv := append(command(f, "serve"), "--listen", "0", "--format", "json")
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
		return nil, fmt.Errorf("HTTP readiness deadline")
	}
	address, err := readyAddress(line)
	if err != nil {
		_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
		_ = child.Wait()
		return nil, fmt.Errorf("HTTP readiness: %w", err)
	}
	transport := &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 1, ForceAttemptHTTP2: false}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	return &server{command: child, reader: reader, stderr: stderr, client: client, url: "http://" + address + "/benchmark"}, nil
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
		var stopped map[string]string
		if err := json.Unmarshal(<-output, &stopped); err != nil || len(stopped) != 2 || stopped["command"] != "serve" || stopped["status"] != "stopped" {
			return fmt.Errorf("HTTP stopped event missing")
		}
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
	child, err := startServer(c, f)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := child.close(); err != nil {
			returned = errors.Join(returned, err)
		}
	}()
	var samples []map[string]any
	for index := 0; index < c.SamplePlan["http"]+warmup; index++ {
		if err := child.alive(); err != nil {
			return nil, err
		}
		times := make([]int64, 0, 200)
		start := time.Now()
		for request := 0; request < 200; request++ {
			before := time.Now()
			response, err := child.client.Post(child.url, "application/octet-stream", strings.NewReader("fixed varied payload 123"))
			if err != nil {
				return nil, err
			}
			body, err := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if err != nil || closeErr != nil || response.StatusCode != 200 || string(body) != f.Expected {
				return nil, fmt.Errorf("HTTP response differs")
			}
			times = append(times, time.Since(before).Nanoseconds())
		}
		elapsed := time.Since(start).Nanoseconds()
		ordered := append([]int64{}, times...)
		sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
		if err := child.alive(); err != nil {
			return nil, err
		}
		if index >= warmup {
			samples = append(samples, map[string]any{"sample": index - warmup, "samples_ns": times, "elapsed_ns": elapsed, "p95_ns": ordered[189], "requests_per_second": 200e9 / float64(elapsed)})
		}
	}
	return map[string]any{"name": f.Name, "source_sha256": f.SourceSHA, "verified": true, "samples": samples, "requests_per_sample": 200, "concurrency": 1, "ready_verified": true, "stopped_verified": true}, nil
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
			for _, path := range []string{fixture.Native, fixture.Allocation, fixture.Artifact} {
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
	report := map[string]any{"other_benchmark_jobs": []string{}, "benchmark_job_observation": "Informational /proc process and load snapshots before and after sequential measurement; no load admission.", "measurement_complete": false, "target": "linux/arm64", "sample_plan": c.SamplePlan, "warmup": warmup, "max_steps": 10000000, "concurrency": 1, "order": "sequential native samples", "clock": "Go monotonic time; native C CLOCK_MONOTONIC", "checked_execution": "program admission before timing; session run, context close, and returned value release inside timing"}
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
