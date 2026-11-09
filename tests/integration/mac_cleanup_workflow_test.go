package integration_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func cleanupApplication(t *testing.T, entry string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repository, "docs/examples/mac-cleanup.lo"))
	if err != nil {
		t.Fatal(err)
	}
	const terminal = "return Main()"
	text := strings.TrimSpace(string(data))
	if !strings.HasSuffix(text, terminal) {
		t.Fatal("canonical cleanup source has no Main entry")
	}
	return strings.TrimSuffix(text, terminal) + "return " + entry + "\n"
}

func TestMacCleanupCompleteSourceChecks(t *testing.T) {
	path := filepath.Join(repository, "docs/examples/mac-cleanup.lo")
	got := run(t, "check", path, "--format", "json")
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, `"result_type":"Program.Exit!"`) {
		t.Fatalf("complete canonical application: %+v", got)
	}
}

func TestMacCleanupCompleteArgumentBoundary(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		code int
		text string
	}{
		{"help", []string{"--help"}, 0, "mac-cleanup"},
		{"unknown", []string{"--unknown"}, 2, "declared option parsing failed"},
		{"conflicting-modes", []string{"--dry-run", "--report"}, 2, "declared option parsing failed"},
		{"literal-shell-input", []string{"$(touch should-not-exist)"}, 2, "declared option parsing failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := sourceFile(t, cleanupApplication(t, "Main()"))
			arguments := append([]string{"--"}, test.args...)
			got := runProgram(t, source, "run", arguments...)
			if got.status != test.code || !strings.Contains(strings.ToLower(got.stdout+got.stderr), strings.ToLower(test.text)) {
				t.Fatalf("application argument boundary: %+v", got)
			}
			if strings.Contains(got.stdout, "Starting mac-cleanup") {
				t.Fatalf("argument handling entered the workflow: %+v", got)
			}
		})
	}
}

var cleanupWorkflowCacheRoots = []string{
	"Library/Caches/staticcheck", "Library/Caches/gopls", "Library/Caches/goimports",
	"Library/Caches/Homebrew/downloads", ".cache/gh",
}

var cleanupWorkflowReportRoots = []string{
	"Library/Caches/go-build", "Library/Caches/Google", "Library/Caches/Firefox",
	"Library/Caches/Codex", "Library/Caches/com.openai.codex", "Library/Caches/Codex-codexw",
	"Library/Caches/lo", "Library/Caches/ms-playwright", "Library/Caches/lima",
	"Library/Developer/Xcode/DerivedData", "Library/Caches/node-gyp", "Library/Caches/CocoaPods",
	"Library/Caches/ReactNative", ".cache/zig", ".npm/_npx", "Library/Developer/CoreSimulator",
	".gradle/caches", ".cache/huggingface", ".lmstudio", ".ollama", ".cache/whisper",
	".cache/puppeteer", ".cache/codex-runtimes", ".cache/mprlab-gateway",
	"Library/Containers/com.docker.docker/Data/vms",
}

type cleanupWorkflowFixture struct {
	cleanupFixture
	calls, temporary, temporaryPrefix, shared, derived string
	tools                                              map[string]string
	overrides                                          [][2]string
	extraFlags                                         []string
	expectedVolumes                                    *int
	busy                                               bool
	direct, recursive, removedTrees, preserved         []string
}

func cleanupWorkflowDirectory(t *testing.T, parent, relative string) string {
	t.Helper()
	for _, component := range strings.Split(relative, "/") {
		next := filepath.Join(parent, component)
		if _, err := os.Stat(next); os.IsNotExist(err) {
			f033Directory(t, parent, component)
		} else if err != nil {
			t.Fatal(err)
		}
		parent = next
	}
	return parent
}

