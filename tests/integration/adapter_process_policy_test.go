package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/processhost"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// These paths are inert descriptions; the tests create no executable, launcher,
// directory or data fixture, and parsing must not require any of them to exist.
func processPolicyPaths(t *testing.T) (root, command, launcher string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "_scratch", "adapter-implementation-x921", "process-policy-fixture"))
	if err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(root, "command,with spaces"), filepath.Join(root, "launcher,with spaces")
}

func processPolicyFlags(t *testing.T) []string {
	t.Helper()
	root, command, launcher := processPolicyPaths(t)
	return []string{"--process-allow", strings.Repeat("aB", 32) + "," + command, "--process-launcher", launcher, "--process-read-root", root}
}

func processPolicyProgram(t *testing.T, code string) execution.Program {
	t.Helper()
	root, _, _ := processPolicyPaths(t)
	text, err := source.New(filepath.Join(root, "policy.lo"), code)
	if err != nil {
		t.Fatal(err)
	}
	checked, diagnostic := compiler.Check(text)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	program, err := eval.Program(checked)
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func TestAdapterProcessPolicyAcceptsInertCompilerInputs(t *testing.T) {
	root, _, _ := processPolicyPaths(t)
	for _, command := range []string{runner.RunCommand, runner.ServeCommand} {
		t.Run(command, func(t *testing.T) {
			args := append([]string{command, filepath.Join(root, "absent-source.lo")}, processPolicyFlags(t)...)
			args = append(args, "--process-allow", strings.Repeat("0", 64)+","+filepath.Join(root, "second-command"), "--process-read-root", filepath.Join(root, "second-read-root"))
			options, err := runner.ParseCompiler(args)
			if err != nil || options.Command() != command {
				t.Fatalf("valid inert policy rejected: %v", err)
			}
		})
	}
}

func TestAdapterProcessPolicyCopiesCollections(t *testing.T) {
	root, command, launcher := processPolicyPaths(t)
	grants := []string{strings.Repeat("aB", 32) + "," + command}
	reads := []string{root}
	policy, err := adapter.NewProcessPolicy(grants, launcher, reads)
	if err != nil {
		t.Fatal(err)
	}
	grants[0], reads[0] = "changed", "changed"
	returnedGrants, returnedReads := policy.Grants(), policy.ReadRoots()
	if policy.Launcher() != launcher || returnedGrants[0].Path() != command || returnedReads[0] != root {
		t.Fatal("policy retained mutable input collections")
	}
	for _, octet := range returnedGrants[0].Digest() {
		if octet != 0xab {
			t.Fatal("grant digest did not normalize hexadecimal case")
		}
	}
	returnedGrants[0], returnedReads[0] = nil, "changed"
	if policy.Grants()[0] == nil || policy.Grants()[0].Path() != command || policy.ReadRoots()[0] != root {
		t.Fatal("policy exposed mutable collection storage")
	}
}

func TestAdapterProcessPolicyParsesBeforeHostValidation(t *testing.T) {
	for _, command := range []string{runner.RunCommand, runner.ServeCommand} {
		t.Run(command, func(t *testing.T) {
			root, _, _ := processPolicyPaths(t)
			if _, err := runner.ParseCompiler(append([]string{command, filepath.Join(root, "source.lo")}, processPolicyFlags(t)...)); err != nil {
				t.Fatal(err)
			}
			var ran atomic.Bool
			program, err := execution.NewProgram(func(execution.Session) (value.Value, error) { ran.Store(true); return value.Int64(7), nil }, nil)
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			status := runner.Execute(program, append([]string{command, "--format", runner.JsonFormat}, processPolicyFlags(t)...), &stdout, &stderr)
			invalidHost := strings.Contains(stderr.String(), fmt.Sprint(execution.ProcessUnavailable().Code())) || strings.Contains(stderr.String(), fmt.Sprint(execution.HostFailure("", "", nil).Code()))
			if status != runner.ExitHost || ran.Load() || stdout.Len() != 0 || !invalidHost {
				t.Fatalf("invalid Process configuration reached evaluation: %s", stderr.String())
			}
		})
	}
}

func TestAdapterProcessPolicyRejectsMalformedOrIncompleteInputs(t *testing.T) {
	root, command, launcher := processPolicyPaths(t)
	digest := strings.Repeat("a", 64)
	allow := []string{"--process-allow", digest + "," + command}
	launch := []string{"--process-launcher", launcher}
	read := []string{"--process-read-root", root}
	full := append(append(append([]string{}, allow...), launch...), read...)
	with := func(extra ...string) []string { return append(append([]string{}, full...), extra...) }
	cases := []struct {
		name  string
		flags []string
	}{
		{"only-allow", allow}, {"only-launcher", launch}, {"only-read", read},
		{"allow-launcher", append(append([]string{}, allow...), launch...)},
		{"allow-read", append(append([]string{}, allow...), read...)},
		{"launcher-read", append(append([]string{}, launch...), read...)},
		{"missing-allow-value", []string{"--process-allow"}},
		{"missing-launcher-value", []string{"--process-launcher"}},
		{"missing-read-value", []string{"--process-read-root"}},
		{"empty-allow", with("--process-allow", "")},
		{"empty-launcher", []string{"--process-launcher", "", "--process-allow", digest + "," + command, "--process-read-root", root}},
		{"empty-read", with("--process-read-root", "")},
		{"short-digest", with("--process-allow", digest[:63]+","+filepath.Join(root, "other"))},
		{"long-digest", with("--process-allow", digest+"0,"+filepath.Join(root, "other"))},
		{"nonhex-digest", with("--process-allow", strings.Repeat("g", 64)+","+filepath.Join(root, "other"))},
		{"missing-separator", with("--process-allow", digest+filepath.Join(root, "other"))},
		{"empty-command", with("--process-allow", digest+",")},
		{"relative-command", with("--process-allow", digest+",relative-command")},
		{"unclean-command", with("--process-allow", digest+","+root+"/part/../other")},
		{"nul-command", with("--process-allow", digest+","+filepath.Join(root, "other")+"\x00")},
		{"relative-launcher", append(append([]string{}, allow...), "--process-launcher", "relative-launcher", "--process-read-root", root)},
		{"unclean-launcher", append(append([]string{}, allow...), "--process-launcher", root+"/part/../launcher", "--process-read-root", root)},
		{"nul-launcher", append(append([]string{}, allow...), "--process-launcher", launcher+"\x00", "--process-read-root", root)},
		{"relative-read", with("--process-read-root", "relative-read")},
		{"unclean-read", with("--process-read-root", root+"/part/../read")},
		{"nul-read", with("--process-read-root", root+"\x00")},
		{"duplicate-launcher", with("--process-launcher", launcher)},
		{"second-launcher", with("--process-launcher", filepath.Join(root, "other-launcher"))},
		{"duplicate-read", with("--process-read-root", root)},
		{"duplicate-command", with("--process-allow", digest+","+command)},
		{"uppercase-duplicate", with("--process-allow", strings.ToUpper(digest)+","+command)},
		{"conflicting-command-digest", with("--process-allow", strings.Repeat("b", 64)+","+command)},
		{"launcher-command-collision", []string{"--process-allow", digest + "," + launcher, "--process-launcher", launcher, "--process-read-root", root}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{runner.RunCommand, filepath.Join(root, "source.lo")}, test.flags...)
			if _, err := runner.ParseCompiler(args); err == nil {
				t.Fatal("invalid Process policy was accepted")
			}
			var ran atomic.Bool
			program, err := execution.NewProgram(func(execution.Session) (value.Value, error) { ran.Store(true); return value.Int64(7), nil }, nil)
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			status := runner.Execute(program, append([]string{runner.RunCommand}, test.flags...), &stdout, &stderr)
			if status != runner.ExitHost || ran.Load() || stdout.Len() != 0 {
				t.Fatal("invalid embedded policy reached execution")
			}
		})
	}
}

