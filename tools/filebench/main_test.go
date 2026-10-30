package main_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("LO_FILEBENCH_WORKER_FAILURE"); mode != "" && len(os.Args) > 2 && os.Args[1] == "run" {
		if err := injectWorkerFailure(mode, os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		result := "1"
		if mode == "overflow" {
			result = "8388608"
		}
		fmt.Printf("{\"result\":{\"type\":\"Int\",\"value\":%q}}\n", result)
		os.Exit(0)
	}
	if os.Getenv("LO_FILEBENCH_NOISY_COMPILER") == "1" && len(os.Args) > 1 && os.Args[1] == "run" {
		for range 130 {
			if _, err := fmt.Fprint(os.Stdout, strings.Repeat("x", 65536)); err != nil {
				os.Exit(0)
			}
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func injectWorkerFailure(mode, source string) error {
	write := func(path, text string) error {
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		_, writeErr := file.WriteString(text)
		return errors.Join(writeErr, file.Close())
	}
	switch mode {
	case "missing":
		return write(source, "return missing_name")
	case "result":
		return write(source, "return 0")
	case "malformed":
		return write(filepath.Join(filepath.Dir(source), "allocation-0.json"), "occupied")
	case "status":
		return write(filepath.Join(filepath.Dir(source), "allocation-0.json"), `{"allocation":{"count":1,"bytes":1,"runner_wall_ns":1},"status":0}`)
	case "fixture-reset", "overflow":
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		_, input, ok := strings.Cut(string(data), "File.Path.new(")
		if !ok {
			return fmt.Errorf("fixture path absent")
		}
		var path string
		if err := json.NewDecoder(strings.NewReader(input)).Decode(&path); err != nil {
			return err
		}
		if mode == "overflow" {
			quoted, err := json.Marshal(path)
			if err != nil {
				return err
			}
			return write(source, "return File.read(File.Path.new("+string(quoted)+"))")
		}
		directory, err := os.MkdirTemp(path, "block-")
		if err != nil {
			return err
		}
		return write(filepath.Join(directory, "child"), "retained")
	default:
		return fmt.Errorf("unknown injected worker failure %q", mode)
	}
}

func TestFilebenchDefaultInventory(t *testing.T) {
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(t.TempDir(), "lo")
	build := exec.Command("go", "build", "-o", compiler, "./cmd/lo")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build compiler: %v\n%s", err, output)
	}
	command := exec.Command("go", "run", ".", "-work", t.TempDir(), "-compiler", compiler, "-backend", "interpreter", "-cases", "startup", "-samples", "1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("default inventory: %v\n%s", err, output)
	}
	var report struct {
		Requested struct {
			Sizes []int `json:"entry_sizes"`
		}
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(report.Requested.Sizes) != "[100 1000 5000]" {
		t.Fatalf("default entry sizes: %v", report.Requested.Sizes)
	}
}

func TestFilebenchRetainsCLIOnWorkerFailure(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct{ mode, workload, payload, message string }{
		{"fixture-reset", "open-paths", "32", "prepare allocation worker fixture"},
		{"missing", "startup", "32", "read worker allocation metrics"},
		{"malformed", "startup", "32", "decode worker allocation metrics"},
		{"status", "startup", "32", "allocation worker status"},
		{"overflow", "read", "8388608", "run allocation worker"},
		{"result", "startup", "32", "verify allocation worker"},
	} {
		t.Run(fixture.mode, func(t *testing.T) {
			command := exec.Command("go", "run", ".", "-work", t.TempDir(), "-compiler", self, "-backend", "interpreter", "-cases", fixture.workload, "-io-sizes", fixture.payload, "-samples", "1")
			command.Env = append(os.Environ(), "LO_FILEBENCH_WORKER_FAILURE="+fixture.mode)
			var stdout, stderr strings.Builder
			command.Stdout, command.Stderr = &stdout, &stderr
			if err := command.Run(); err == nil {
				t.Fatal("injected allocation-worker failure accepted")
			}
			var report struct {
				Status, Error string
				Samples       []struct {
					WallNS      int64  `json:"process_wall_ns"`
					RSS         uint64 `json:"peak_rss_bytes"`
					Status      int    `json:"exit_status"`
					Verified    bool
					Error       string
					Allocations json.RawMessage
				}
			}
			if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
				t.Fatalf("partial report: %v\nstdout=%s stderr=%s", err, &stdout, &stderr)
			}
			if report.Status != "failed" || len(report.Samples) != 1 {
				t.Fatalf("observed CLI sample lost: %+v stderr=%s", report, &stderr)
			}
			sample := report.Samples[0]
			if sample.WallNS <= 0 || sample.RSS == 0 || sample.Status != 0 || sample.Verified || !strings.Contains(sample.Error, fixture.message) || string(sample.Allocations) != "null" {
				t.Fatalf("invalid retained CLI sample: %+v", sample)
			}
		})
	}
}

func TestFilebenchCLI(t *testing.T) {
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(t.TempDir(), "lo")
	build := exec.Command("go", "build", "-o", compiler, "./cmd/lo")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build public compiler: %v\n%s", err, output)
	}
	work := t.TempDir()
	command := exec.Command("go", "run", ".", "-work", work, "-compiler", compiler, "-backend", "interpreter", "-cases", "list,scan,remove,checked-remove,read,write,append,append-many,directories", "-sizes", "3", "-io-sizes", "32", "-directory-counts", "3", "-samples", "1", "-label", "smoke")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run public benchmark command: %v\n%s", err, output)
	}
	var report struct {
		Label   string `json:"label"`
		Status  string `json:"status"`
		Samples []struct {
			Case       string `json:"case"`
			Backend    string `json:"backend"`
			Verified   bool   `json:"verified"`
			WallNS     int64  `json:"process_wall_ns"`
			PeakRSS    uint64 `json:"peak_rss_bytes"`
			Allocation *struct {
				Count uint64 `json:"count"`
				Bytes uint64 `json:"bytes"`
			} `json:"allocations"`
		} `json:"samples"`
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("decode public report: %v\n%s", err, output)
	}
	if report.Label != "smoke" || report.Status != "complete" || len(report.Samples) != 9 {
		t.Fatalf("report inventory: label=%q samples=%d", report.Label, len(report.Samples))
	}
	for _, sample := range report.Samples {
		if !sample.Verified || sample.Backend != "interpreter" || sample.WallNS <= 0 || sample.PeakRSS == 0 || sample.Allocation == nil || sample.Allocation.Count == 0 || sample.Allocation.Bytes == 0 {
			t.Fatalf("unverified or incomplete sample: %+v", sample)
		}
	}
}

