package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

type goCase struct {
	Package       string   `json:"package"`
	Name          string   `json:"name"`
	File          string   `json:"file"`
	Constraint    string   `json:"constraint"`
	Configuration string   `json:"configuration"`
	AllowSkip     bool     `json:"allow_skip"`
	SkipReasons   []string `json:"skip_reasons,omitempty"`
}

type sourceInventory struct {
	Go     []goCase          `json:"go"`
	Python []json.RawMessage `json:"python"`
	Rust   []json.RawMessage `json:"rust"`
}

func discover(root string) (sourceInventory, error) {
	var result sourceInventory
	for _, base := range []string{"cmd", "internal", "tests", "tools"} {
		err := filepath.WalkDir(filepath.Join(root, base), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, contents, 0)
			if err != nil {
				return err
			}
			relative, _ := filepath.Rel(root, path)
			constraint := ""
			for _, line := range strings.Split(string(contents), "\n") {
				if strings.HasPrefix(line, "//go:build ") {
					constraint = strings.TrimPrefix(line, "//go:build ")
					break
				}
			}
			defaultContext := build.Default
			defaultContext.BuildTags = nil
			if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
				defaultContext.CgoEnabled = true
			}
			active, err := defaultContext.MatchFile(filepath.Dir(path), filepath.Base(path))
			if err != nil {
				return err
			}
			configuration := "host"
			if !active {
				metalContext := defaultContext
				metalContext.BuildTags = []string{"metal"}
				metal, err := metalContext.MatchFile(filepath.Dir(path), filepath.Base(path))
				if err != nil {
					return err
				}
				if metal {
					configuration = "metal"
				} else {
					configuration = "other-platform"
				}
			}
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Recv != nil || function.Type.Params == nil || len(function.Type.Params.List) != 1 {
					continue
				}
				name := function.Name.Name
				if !strings.HasPrefix(name, "Test") && !strings.HasPrefix(name, "Fuzz") {
					continue
				}
				pointer, ok := function.Type.Params.List[0].Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				selector, ok := pointer.X.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "T" && selector.Sel.Name != "F" {
					continue
				}
				allowSkip := false
				reasons := []string{}
				ast.Inspect(function.Body, func(node ast.Node) bool {
					if call, ok := node.(*ast.CallExpr); ok {
						if selected, ok := call.Fun.(*ast.SelectorExpr); ok && (selected.Sel.Name == "Skip" || selected.Sel.Name == "Skipf") {
							allowSkip = true
							if len(call.Args) > 0 {
								if literal, ok := call.Args[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
									if value, err := strconv.Unquote(literal.Value); err == nil {
										reasons = append(reasons, value)
									}
								}
							}
						}
					}
					return true
				})
				result.Go = append(result.Go, goCase{Package: "./" + filepath.Dir(relative), Name: name, File: relative, Constraint: constraint, Configuration: configuration, AllowSkip: allowSkip, SkipReasons: reasons})
			}
			return nil
		})
		if err != nil {
			return result, err
		}
	}
	baseline, err := os.ReadFile(filepath.Join(root, "testdata/testsession/inventory.json"))
	if err != nil {
		return result, err
	}
	var original sourceInventory
	if err = json.Unmarshal(baseline, &original); err != nil {
		return result, err
	}
	current := map[string]bool{}
	for _, entry := range result.Go {
		current[entry.Package+"/"+entry.Name] = true
	}
	for _, entry := range original.Go {
		if !current[entry.Package+"/"+entry.Name] {
			return result, fmt.Errorf("original test missing from source inventory: %s/%s", entry.Package, entry.Name)
		}
	}
	skipData, err := os.ReadFile(filepath.Join(root, "testdata/testsession/skips.json"))
	if err != nil {
		return result, err
	}
	var skipPolicy map[string][]string
	if err = json.Unmarshal(skipData, &skipPolicy); err != nil {
		return result, err
	}
	for index := range result.Go {
		entry := &result.Go[index]
		entry.SkipReasons = skipPolicy[entry.Package+"/"+entry.Name]
		entry.AllowSkip = len(entry.SkipReasons) > 0
	}
	command := exec.Command("python3", "-B", filepath.Join(root, "tools/testsession/inventory.py"))
	command.Dir = root
	data, err := command.CombinedOutput()
	if err != nil {
		return result, fmt.Errorf("verify original Python and Rust inventory: %w\n%s", err, data)
	}
	var additional sourceInventory
	if err = json.Unmarshal(data, &additional); err != nil {
		return result, err
	}
	result.Python = additional.Python
	result.Rust = additional.Rust
	return result, nil
}

