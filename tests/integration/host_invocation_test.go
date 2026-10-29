package integration_test

import (
	"context"
	encodingbinary "encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func TestHostInvocationArguments(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments []string
		expected  string
	}{
		{"absent", nil, `[]`},
		{"literals", []string{"--", "literal argument", "--flag", ""}, `[{"type":"String","value":"literal argument"},{"type":"String","value":"--flag"},{"type":"String","value":""}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := `flow Main() -> List<String>! { initial = Host.Invocation.current(); return initial.arguments }; return Main()`
			arguments := []string{"run", sourceFile(t, source), "--format", "json"}
			arguments = append(arguments, test.arguments...)
			valueJSON(t, fileOutput(t, run(t, arguments...)), test.expected)
		})
	}
}

func TestHostInvocationCaptureAndRedaction(t *testing.T) {
	const privateArgument = "f025-private-program-argument"
	home := fileFixtureRoot(t)
	t.Setenv("HOME", home)
	source := `flow Main() -> Record{same_clock: Bool, same_uid: Bool, snapshot: Host.Invocation, nested: List<Host.Invocation>}! { first = Host.Invocation.current(); second = Host.Invocation.current(); return Record.new(same_clock: Math.eq(first.now_unix_ns, second.now_unix_ns), same_uid: Math.eq(first.real_uid, second.real_uid), snapshot: first, nested: List.new(first)) }; return Main()`
	got := run(t, "run", sourceFile(t, source), "--format", "json", "--", privateArgument)
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("host invocation output: %+v", got)
	}
	if strings.Contains(got.stdout, home) || strings.Contains(got.stdout, privateArgument) {
		t.Fatalf("host invocation exposed private captured inputs: %s", got.stdout)
	}
	fields := processFields(t, fileOutput(t, got))
	valueJSON(t, fields["same_clock"], `true`)
	valueJSON(t, fields["same_uid"], `true`)
	explicit := `flow Main() -> File.Path! { initial = Host.Invocation.current(); return initial.home }; return Main()`
	valueJSON(t, fileOutput(t, run(t, "run", sourceFile(t, explicit), "--format", "json")), fmt.Sprintf(`%q`, home))
}

func TestHostInvocationDeferredCaptureFailure(t *testing.T) {
	for _, test := range []struct {
		name string
		home string
	}{
		{"relative", "relative-private-home"}, {"empty", ""}, {"missing", ""}, {"invalid-utf8", string([]byte{'/', 255})},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HOME", test.home)
			if test.name == "missing" {
				if err := os.Unsetenv("HOME"); err != nil {
					t.Fatal(err)
				}
			}
			valueJSON(t, fileOutput(t, run(t, "run", sourceFile(t, `return "ordinary"`), "--format", "json")), `"ordinary"`)
			processConstructorFailure(t, `return Host.Invocation.current()`, "Host.InvocationFailed")
		})
	}
}

func TestHostInvocationArgumentAdmission(t *testing.T) {
	source := sourceFile(t, `return "ordinary"`)
	got := run(t, "run", source, "--format", "json", "--", strings.Repeat("x", 65536))
	if got.status != 2 {
		t.Fatalf("oversized program argument must fail admission: %+v", got)
	}
}

func TestHostInvocationObservationPurity(t *testing.T) {
	source := `flow Observe(index: Int) -> Host.Invocation! { return Host.Invocation.current() }; return List.map(List.new(1), Observe)`
	got := run(t, "check", sourceFile(t, source), "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, "check.effectful-callback") {
		t.Fatalf("host observation in pure callback: %+v", got)
	}
}

func hostFixtureInvocation(t *testing.T, home string, now int64) value.HostInvocation {
	t.Helper()
	path, err := value.NewText(types.Path(), home)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := value.NewHostInvocation([]string{"literal", ""}, path, value.Int64(123), value.Int64(456), value.Int64(now))
	if err != nil {
		t.Fatal(err)
	}
	return initial
}

func TestHostInvocationInjectedLifetime(t *testing.T) {
	root := fileFixtureRoot(t)
	initial := hostFixtureInvocation(t, root, -17)
	ctx, err := execution.WithHostInvocation(context.Background(), initial)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execution.WithHostInvocation(context.Background(), value.Snapshot(initial).(value.HostInvocation)); err == nil {
		t.Fatal("diagnostic snapshot was admitted as validated host input")
	}
	ctx = execution.CaptureHostInvocation(ctx, []string{"ignored"})
	t.Setenv("HOME", fileFixtureRoot(t))
	files, err := adapter.OpenFiles(adapter.FileRoots{}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := files.Close(); err != nil {
			t.Error(err)
		}
	})
	program := processCompositionProgram(t, root, `flow Main()->Record{clock:Int,uid:Int,home:File.Path,count:Int}!{first=Host.Invocation.current();second=Host.Invocation.current();return Record.new(clock:second.now_unix_ns,uid:first.real_uid,home:second.home,count:List.length(first.arguments))};return Main()`)
	for _, now := range []int64{-17, -29} {
		if now != -17 {
			ctx, err = execution.WithHostInvocation(context.Background(), hostFixtureInvocation(t, root, now))
			if err != nil {
				t.Fatal(err)
			}
		}
		output, err := program.Run(ctx, files, execution.DefaultBudget(), device.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		result, failure := output.(value.Fallible).Outcome()
		if failure != nil {
			t.Fatal(failure)
		}
		fields := result.(value.Record)
		if fields.Field("clock").(value.Integer).Big().Int64() != now || fields.Field("uid").(value.Integer).Big().Int64() != 123 || fields.Field("home").(value.Text).Text() != root || fields.Field("count").(value.Integer).Big().Int64() != 2 {
			t.Fatalf("captured input changed: %s", value.TextResult(result))
		}
	}
}

func TestHostInvocationCanonicalTransport(t *testing.T) {
	root := fileFixtureRoot(t)
	initial := hostFixtureInvocation(t, root, -17)
	append32 := func(body []byte, n uint32) []byte { return encodingbinary.BigEndian.AppendUint32(body, n) }
	reference := append32(nil, 2)
	for _, text := range []string{"literal", "", root} {
		reference = append32(reference, uint32(len(text)+1))
		reference = append(reference, []byte(text)...)
		reference = append(reference, 0)
	}
	reference = append32(reference, 123)
	reference = append32(reference, 456)
	reference = encodingbinary.BigEndian.AppendUint64(reference, ^uint64(16))
	if string(initial.Transport()) != string(reference) {
		t.Fatal("snapshot body differs from canonical field framing")
	}
	files, err := adapter.OpenFiles(adapter.FileRoots{}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := files.Close(); err != nil {
			t.Error(err)
		}
	})
	program := processCompositionProgram(t, root, `flow Main()->Int!{initial=Host.Invocation.current();return initial.now_unix_ns};return Main()`)
	for _, test := range []struct {
		name   string
		body   []byte
		failed bool
	}{
		{"valid", reference, false}, {"truncated", reference[:len(reference)-1], true}, {"trailing", append(append([]byte{}, reference...), 0), true},
		{"count-overflow", []byte{0, 0, 16, 1}, true}, {"zero-string-length", []byte{0, 0, 0, 1, 0, 0, 0, 0}, true},
		{"interior-nul", []byte{0, 0, 0, 1, 0, 0, 0, 3, 0, 'x', 0}, true}, {"invalid-utf8", []byte{0, 0, 0, 1, 0, 0, 0, 2, 255, 0}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, err := execution.WithHostInvocationTransport(context.Background(), test.body)
			if err != nil {
				t.Fatal(err)
			}
			output, err := program.Run(ctx, files, execution.DefaultBudget(), device.DefaultConfig())
			if test.failed {
				var failure execution.Failure
				if !errors.As(err, &failure) || failure.Value().Kind() != "Host.InvocationFailed" {
					t.Fatalf("invalid transport admitted: %v %v", output, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			result, failure := output.(value.Fallible).Outcome()
			if failure != nil || result.(value.Integer).Big().Int64() != -17 {
				t.Fatalf("valid transport rejected: %v %v", result, failure)
			}
		})
	}
	path, err := value.NewText(types.Path(), root)
	if err != nil {
		t.Fatal(err)
	}
	large, err := value.NewInt(new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := value.NewHostInvocation(nil, path, large, value.Int64(0), value.Int64(0)); err == nil {
		t.Fatal("UID outside uint32 was admitted")
	}
	if _, err := value.NewHostInvocation(nil, path, value.Int64(0), value.Int64(0), large); err == nil {
		t.Fatal("clock outside int64 was admitted")
	}
}

func TestHostInvocationServerSnapshot(t *testing.T) {
	for _, test := range []struct {
		name       string
		failed     bool
		statusCode int
		body       string
	}{
		{"captured", false, 200, "-17"}, {"capture-error", true, 500, "failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			testHostInvocationServerSnapshot(t, test.failed, test.statusCode, test.body)
		})
	}
}

func testHostInvocationServerSnapshot(t *testing.T, captureFailed bool, expectedStatus int, expectedBody string) {
	t.Helper()
	root := fileFixtureRoot(t)
	ctx, err := execution.WithHostInvocation(context.Background(), hostFixtureInvocation(t, root, -17))
	if err != nil {
		t.Fatal(err)
	}
	if captureFailed {
		ctx, err = execution.WithHostInvocationTransport(context.Background(), []byte{0})
		if err != nil {
			t.Fatal(err)
		}
	}
	files, err := adapter.OpenFiles(adapter.FileRoots{}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := files.Close(); err != nil {
			t.Error(err)
		}
	})
	policy, err := adapter.NewListenPolicy([]uint16{0})
	if err != nil {
		t.Fatal(err)
	}
	program := processCompositionProgram(t, root, `flow Handler(request:HTTP.Request)->HTTP.Response!{initial=Host.Invocation.current();return HTTP.Response.Text.new(200,Int.to_string(initial.now_unix_ns))};flow Failed(error:Error)->HTTP.Response{return HTTP.Response.Text.new(500,"failed")};target=HTTP.Server.new(0,Handler,Failed);commit target`)
	servers, err := adapter.NewServers(program, files, policy, execution.DefaultBudget(), device.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := servers.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if _, err := servers.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", fileFixtureRoot(t))
	response, err := http.Get("http://" + servers.Snapshot()[0].Address)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != expectedStatus || string(body) != expectedBody {
		t.Fatalf("server recaptured host inputs: status=%d body=%q error=%v", response.StatusCode, body, err)
	}
}

func TestNativeHostInvocationArgumentAdmission(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM artifact lane")
	}
	artifact, built := buildForTest(t, sourceFile(t, `return "ordinary"`))
	if built.status != 0 {
		t.Fatalf("build artifact: %+v", built)
	}
	for _, arguments := range [][]string{{strings.Repeat("x", 65536)}, make([]string, 4097), {string([]byte{255})}} {
		args := append([]string{"run", "--format", "json", "--"}, arguments...)
		got := runExecutable(t, artifact, args...)
		if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "cli.invalid-invocation" {
			t.Fatalf("native host argument admission: %+v", got)
		}
	}
}
