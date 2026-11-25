package main_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var sharedTools struct {
	sync.Once
	directory, compiler, filebench string
	err                            error
}

func TestMain(tests *testing.M) {
	if mode := os.Getenv("LO_FILEBENCH_INJECTION"); mode != "" && len(os.Args) > 1 {
		switch os.Args[1] {
		case "build":
			for index, arg := range os.Args {
				if arg == "--output" && index+1 < len(os.Args) {
					self, err := os.Executable()
					if err != nil {
						os.Exit(2)
					}
					if err = os.Link(self, os.Args[index+1]); err != nil {
						os.Exit(2)
					}
					os.Exit(0)
				}
			}
			os.Exit(2)
		case "run":
			if mode == "overflow" {
				for range 130 {
					if _, err := fmt.Fprint(os.Stdout, strings.Repeat("x", 65536)); err != nil {
						os.Exit(0)
					}
				}
				os.Exit(0)
			}
			fmt.Println(`{"result":{"type":"Int","value":"0"}}`)
			os.Exit(0)
		}
	}
	status := tests.Run()
	if sharedTools.directory != "" {
		if err := os.RemoveAll(sharedTools.directory); err != nil {
			fmt.Fprintln(os.Stderr, err)
			status = 1
		}
	}
	os.Exit(status)
}

func fixtureTools(t *testing.T) (string, string) {
	t.Helper()
	sharedTools.Do(func() {
		repository, err := filepath.Abs("../..")
		if err != nil {
			sharedTools.err = err
			return
		}
		root, err := filepath.EvalSymlinks(os.Getenv("TMPDIR"))
		if err != nil {
			sharedTools.err = err
			return
		}
		if relative, err := filepath.Rel(repository, root); err != nil || !filepath.IsLocal(relative) {
			sharedTools.err = fmt.Errorf("filebench TMPDIR must stay inside Lo")
			return
		}
		for _, tool := range []struct {
			variable, name, packagePath string
			destination                 *string
		}{
			{"LO_TEST_COMPILER", "lo", "./cmd/lo", &sharedTools.compiler},
			{"LO_TEST_FILEBENCH", "filebench", "./tools/filebench", &sharedTools.filebench},
		} {
			if selected := os.Getenv(tool.variable); selected != "" {
				resolved, err := filepath.EvalSymlinks(selected)
				if err != nil {
					sharedTools.err = fmt.Errorf("resolve %s: %w", tool.variable, err)
					return
				}
				if relative, err := filepath.Rel(repository, resolved); err != nil || !filepath.IsLocal(relative) {
					sharedTools.err = fmt.Errorf("%s must stay inside Lo", tool.variable)
					return
				}
				*tool.destination = resolved
				continue
			}
			if sharedTools.directory == "" {
				sharedTools.directory, err = os.MkdirTemp(root, "filebench-tools-")
				if err != nil {
					sharedTools.err = err
					return
				}
			}
			*tool.destination = filepath.Join(sharedTools.directory, tool.name)
			command := exec.Command("go", "build", "-o", *tool.destination, tool.packagePath)
			command.Dir = repository
			if output, err := command.CombinedOutput(); err != nil {
				sharedTools.err = fmt.Errorf("build %s: %w\n%s", tool.name, err, output)
				return
			}
		}
	})
	if sharedTools.err != nil {
		t.Fatal(sharedTools.err)
	}
	return sharedTools.compiler, sharedTools.filebench
}