func TestFilebenchNearLimitPipelinesCLI(t *testing.T) {
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(t.TempDir(), "lo")
	build := exec.Command("go", "build", "-o", compiler, "./cmd/lo")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build compiler: %v\n%s", err, output)
	}
	command := exec.Command("go", "run", ".", "-work", t.TempDir(), "-compiler", compiler, "-backend", "interpreter", "-cases", "read,write,append", "-io-sizes", "12582912,16777215", "-samples", "1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("near-limit pipelines: %v\n%s", err, output)
	}
	var report struct {
		Status  string
		Samples []struct {
			Case     string
			Size     int
			Expected string
			Verified bool
		}
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "complete" || len(report.Samples) != 6 {
		t.Fatalf("near-limit inventory: %+v", report)
	}
	for _, sample := range report.Samples {
		expected := "runtime.limit"
		if sample.Size == 12582912 {
			expected = "12582912"
		}
		if !sample.Verified || sample.Expected != expected {
			t.Fatalf("near-limit result: %+v want %s", sample, expected)
		}
	}
}

func TestFilebenchWorkerMetricsStayInScratch(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "program.lo")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("return 1"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".", "worker", source, filepath.Join(repository, "go.mod"), "run", "--format", "json")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "metrics directory must stay inside Lo/_scratch") {
		t.Fatalf("non-scratch metrics accepted: %v\n%s", err, output)
	}
}

func TestFilebenchBoundsChildOutput(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".", "-work", t.TempDir(), "-compiler", self, "-backend", "interpreter", "-cases", "startup", "-samples", "1", "-timeout", "5s")
	command.Env = append(os.Environ(), "LO_FILEBENCH_NOISY_COMPILER=1")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "benchmark output exceeds") {
		t.Fatalf("unbounded child output accepted: %v\n%s", err, output)
	}
}

func TestFilebenchRejectsExternalWork(t *testing.T) {
	command := exec.Command("go", "run", ".", "-work", "/", "-backend", "interpreter", "-cases", "startup")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "inside Lo") {
		t.Fatalf("external work directory accepted: %v\n%s", err, output)
	}
}

func TestFilebenchLimitsCLI(t *testing.T) {
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(t.TempDir(), "lo")
	build := exec.Command("go", "build", "-o", compiler, "./cmd/lo")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build public compiler: %v\n%s", err, output)
	}
	command := exec.Command("go", "run", ".", "-work", t.TempDir(), "-compiler", compiler, "-backend", "interpreter", "-cases", "read,write,append,directories", "-io-sizes", "16777217", "-directory-counts", "129", "-samples", "1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run capacity benchmark: %v\n%s", err, output)
	}
	var report struct {
		Samples []struct {
			Expected string `json:"expected"`
			Verified bool   `json:"verified"`
			Status   int    `json:"exit_status"`
		} `json:"samples"`
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Samples) != 4 {
		t.Fatalf("capacity inventory: %d", len(report.Samples))
	}
	for _, sample := range report.Samples {
		if sample.Expected == "129" {
			if !sample.Verified || sample.Status != 0 {
				t.Fatalf("released directory observations: %+v", sample)
			}
		} else if sample.Expected != "runtime.limit" || !sample.Verified || sample.Status != 2 {
			t.Fatalf("capacity result: %+v", sample)
		}
	}
}

func TestFilebenchWorkerRejectsExistingOutput(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "program.lo")
	program, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := program.WriteString("return 1"); err != nil {
		t.Fatal(err)
	}
	if err := program.Close(); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "sentinel")
	file, err := os.Create(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("retain"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "allocation.json")
	if err := os.Symlink(sentinel, output); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".", "worker", source, output, "run", "--format", "json")
	if data, err := command.CombinedOutput(); err == nil || !strings.Contains(string(data), "file exists") {
		t.Fatalf("existing metrics output accepted: %v\n%s", err, data)
	}
	retained, err := os.ReadFile(sentinel)
	if err != nil || string(retained) != "retain" {
		t.Fatalf("existing output target changed: %q %v", retained, err)
	}
}