func canonicalManifest(ctx context.Context, root, work string, base []string, ci bool, selection *regexp.Regexp, goRun, scope string) (manifest, error) {
	var result manifest
	inventory, err := discover(root)
	if err != nil {
		return result, err
	}
	data, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return result, err
	}
	file, err := os.Create(filepath.Join(work, "inventory.json"))
	if err != nil {
		return result, err
	}
	_, writeErr := file.Write(data)
	if err = errorsJoin(writeErr, file.Close()); err != nil {
		return result, err
	}
	result.Prepare = []jobSpec{
		{ID: "prepare/frontend", Argv: []string{"go", "build", "-o", filepath.Join(work, "bin/lo"), "./cmd/lo"}},
		{ID: "prepare/filebench", Argv: []string{"go", "build", "-o", filepath.Join(work, "bin/filebench"), "./tools/filebench"}},
		{ID: "prepare/performance-runner", Argv: []string{"go", "build", "-o", filepath.Join(work, "bin/performance-runner"), "./tools/loqualification/performance-runner"}},
	}
	if runtime.GOOS == "darwin" {
		result.Prepare = append(result.Prepare, jobSpec{ID: "prepare/process-launcher", Argv: []string{"python3", "-B", "tools/processbuild/identity.py", "build", "bin/lo-process-launcher", "runtime/native/src/process_helper_identity.rs"}})
	}
	var goPattern *regexp.Regexp
	if goRun != "" {
		goPattern, err = regexp.Compile(strings.Split(goRun, "/")[0])
		if err != nil {
			return result, fmt.Errorf("select Go cases: %w", err)
		}
	}
	packages := map[string][]goCase{}
	for _, entry := range inventory.Go {
		if goPattern != nil && !goPattern.MatchString(entry.Name) {
			continue
		}
		if entry.Configuration == "other-platform" {
			continue
		}
		id := "go/" + strings.TrimPrefix(entry.Package, "./") + "/" + entry.Configuration + "/" + entry.Name
		if !selection.MatchString(id) {
			continue
		}
		packages[entry.Package+"\x00"+entry.Configuration] = append(packages[entry.Package+"\x00"+entry.Configuration], entry)
	}
	keys := []string{}
	for key := range packages {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		entries := packages[key]
		parts := strings.Split(key, "\x00")
		pkg, configuration := parts[0], parts[1]
		binary := filepath.Join(work, "bin", safeName(strings.TrimPrefix(pkg, "./"))+"-"+configuration+".test")
		args := []string{"go", "test", "-c", "-o", binary}
		if configuration == "metal" {
			args = append(args, "-tags=metal")
		}
		args = append(args, pkg)
		preparation := jobSpec{ID: "prepare/" + strings.TrimPrefix(pkg, "./") + "/" + configuration, Argv: args}
		if configuration == "metal" {
			preparation.Environment = map[string]string{"CGO_ENABLED": "1"}
		}
		result.Prepare = append(result.Prepare, preparation)
		for _, entry := range entries {
			id := "go/" + strings.TrimPrefix(pkg, "./") + "/" + configuration + "/" + entry.Name
			group := jobSpec{ID: id, Cwd: filepath.Join(root, strings.TrimPrefix(pkg, "./")), Argv: []string{binary, "-test.v=test2json", "-test.count=1", "-test.failfast", "-test.timeout=240s", "-test.run=^" + regexp.QuoteMeta(entry.Name) + "$"}, Format: "go", Package: "github.com/MarcoPoloResearchLab/Lo/" + strings.TrimPrefix(pkg, "./"), Expected: []string{entry.Name}}
			if entry.AllowSkip {
				group.AllowSkips = map[string][]string{entry.Name: entry.SkipReasons}
			}
			result.Groups = append(result.Groups, group)
		}
	}
	pythonDirectories := map[string]bool{}
	pythonExpected := map[string][]string{}
	for _, record := range inventory.Python {
		var entry struct {
			File string `json:"file"`
			Name string `json:"name"`
		}
		if err = json.Unmarshal(record, &entry); err != nil {
			return result, err
		}
		pythonDirectories[filepath.Dir(entry.File)] = true
		pythonExpected[filepath.Dir(entry.File)] = append(pythonExpected[filepath.Dir(entry.File)], entry.File+"::"+entry.Name)
	}
	directories := []string{}
	for directory := range pythonDirectories {
		directories = append(directories, directory)
	}
	slices.Sort(directories)
	for _, directory := range directories {
		id := "python/" + directory
		if !selection.MatchString(id) {
			continue
		}
		result.Groups = append(result.Groups, jobSpec{ID: id, Argv: []string{"uv", "run", "--offline", "--with", "pytest==8.3.5", "python", "-B", "-m", "pytest", "-p", "no:cacheprovider", "-p", "tools.testsession.pytest_plugin", "--basetemp", filepath.Join(work, "pytest-"+workerName(id)), "-q", "-x", directory}, Format: "pytest", Expected: pythonExpected[directory], Release: []string{filepath.Join(work, "pytest-"+workerName(id))}})
	}
	integrationBinary := filepath.Join(work, "bin/tests_integration-host.test")
	needIntegration := false
	for _, group := range result.Groups {
		if group.ID == "python/tools/semanticcoverage" {
			needIntegration = true
		}
	}
	if needIntegration {
		prepared := false
		for _, entry := range result.Prepare {
			if entry.ID == "prepare/tests/integration/host" {
				prepared = true
			}
		}
		if !prepared {
			result.Prepare = append(result.Prepare, jobSpec{ID: "prepare/tests/integration/host", Argv: []string{"go", "test", "-c", "-o", integrationBinary, "./tests/integration"}})
		}
		for index := range result.Groups {
			if result.Groups[index].ID == "python/tools/semanticcoverage" {
				result.Groups[index].Environment = map[string]string{"LO_TEST_INTEGRATION_BINARY": integrationBinary}
			}
		}
	}
	if selection.MatchString("rust/runtime") {
		rust, err := rustJobs(root, work)
		if err != nil {
			return result, err
		}
		result.Groups = append(result.Groups, rust...)
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		preparation := os.Getenv("LO_NATIVE_PREPARATION")
		if preparation == "" {
			preparation = filepath.Join(root, "_scratch/native-runtime")
		}
		packages := filepath.Join(work, "distributions/packages")
		if selection.MatchString("acceptance/distributions") {
			result.Groups = append(result.Groups, jobSpec{ID: "acceptance/distributions", Argv: []string{"python3", "-B", "tools/lodistribution/acceptance.py", "--darwin", filepath.Join(packages, "lo-darwin-arm64-software"), "--linux", filepath.Join(packages, "lo-linux-arm64-software"), "--metal", filepath.Join(packages, "lo-darwin-arm64-metal"), "--preparation", preparation, "--output", filepath.Join(work, "distribution-acceptance")}})
		}
		if selection.MatchString("acceptance/linux-semantic") {
			linuxEnvironment := map[string]string{"GOOS": "linux", "GOARCH": "arm64", "CGO_ENABLED": "0"}
			for _, entry := range []struct{ name, pkg string }{{"integration.test", "./tests/integration"}, {"fileprobe", "./testdata/fileprobe"}, {"lo-development", "./cmd/lo"}} {
				argv := []string{"go", "build", "-o", filepath.Join(work, "bin/linux-"+entry.name), entry.pkg}
				if entry.name == "integration.test" {
					argv = []string{"go", "test", "-c", "-o", filepath.Join(work, "bin/linux-"+entry.name), entry.pkg}
				}
				result.Prepare = append(result.Prepare, jobSpec{ID: "prepare/linux/" + entry.name, Argv: argv, Environment: linuxEnvironment})
			}
			result.Groups = append(result.Groups, jobSpec{ID: "acceptance/linux-semantic", Argv: []string{"python3", "-B", "tools/loqualification/suite.py", "--preparation", preparation, "--package", filepath.Join(packages, "lo-linux-arm64-software"), "--matrix", "testdata/semantic-migration/matrix.json", "--work", filepath.Join(work, "linux-suite"), "--timeout", "240", "--accelerator", "hvf"}, Environment: map[string]string{"LO_TEST_LINUX_INTEGRATION_BINARY": filepath.Join(work, "bin/linux-integration.test"), "LO_TEST_LINUX_FILEPROBE": filepath.Join(work, "bin/linux-fileprobe"), "LO_TEST_LINUX_COMPILER": filepath.Join(work, "bin/linux-lo-development")}})
		}
		if selection.MatchString("acceptance/semantic-report") {
			result.Finalize = append(result.Finalize, jobSpec{ID: "acceptance/semantic-report", Argv: []string{"python3", "-B", "tools/semanticcoverage/report.py", "--matrix", "testdata/semantic-migration/matrix.json", "--native", "darwin/arm64=" + filepath.Join(work, "semantic-host.jsonl"), "--native", "linux/arm64=" + filepath.Join(work, "linux-suite/semantic-linux-arm64.jsonl"), "--output", filepath.Join(work, "semantic-coverage.json")}})
		}
	}
	needRuntime := selection.MatchString("python/tools/nativeruntime") || selection.MatchString("python/tools/lodistribution") || selection.MatchString("acceptance/distributions") || selection.MatchString("acceptance/linux-semantic")
	if needRuntime {
		runtimeOutput := filepath.Join(work, "native-runtime")
		producer := "tools/nativeruntime/test_prepare.py::test_runtime_build_uses_prepared_host_linker"
		result.Prepare = append(result.Prepare, jobSpec{ID: "prepare/runtime-producer-test", Argv: []string{"uv", "run", "--offline", "--with", "pytest==8.3.5", "python", "-B", "-m", "pytest", "-p", "no:cacheprovider", "-p", "tools.testsession.pytest_plugin", "--basetemp", filepath.Join(work, "producer-tmp"), "-q", producer}, Environment: map[string]string{"LO_TEST_RUNTIME_DISTRIBUTION": runtimeOutput}, Format: "pytest", Expected: []string{producer}})
		for index := range result.Groups {
			entry := &result.Groups[index]
			if entry.ID == "python/tools/nativeruntime" {
				entry.Argv = append(entry.Argv, "--deselect", producer)
				entry.Expected = slices.DeleteFunc(entry.Expected, func(name string) bool { return name == producer })
			}
			if entry.ID == "python/tools/lodistribution" {
				if entry.Environment == nil {
					entry.Environment = map[string]string{}
				}
				entry.Environment["LO_TEST_RUNTIME_DISTRIBUTION"] = runtimeOutput
			}
		}
		if selection.MatchString("acceptance/distributions") || selection.MatchString("acceptance/linux-semantic") {
			result.Prepare = append(result.Prepare, jobSpec{ID: "prepare/compiler-packages", Argv: []string{"python3", "-B", "tools/testsession/distributions.py", "--prepared", filepath.Join(root, "_scratch/compiler-distribution/prepared.json"), "--runtime", runtimeOutput, "--work", filepath.Join(work, "distributions")}})
		}
	}
	if ci || scope != "" && selection.MatchString("static/go-vet") {
		result.Prepare = append([]jobSpec{{ID: "format", Argv: []string{"go", "fmt", "./..."}}}, result.Prepare...)
		result.Groups = append(result.Groups, jobSpec{ID: "static/go-vet", Argv: []string{"go", "vet", "./..."}})
		result.Finalize = append(result.Finalize, jobSpec{ID: "finalize/compiler", Argv: []string{"go", "build", "-o", filepath.Join(root, "bin/lo"), "./cmd/lo"}})
	}
	// Serialize additional package producers behind installed-package checks.
	// Linux semantics consume the separate retained expansion, not acceptance archives.
	if selection.MatchString("acceptance/distributions") {
		for index := range result.Groups {
			entry := &result.Groups[index]
			if entry.ID == "python/tools/lodistribution" {
				entry.After = []string{"acceptance/distributions"}
			}
		}
	}
	// Constructor tests own verified input snapshots; admit one such test at a time.
	previousConstructor := ""
	for index := range result.Groups {
		entry := &result.Groups[index]
		if !strings.HasPrefix(entry.ID, "go/internal/testsession/") &&
			entry.ID != "go/tests/integration/host/TestLLVMRuntimeSessionInputsAndObjectReuse" &&
			entry.ID != "go/tests/integration/host/TestSessionRuntimeFixturesReuseImmutableValues" {
			continue
		}
		if previousConstructor != "" {
			entry.After = append(entry.After, previousConstructor)
		} else if selection.MatchString("acceptance/distributions") {
			entry.After = append(entry.After, "acceptance/distributions")
		}
		previousConstructor = entry.ID
	}
	// Release session intermediates after their last declared consumer.
	packageDirectory := filepath.Join(work, "distributions/packages")
	packageNames := []string{"lo-darwin-arm64-software", "lo-linux-arm64-software", "lo-darwin-arm64-metal"}
	for index := range result.Prepare {
		if result.Prepare[index].ID == "prepare/compiler-packages" {
			result.Prepare[index].Release = []string{filepath.Join(packageDirectory, "lo-darwin-arm64-software"), filepath.Join(packageDirectory, "lo-darwin-arm64-metal"), filepath.Join(work, "distributions/producer-state")}
			if !selection.MatchString("acceptance/distributions") {
				for _, name := range packageNames {
					result.Prepare[index].Release = append(result.Prepare[index].Release, filepath.Join(packageDirectory, name+".tar"), filepath.Join(packageDirectory, name+".receipt.json"))
				}
			}
		}
	}
	for index := range result.Groups {
		entry := &result.Groups[index]
		if entry.ID == "acceptance/distributions" {
			entry.Release = []string{filepath.Join(work, "distribution-acceptance")}
			for _, name := range packageNames {
				entry.Release = append(entry.Release, filepath.Join(packageDirectory, name+".tar"), filepath.Join(packageDirectory, name+".receipt.json"))
			}
		}
		if entry.ID == "acceptance/linux-semantic" && !selection.MatchString("acceptance/semantic-report") {
			entry.Release = []string{filepath.Join(packageDirectory, "lo-linux-arm64-software")}
		}
	}
	for index := range result.Finalize {
		if result.Finalize[index].ID == "acceptance/semantic-report" {
			result.Finalize[index].Release = []string{filepath.Join(packageDirectory, "lo-linux-arm64-software")}
		}
	}
	// Ordering starts broad, expensive groups first while individual cases fill free workers.
	for index := range result.Groups {
		entry := &result.Groups[index]
		if entry.Format == "go" && strings.Contains(entry.ID, "Linux") || entry.ID == "acceptance/linux-semantic" || entry.ID == "acceptance/distributions" || entry.ID == "python/tools/nativeruntime" || entry.ID == "python/tools/loqualification" {
			entry.Resource = resourceGuest
		} else if entry.Format == "go" && strings.Contains(entry.ID, "/host/") && len(entry.Expected) == 1 && strings.HasPrefix(entry.Expected[0], "TestLLVM") && strings.Contains(entry.Expected[0], "HTTP") {
			entry.Resource = resourceHostHTTP
		}
	}
	slices.SortStableFunc(result.Groups, func(a, b jobSpec) int { return weight(b) - weight(a) })
	return result, nil
}