func TestAdapterProcessPolicyRejectsFlagsForOtherCommands(t *testing.T) {
	root, _, _ := processPolicyPaths(t)
	for _, command := range []string{runner.ParseCommand, runner.CheckCommand, runner.InspectCommand, runner.BuildCommand} {
		t.Run(command, func(t *testing.T) {
			args := []string{command, filepath.Join(root, "source.lo")}
			if command == runner.BuildCommand {
				args = append(args, "--output", filepath.Join(root, "artifact"))
			}
			if _, err := runner.ParseCompiler(append(args, processPolicyFlags(t)...)); err == nil {
				t.Fatal("Process policy accepted outside run/serve")
			}
			var ran atomic.Bool
			program, err := execution.NewProgram(func(execution.Session) (value.Value, error) { ran.Store(true); return value.Int64(7), nil }, nil)
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if runner.Execute(program, append([]string{command}, processPolicyFlags(t)...), &stdout, &stderr) != runner.ExitHost || ran.Load() {
				t.Fatal("unsupported embedded command reached execution")
			}
		})
	}
}

func TestAdapterProcessPolicyRequiresSessionBeforeEvaluation(t *testing.T) {
	var evaluated atomic.Bool
	program, err := execution.NewProgram(func(execution.Session) (value.Value, error) {
		evaluated.Store(true)
		return value.Int64(7), nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root, _, _ := processPolicyPaths(t)
	options, err := runner.ParseCompiler(append([]string{runner.RunCommand, filepath.Join(root, "source.lo"), "--format", runner.JsonFormat}, processPolicyFlags(t)...))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if runner.Run(options, program, &stdout, &stderr) != runner.ExitHost || evaluated.Load() || stdout.Len() != 0 {
		t.Fatalf("invalid Process session reached program evaluation: %s %s", stdout.String(), stderr.String())
	}
}

func TestAdapterProcessPolicyDoesNotGrantParentFileAuthority(t *testing.T) {
	configuration, root, _ := sessionFixture(t)
	var creations atomic.Int32
	session, err := processhost.OpenTrusted(context.Background(), configuration, adapter.FileRoots{}, sessionCreator(func(context.Context, adapter.ProcessInvocation) (processhost.Primary, error) {
		creations.Add(1)
		return nil, errors.New("unexpected creation")
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	for _, code := range []string{
		fmt.Sprintf(`return File.read(File.Path.new(%q))`, filepath.Join(root, "data")),
		fmt.Sprintf(`flow Main() -> File.WriteReceipt! { target = File.Write.new(File.Path.new(%q), UTF8.Bytes.new("data")); result = commit target; return result }; return Main()`, filepath.Join(root, "data")),
	} {
		program := processPolicyProgram(t, code)
		_, runErr := program.Run(context.Background(), session.Boundary(), execution.DefaultBudget(), device.DefaultConfig())
		var host execution.HostError
		if !errors.As(runErr, &host) || host.Code() != execution.Denied("File", "fixture").Code() {
			t.Fatalf("Process policy granted parent File authority: %v", runErr)
		}
	}
	if creations.Load() != 0 {
		t.Fatal("File operation created a process")
	}
}
