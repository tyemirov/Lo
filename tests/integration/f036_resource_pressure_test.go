package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestF036ResourceTransferBackpressure(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Darwin descriptor transfer and audit-token process fixtures")
	}
	for number, name := range []string{"adopt-before-next-resource", "adoption-failure-releases-transferred-resource", "wrong-resource-ack-index", "descriptor-bearing-resource-ack"} {
		t.Run(name, func(t *testing.T) {
			rejected, malformed := number == 1, number >= 2
			fixture := newF036Fixture(t)
			trace := fileFixture(t, fixture.root, "resource-pressure", "")
			f036ConfinementDiagnostics(t, fixture)
			t.Cleanup(func() {
				if !t.Failed() {
					return
				}
				file, err := os.Open(trace)
				if err != nil {
					t.Log(err)
					return
				}
				data, _ := io.ReadAll(io.LimitReader(file, 32768))
				_ = file.Close()
				t.Logf("bounded resource-transfer evidence: %q", data)
			})
			flags := []string{"--process-kernel-limit", "4096"}
			for index := 0; index < 8; index++ {
				alias := filepath.Join(fixture.root, fmt.Sprintf("registered-%02d", index))
				if err := os.Link(fixture.command, alias); err != nil {
					t.Fatal(err)
				}
				flags = append(flags, "--process-allow", strings.SplitN(fixture.grant, ",", 2)[0]+","+alias)
			}
			provider, err := os.ReadFile(filepath.Join(repository, "testdata/native-runtime/process-ownership-provider.c"))
			if err != nil {
				t.Fatal(err)
			}
			const bootstrap = "int lo_test_host_init(int argc,char **argv,char **environment) {"
			if strings.Count(string(provider), bootstrap) != 1 {
				t.Fatal("provider bootstrap seam changed")
			}
			provider = []byte(strings.Replace(string(provider), bootstrap, bootstrap+"\n    pressure_bootstrap(argc, argv);", 1))
			// Keep the underlying event source on its ordinary successful case.
			provider = []byte(strings.ReplaceAll(string(provider), "LO_CASE", "0"))
			source := f036ConfinementSource(fixture, fixture.command, "finish")
			probes := ""
			if malformed {
				later := fileFixture(t, fixture.root, "later-lock", "")
				flags = append(flags, "--lock-file", later)
				source = fmt.Sprintf(`flow ProbeLater()->Bool onError Denied{target=Lock.File.new(File.Path.new(%q));lease=commit target;return false}
flow ProbeSame()->Bool onError Denied{target=Lock.File.new(File.Path.new(%q));lease=commit target;return false}
flow Denied(error:Error)->Bool{return Match.new(String.eq(error.kind,"Lock.Failed"),true:String.starts_with(error.message,"file lock acquisition is frozen by active remote owner "),_:false)}
`, later, fixture.lock) + source
				data, err := os.ReadFile(filepath.Join(repository, "testdata/native-runtime/process-resource-ack-probes.c"))
				if err != nil {
					t.Fatal(err)
				}
				probes = string(data)
				const main = "int main(int argc,char **argv,char **environment)"
				if strings.Count(string(provider), main) != 1 {
					t.Fatal("provider main seam changed")
				}
				provider = []byte(strings.Replace(string(provider), main, "int pressure_provider_main(int argc,char **argv,char **environment)", 1))
			}
			runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "public-cli", source: source, number: number,
				harness: "testdata/native-runtime/process-resource-pressure.c", hostBootstrap: "lo_test_host_init", sdkHarness: true,
				transform: func(t *testing.T, module []byte) []byte {
					if !malformed {
						return module
					}
					const entry = "define void @lo_entry("
					if strings.Count(string(module), entry) != 1 {
						t.Fatal("unique compiled entry missing")
					}
					return []byte(strings.Replace(string(module), entry, "define void @lo_resource_pressure_entry(", 1))
				},
				harnessTransform: func(t *testing.T, source []byte) []byte {
					return fixture.transform(t, []byte(strings.NewReplacer("@F036_PRESSURE_LOG@", strconv.Quote(trace),
						"@F036_PRESSURE_PROVIDER@", string(provider), "@F036_ACK_PROBES@", probes).Replace(string(source))))
				}, run: func(t *testing.T, artifact, work string) {
					actual := f036RunPolicyFixture(t, fixture, artifact, flags...)
					if rejected || malformed {
						expectedStatus := 2
						if malformed {
							expectedStatus = 0 // Wrapper verifies shared-session lock denial before CLI disposal.
						}
						nativeFailure(t, actual, expectedStatus, "runtime.host-failure", "")
						var document struct {
							Diagnostics []struct {
								Progress runtimeValue `json:"process_progress"`
							} `json:"diagnostics"`
						}
						if err := json.Unmarshal([]byte(actual.stderr), &document); err != nil || len(document.Diagnostics) != 1 {
							t.Fatalf("resource failure diagnostic: %v: %+v", err, actual)
						}
						fields := processFields(t, document.Diagnostics[0].Progress)
						valueJSON(t, fields["cleanup_complete"], strconv.FormatBool(!malformed))
						valueJSON(t, fields["start_confirmed"], `false`)
						if rejected {
							fixture.wait(t, "rejected resource lock release", func() bool { return !fixture.lockBusy(t) })
						}
						if fixture.logged("O\n") {
							t.Fatal("failed resource adoption opened the process provider")
						}
						macCleanupPresent(t, fixture.ready, false)
					} else {
						f036ConfinementComplete(t, fixture, actual)
						macCleanupPresent(t, fixture.ready, true)
					}
					data, err := os.ReadFile(trace)
					if err != nil || len(data) > 32768 {
						t.Fatalf("resource-transfer trace: %v, bytes=%d", err, len(data))
					}
					text := string(data)
					boundary := strings.Index(text, "first-resource-reader-resumed\n")
					if boundary < 0 || !strings.Contains(text, "send-buffer-bounded\n") || strings.Count(text[:boundary], "resource-sent\n") != 1 {
						t.Fatalf("more than one resource was sent before receiver adoption: %q", text)
					}
					if malformed {
						if strings.Count(text, "resource-sent\n") != 1 || !strings.Contains(text, name+"\n") || !strings.Contains(text, "resource-ack-locks-frozen\n") {
							t.Fatalf("malformed ACK did not stop transfer and preserve session freeze: %q", text)
						}
					} else if rejected {
						if strings.Count(text, "resource-sent\n") != 1 || !strings.Contains(text, "resource-adoption-rejected\n") || !strings.Contains(text, "received-resource-closed\n") {
							t.Fatalf("rejected adoption did not stop transfer and close its descriptor: %q", text)
						}
					} else if sent, received := strings.Count(text, "resource-sent\n"), strings.Count(text, "resource-received\n"); sent < 24 || sent != received {
						t.Fatalf("resource transfer incomplete: sent=%d received=%d", sent, received)
					}
				}}})
		})
	}
}
