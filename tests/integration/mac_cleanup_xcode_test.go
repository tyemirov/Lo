package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacCleanupXcodeRetainedTargets(t *testing.T) {
	for scenario, name := range []string{"preview", "apply", "ordinary-failure-continues", "partial-failure-continues"} {
		t.Run(name, func(t *testing.T) {
			fixture := newMacCleanupFixture(t)
			parent := fixture.root
			for _, component := range []string{"Library", "Developer", "Xcode", "DerivedData"} {
				parent = f033Directory(t, parent, component)
			}
			fixture.writes = parent
			shared := f033Directory(t, parent, "ModuleCache.noindex")
			const removedPayload = "first"
			var removedPrefix string
			if scenario == 3 {
				removedPrefix = fileFixture(t, shared, "a.pcm", removedPayload)
			}
			sharedFile := fileFixture(t, shared, "module.pcm", "shared")
			project := f033Directory(t, parent, "ZProject")
			build := f033Directory(t, project, "Build")
			buildFile := fileFixture(t, build, "main.o", "object")
			packages := f033Directory(t, project, "SourcePackages")
			packageFile := fileFixture(t, packages, "package", "preserved")
			projectSibling := fileFixture(t, project, "index", "preserved")
			foreign := f033Directory(t, parent, "ForeignProject")
			foreignBuild := f033Directory(t, foreign, "Build")
			foreignFile := fileFixture(t, foreignBuild, "foreign.o", "preserved")
			linkProject := f033Directory(t, parent, "LinkProject")
			linkedBuild := filepath.Join(linkProject, "Build")
			if err := os.Symlink(fixture.prunes, linkedBuild); err != nil {
				t.Fatal(err)
			}
			f033Directory(t, parent, "EmptyProject")
			outside := fileFixture(t, fixture.prunes, "outside.o", "preserved")
			apply := scenario != 0
			behavior := "success"
			if scenario == 2 {
				behavior = "first-fails"
			}
			source := cleanupFlows(t, "Xcode", "InitialWorkflow", "JoinStrings") + fmt.Sprintf(`
flow XcodeFixture() -> Program.Exit! {
    lock = Lock.File.new(Program.path())
    lease = commit lock
    environment = Map.new(Record.new(key: "PATH", value: %q), Record.new(key: "FIXTURE_CALLS", value: %q), Record.new(key: "FIXTURE_BEHAVIOR", value: %q), Record.new(key: "FIXTURE_OPENED", value: ""), Record.new(key: "FIXTURE_CHANGED", value: ""))
    process = Process.Context.new(environment, File.Path.new(%q))
    context = Record.new(apply: %t, home: File.Path.new(%q), uid: %d, process: process)
    state = Xcode(context, InitialWorkflow())
    return Program.Exit.new(Match.new(List.length(state.failures), 0: 0, _: 1), UTF8.Bytes.new(String.concat("FAILURES ", JoinStrings(state.failures, ","), "\n")), Bytes.new(""))
}
return XcodeFixture()`, fixture.root, fixture.calls, behavior, fixture.root, apply, fixture.root, os.Getuid())
			arguments := append([]string{"fixture", "run"}, fixture.treeFlags(t, apply, parent)...)
			quoted := make([]string, len(arguments))
			for index, argument := range arguments {
				quoted[index] = fmt.Sprintf("%q", argument)
			}
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{
				name: name, number: scenario, source: source,
				harness: "testdata/native-runtime/mac-cleanup-tree-observer.c",
				transform: func(t *testing.T, module []byte) []byte {
					t.Helper()
					const entry = "define void @lo_entry("
					if strings.Count(string(module), entry) != 1 {
						t.Fatal("missing unique Xcode fixture entry")
					}
					return []byte(strings.Replace(string(module), entry, "define void @lo_cleanup_tree_entry(", 1))
				},
				harnessTransform: func(t *testing.T, data []byte) []byte {
					t.Helper()
					return []byte(strings.NewReplacer("LO_FOREIGN_PATH", fmt.Sprintf("%q", foreign), "LO_SHARED_PATH", fmt.Sprintf("%q", shared), "LO_FIXTURE_ARGUMENTS", strings.Join(quoted, ", ")).Replace(string(data)))
				},
				verify: func(t *testing.T, actual result) {
					t.Helper()
					if actual.status != 0 || actual.stderr != "" {
						t.Fatalf("Xcode retained targets: %+v", actual)
					}
					macCleanupPresent(t, sharedFile, scenario != 1)
					macCleanupPresent(t, shared, scenario != 1)
					if scenario == 3 {
						macCleanupPresent(t, removedPrefix, false)
						contents, err := os.ReadFile(sharedFile)
						if err != nil || string(contents) != "shared" {
							t.Fatalf("partial failure changed the retained suffix: %q, %v", contents, err)
						}
					}
					macCleanupPresent(t, buildFile, !apply)
					macCleanupPresent(t, build, !apply)
					for _, path := range []string{parent, project, packages, packageFile, projectSibling, foreign, foreignBuild, foreignFile, linkedBuild, outside} {
						macCleanupPresent(t, path, true)
					}
					calls, err := os.ReadFile(fixture.calls)
					if err != nil {
						t.Fatal(err)
					}
					if apply {
						want := "-F0n\n+D\n" + shared + "\n-F0n\n+D\n" + build + "\n"
						if string(calls) != want {
							t.Fatalf("Xcode target/probe order: %q, want %q", calls, want)
						}
					} else if len(calls) != 0 {
						t.Fatalf("Xcode preview started probes: %q", calls)
					}
					if scenario == 2 || scenario == 3 {
						if !strings.Contains(actual.stdout, "FAILED "+shared+":") || !strings.Contains(actual.stdout, "FAILURES "+shared+"\n") || !strings.Contains(actual.stdout, "Deleted whole build tree: "+build) {
							t.Fatalf("tree failure lost its label or stopped the later tree: %+v", actual)
						}
						if strings.Contains(actual.stdout, "Deleted whole build tree: "+shared) {
							t.Fatalf("failed tree was reported as complete: %+v", actual)
						}
						if scenario == 3 {
							progress := fmt.Sprintf("tree removal: completed 1 of 3 entries, %d logical bytes; stage remove; entry \"module.pcm\"", len(removedPayload))
							if !strings.Contains(actual.stdout, progress) || !strings.Contains(actual.stdout, "input/output error") {
								t.Fatalf("AttemptTree lost confirmed partial removal evidence: %+v", actual)
							}
						}
					} else if !strings.Contains(actual.stdout, "FAILURES \n") {
						t.Fatalf("successful Xcode fixture: %+v", actual)
					}
				},
			}})
		})
	}
}