func newCleanupWorkflowFixture(t *testing.T) cleanupWorkflowFixture {
	t.Helper()
	base := newCleanupFixtureSource(t, "mac-cleanup-command.c")
	fixture := cleanupWorkflowFixture{cleanupFixture: base, tools: map[string]string{}}
	fixture.calls = fileFixture(t, base.prunes, "calls", "")
	for _, tool := range []string{"npm", "go", "uv", "pnpm", "python3", "getconf", "du", "lsof", "docker"} {
		path := filepath.Join(base.root, tool)
		if err := os.Link(base.executable, path); err != nil {
			t.Fatal(err)
		}
		fixture.tools[tool] = path
	}
	fixture.shared = f033Directory(t, base.root, "shared-tmp")
	fixture.temporaryPrefix = f033Directory(t, base.root, "user-temp")
	fixture.temporary = cleanupWorkflowDirectory(t, fixture.temporaryPrefix, "account/T")
	for _, relative := range append(append([]string{}, cleanupWorkflowCacheRoots...), cleanupWorkflowReportRoots...) {
		cleanupWorkflowDirectory(t, base.writes, relative)
	}
	for _, relative := range cleanupWorkflowCacheRoots {
		root := filepath.Join(base.writes, relative)
		nested := f033Directory(t, root, "nested")
		fixture.recursive = append(fixture.recursive, fileFixture(t, nested, "old.cache", "old cache"))
		fixture.preserved = append(fixture.preserved, root, nested)
	}
	logs := cleanupWorkflowDirectory(t, base.writes, ".npm/_logs")
	fixture.direct = append(fixture.direct, fileFixture(t, logs, "old.log", "old npm log"))
	fixture.preserved = append(fixture.preserved, fileFixture(t, logs, "unselected.txt", "preserved"))
	for _, root := range []string{fixture.shared, fixture.temporary} {
		fixture.direct = append(fixture.direct, fileFixture(t, root, "old.tmp", "temporary"))
		fixture.preserved = append(fixture.preserved, fileFixture(t, root, "unselected.txt", "preserved"))
	}
	fixture.derived = filepath.Join(base.writes, "Library/Developer/Xcode/DerivedData")
	shared := f033Directory(t, fixture.derived, "ModuleCache.noindex")
	project := f033Directory(t, fixture.derived, "Project")
	build := f033Directory(t, project, "Build")
	fileFixture(t, shared, "module.pcm", "module")
	fileFixture(t, build, "main.o", "object")
	fixture.removedTrees = []string{shared, build}
	packages := f033Directory(t, project, "SourcePackages")
	fixture.preserved = append(fixture.preserved, fileFixture(t, packages, "package", "source package"), project, packages)
	gateway := cleanupWorkflowDirectory(t, base.writes, "Library/Caches/mprlab-gateway/deployments")
	fixture.direct = append(fixture.direct, fileFixture(t, gateway, "deployment-123.log", "old deployment log"))
	fixture.preserved = append(fixture.preserved, fileFixture(t, gateway, "state.json", "operator state"))
	return fixture
}

func (fixture cleanupWorkflowFixture) source(t *testing.T) string {
	t.Helper()
	entry := fmt.Sprintf(`MainWithSettings(Record.new(shared_temp: File.Path.new(%q), user_temp_prefix: %q, getconf: File.Path.new(%q), du: File.Path.new(%q)))`,
		fixture.shared, fixture.temporaryPrefix+"/", fixture.tools["getconf"], fixture.tools["du"])
	return cleanupApplication(t, entry)
}