func toolchain(t *testing.T) string {
	t.Helper()
	path := os.Getenv("LO_LLVM_TOOLCHAIN")
	if path == "" {
		t.Fatal("LO_LLVM_TOOLCHAIN must select native inputs")
	}
	return path
}
func invoke(t *testing.T, cases string, extra ...string) []byte {
	t.Helper()
	frontend, executable := fixtureTools(t)
	args := []string{"-work", t.TempDir(), "-compiler", frontend, "-toolchain", toolchain(t), "-cases", cases, "-samples", "1"}
	args = append(args, extra...)
	output, err := exec.Command(executable, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("native filebench: %v\n%s", err, output)
	}
	return output
}
func TestFilebenchCLI(t *testing.T) {
	output := invoke(t, "startup,list,scan,remove,checked-remove,read,write,append,append-many,directories", "-sizes", "3", "-io-sizes", "32", "-directory-counts", "3")
	var report struct {
		Status  string
		Samples []struct {
			Verified bool
			Wall     int64  `json:"process_wall_ns"`
			RSS      uint64 `json:"peak_rss_bytes"`
		}
		Requested struct {
			Sizes []int `json:"entry_sizes"`
		}
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("report: %v\n%s", err, output)
	}
	if report.Status != "complete" || len(report.Samples) != 10 {
		t.Fatalf("inventory: %+v", report)
	}
	for _, sample := range report.Samples {
		if !sample.Verified || sample.Wall <= 0 || sample.RSS == 0 {
			t.Fatalf("native sample: %+v", sample)
		}
	}
	if strings.Contains(string(output), "interpreter") || strings.Contains(string(output), "worker_runtime") {
		t.Fatal("obsolete runtime measurement")
	}
}
func TestFilebenchDefaultInventory(t *testing.T) {
	var report struct {
		Requested struct {
			Sizes []int `json:"entry_sizes"`
		}
	}
	if err := json.Unmarshal(invoke(t, "startup"), &report); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(report.Requested.Sizes) != "[100 1000 5000]" {
		t.Fatal(report.Requested.Sizes)
	}
}
func TestFilebenchLimitsCLI(t *testing.T) {
	var report struct {
		Samples []struct {
			Expected string
			Verified bool
			Status   int `json:"exit_status"`
		}
	}
	if err := json.Unmarshal(invoke(t, "read,write,append,directories", "-io-sizes", "16777217", "-directory-counts", "129"), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Samples) != 4 {
		t.Fatal("capacity inventory")
	}
	for _, sample := range report.Samples {
		if !sample.Verified || (sample.Expected == "runtime.limit" && sample.Status != 2) || (sample.Expected == "129" && sample.Status != 0) {
			t.Fatalf("capacity: %+v", sample)
		}
	}
}
func TestFilebenchNearLimitPipelinesCLI(t *testing.T) {
	var report struct {
		Samples []struct {
			Expected string
			Verified bool
			Size     int
		}
	}
	if err := json.Unmarshal(invoke(t, "read,write,append", "-io-sizes", "12582912,16777215"), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Samples) != 6 {
		t.Fatal("near-limit inventory")
	}
	for _, sample := range report.Samples {
		expected := "runtime.limit"
		if sample.Size == 12582912 {
			expected = "12582912"
		}
		if !sample.Verified || sample.Expected != expected {
			t.Fatalf("near-limit: %+v", sample)
		}
	}
}
func TestFilebenchRejectsRemovedCommands(t *testing.T) {
	_, executable := fixtureTools(t)
	for _, args := range [][]string{{"-backend", "native"}, {"worker", "source.lo", "metrics.json"}, {"-work", "/"}} {
		output, err := exec.Command(executable, args...).CombinedOutput()
		if err == nil {
			t.Fatalf("obsolete/outside command accepted: %v", args)
		}
		if len(output) == 0 {
			t.Fatal("failure missing diagnostic")
		}
	}
}
func TestFilebenchPreservesFailureAndOutputBound(t *testing.T) {
	_, executable := fixtureTools(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"wrong-result", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			command := exec.Command(executable, "-work", t.TempDir(), "-compiler", self, "-toolchain", toolchain(t), "-cases", "startup", "-samples", "1", "-timeout", "5s")
			command.Env = append(os.Environ(), "LO_FILEBENCH_INJECTION="+mode)
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatal("invalid sample accepted")
			}
			expected := "want Int"
			if mode == "overflow" {
				expected = "benchmark output exceeds"
			}
			if !strings.Contains(string(output), expected) {
				t.Fatalf("failure: %s", output)
			}
		})
	}
}