func errorsJoin(first, second error) error {
	if first != nil {
		return first
	}
	return second
}
func weight(entry jobSpec) int {
	if entry.ID == "acceptance/distributions" {
		return 200
	}
	if entry.ID == "acceptance/linux-semantic" {
		return 180
	}
	if entry.ID == "go/tests/integration/host/TestLLVMF034TreeLifecycle" || entry.ID == "go/tests/integration/host/TestF036InjectedDescendantOwnership" {
		return 110
	}
	if entry.Format == "pytest" || entry.Format == "rust" {
		return 100
	}
	if strings.HasPrefix(entry.ID, "go/tools/filebench/") {
		return 95
	}
	switch entry.ID {
	case "go/tests/integration/host/TestMacCleanupCompleteDockerBoundary",
		"go/tests/integration/host/TestMacCleanupCompleteArgumentBoundary",
		"go/tests/integration/host/TestMacCleanupCacheProbePolicies",
		"go/tests/integration/host/TestMacCleanupCompleteTemporaryBoundary":
		return 95
	}
	if strings.Contains(entry.ID, "F034") || strings.Contains(entry.ID, "F036") || strings.Contains(entry.ID, "AutodiffAnalyticalAndFiniteDifferences") {
		return 90
	}
	if strings.Contains(entry.ID, "Linux") {
		return 80
	}
	if strings.Contains(entry.ID, "LLVM") {
		return 50
	}
	return 10
}

func rustJobs(root, work string) ([]jobSpec, error) {
	preparation := os.Getenv("LO_NATIVE_PREPARATION")
	if preparation == "" {
		preparation = filepath.Join(root, "_scratch/native-runtime")
	}
	compiler := filepath.Join(preparation, "rust-prefix/bin/rustc")
	cargo := filepath.Join(preparation, "rust-prefix/bin/cargo")
	if _, err := os.Stat(compiler); err != nil {
		return nil, fmt.Errorf("runtime tests require prepared Rust compiler: %w", err)
	}
	_ = cargo
	return []jobSpec{{ID: "rust/runtime", Argv: []string{"python3", "-B", "tools/testsession/runtime_tests.py", "--preparation", preparation, "--work", filepath.Join(work, "runtime-tests")}, Format: "pytest"}}, nil
}

func commandOutput(ctx context.Context, root string, base []string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, arguments[0], arguments[1:]...)
	command.Dir = root
	command.Env = base
	var stderr bytes.Buffer
	command.Stderr = &stderr
	data, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w\n%s", strings.Join(arguments, " "), err, &stderr)
	}
	return data, nil
}