func (fixture cleanupWorkflowFixture) flags(t *testing.T, mode string) []string {
	t.Helper()
	flags := []string{"--lock-entry", "--max-steps", "10000000", "--list-root", fixture.writes}
	listed := map[string]bool{fixture.writes: true}
	addList := func(path string) {
		if !listed[path] {
			flags = append(flags, "--list-root", path)
			listed[path] = true
		}
	}
	for _, path := range []string{filepath.Join(fixture.writes, ".npm/_logs"), fixture.shared, fixture.temporary, filepath.Join(fixture.writes, "Library/Caches/mprlab-gateway/deployments")} {
		addList(path)
		if mode == "apply" {
			flags = append(flags, "--remove-root", path)
		}
	}
	for _, relative := range append(append([]string{}, cleanupWorkflowCacheRoots...), cleanupWorkflowReportRoots...) {
		addList(filepath.Dir(filepath.Join(fixture.writes, relative)))
	}
	for _, relative := range cleanupWorkflowCacheRoots {
		path := filepath.Join(fixture.writes, relative)
		flags = append(flags, "--observe-tree-root", path)
		if mode == "apply" {
			flags = append(flags, "--remove-tree-root", path)
		}
	}
	flags = append(flags, "--observe-tree-root", fixture.derived)
	if mode == "apply" {
		flags = append(flags, "--remove-tree-root", fixture.derived)
	}
	if mode != "preview" {
		flags = append(flags, "--process-launcher", filepath.Join(repository, "bin/lo-process-launcher"), "--process-read-root", fixture.root,
			"--max-process-ms", "1800000", "--process-forward-output")
		for _, name := range []string{"npm", "go", "uv", "pnpm", "python3", "getconf", "du", "lsof", "docker"} {
			if mode == "report" && name != "du" {
				continue
			}
			path := fixture.tools[name]
			if path == "" {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			flags = append(flags, "--process-allow", fmt.Sprintf("%x,%s", sha256.Sum256(data), path),
				"--process-write-root", cleanupScope(t, path, "root", fixture.prunes))
		}
	}
	flags = append(flags, fixture.extraFlags...)
	if mode == "preview" {
		flags = append(flags, "--", "--dry-run")
	} else if mode == "report" {
		flags = append(flags, "--", "--report")
	}
	return flags
}

func (fixture cleanupWorkflowFixture) scenario(t *testing.T, mode, behavior string, rootUID bool, verify func(*testing.T, result)) llvmRuntimeScenario {
	t.Helper()
	arguments := append([]string{"fixture", "run"}, fixture.flags(t, mode)...)
	quoted := make([]string, len(arguments))
	for index, argument := range arguments {
		quoted[index] = fmt.Sprintf("%q", argument)
	}
	environment := [][2]string{
		{"HOME", fixture.writes}, {"PATH", fixture.root}, {"FIXTURE_CALLS", fixture.calls},
		{"FIXTURE_BEHAVIOR", behavior}, {"FIXTURE_USER_TEMP", fixture.temporary + "/"},
		{"FIXTURE_DOCKER_JSON", `[{"Name":"desktop-linux","Endpoints":{"docker":{"Host":"unix:///fixture/docker.sock"}}}]`},
		{"FIXTURE_DOCKER_BUILDER", "Name: desktop-linux\nDriver: docker\nEndpoint: desktop-linux\n"},
	}
	for _, override := range fixture.overrides {
		replaced := false
		for index, item := range environment {
			if item[0] == override[0] {
				environment[index], replaced = override, true
			}
		}
		if !replaced {
			environment = append(environment, override)
		}
	}
	encoded := make([]string, len(environment))
	for index, item := range environment {
		encoded[index] = fmt.Sprintf("{%q,%q}", item[0], item[1])
	}
	volumes := 2
	if mode == "report" {
		volumes = 1
	}
	uid := 0
	if rootUID {
		uid, volumes = 1, 0
	}
	if fixture.expectedVolumes != nil {
		volumes = *fixture.expectedVolumes
	}
	return llvmRuntimeScenario{
		name: mode + "-" + behavior, source: fixture.source(t), timeout: 45 * time.Second,
		harness: "testdata/native-runtime/mac-cleanup-workflow.c",
		transform: func(t *testing.T, module []byte) []byte {
			t.Helper()
			const entry = "define void @lo_entry("
			if strings.Count(string(module), entry) != 1 {
				t.Fatal("missing unique complete application entry")
			}
			return []byte(strings.Replace(string(module), entry, "define void @lo_cleanup_workflow_entry(", 1))
		},
		harnessTransform: func(t *testing.T, data []byte) []byte {
			return []byte(strings.NewReplacer(
				"LO_FIXTURE_ARGUMENTS", strings.Join(quoted, ","),
				"LO_FIXTURE_ENVIRONMENT", strings.Join(encoded, ","),
				"LO_FIXTURE_DIRECTORY", fmt.Sprintf("%q", fixture.root),
				"LO_VOLUME_CALLS", fmt.Sprint(volumes), "LO_ROOT_UID", fmt.Sprint(uid),
				"LO_LOCK_BUSY", fmt.Sprint(map[bool]int{true: 1, false: 0}[fixture.busy]),
				"LO_TEMP_PATH", fmt.Sprintf("%q", fixture.temporary),
			).Replace(string(data)))
		}, verify: verify,
	}
}

func TestMacCleanupCompleteWorkflow(t *testing.T) {
	for _, test := range []struct{ mode, behavior string }{
		{"preview", "success"}, {"report", "success"}, {"apply", "success"},
		{"apply", "npm-failure"}, {"apply", "pip-absent"}, {"apply", "builder-multiline"},
	} {
		t.Run(test.mode+"-"+test.behavior, func(t *testing.T) {
			fixture := newCleanupWorkflowFixture(t)
			if test.behavior == "builder-multiline" {
				fixture.overrides = [][2]string{{"FIXTURE_DOCKER_BUILDER", "Driver:\r\n docker\r\nEndpoint:\r\n desktop-linux\r\n"}}
			}
			scenario := fixture.scenario(t, test.mode, test.behavior, false, func(t *testing.T, actual result) {
				wantStatus := 0
				if test.behavior == "npm-failure" {
					wantStatus = 1
				}
				if actual.status != wantStatus {
					t.Fatalf("complete workflow: %+v", actual)
				}
				calls, err := os.ReadFile(fixture.calls)
				if err != nil {
					t.Fatal(err)
				}
				for _, path := range fixture.preserved {
					macCleanupPresent(t, path, true)
				}
				for _, paths := range [][]string{fixture.direct, fixture.recursive, fixture.removedTrees} {
					for _, path := range paths {
						macCleanupPresent(t, path, test.mode != "apply")
					}
				}
				switch test.mode {
				case "preview":
					if len(calls) != 0 || !strings.Contains(actual.stdout, "Preview complete; nothing deleted") || !strings.Contains(actual.stdout, "per-user temporary") {
						t.Fatalf("preview process boundary: calls=%q result=%+v", calls, actual)
					}
				case "report":
					if !strings.HasPrefix(string(calls), "du\t-k\t-s\t") || strings.Count(string(calls), "\n") != 1 || !strings.Contains(actual.stdout, "Storage inventory (allocated size; NOT an estimate of reclaimable space):") {
						t.Fatalf("report process boundary: calls=%q result=%+v", calls, actual)
					}
				case "apply":
					prefix := "python3\t-m\tpip\t--version\nnpm\tcache\tclean\t--force\ngo\tclean\t-cache\t-testcache\nuv\tcache\tclean\t--force\npnpm\tstore\tprune\t--yes\n"
					if !strings.HasPrefix(string(calls), prefix) || !strings.Contains(string(calls), "docker\t--context\tdesktop-linux\timage\tprune\t--all\t--force\n") {
						t.Fatalf("command sequence: %q; result=%+v", calls, actual)
					}
					if !strings.Contains(actual.stdout, "go fixture complete") || !strings.Contains(actual.stdout, "uv fixture complete") || strings.Contains(actual.stdout, "pip fixture") {
						t.Fatalf("command output policy: %+v", actual)
					}
					purgedPip := strings.Contains(string(calls), "python3\t-m\tpip\tcache\tpurge\n")
					if purgedPip != (test.behavior != "pip-absent") {
						t.Fatalf("optional pip command: %q", calls)
					}
					if test.behavior == "npm-failure" && !strings.Contains(actual.stderr, "Failed categories: npm") {
						t.Fatalf("ordinary failure lost its label: %+v", actual)
					}
				}
				if test.mode != "report" && !strings.Contains(actual.stdout, "net volume change: 2.00 GiB") {
					t.Fatalf("fresh final volume observation: %+v", actual)
				}
			})
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
		})
	}
}

func TestMacCleanupCompleteRootUIDRejected(t *testing.T) {
	fixture := newCleanupWorkflowFixture(t)
	scenario := fixture.scenario(t, "preview", "success", true, func(t *testing.T, actual result) {
		if actual.status != 2 || !strings.Contains(actual.stderr, "without sudo") || strings.Contains(actual.stdout, "Starting mac-cleanup") {
			t.Fatalf("root UID boundary: %+v", actual)
		}
		calls, err := os.ReadFile(fixture.calls)
		if err != nil || len(calls) != 0 {
			t.Fatalf("root UID rejection started a process: %q, %v", calls, err)
		}
	})
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{scenario})
}
