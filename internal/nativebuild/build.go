// Package nativebuild constructs isolated, source-independent native artifacts.
package nativebuild

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/codegen"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/metalconfig"
	"github.com/MarcoPoloResearchLab/Lo/internal/runtimebundle"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

const Toolchain = metalconfig.GoToolchain
const BuildTimeout = 180 * time.Second
const MaxDiagnostics = 1 << 20
const MaxArtifactBytes = 64 << 20
const CompilerIdentity = "lo-native-go-v1"

type Receipt struct {
	Command  string `json:"command"`
	Status   string `json:"status"`
	Artifact string `json:"artifact"`
	Target   string `json:"target"`
	Emit     string `json:"emit,omitempty"`
	ABI      string `json:"abi,omitempty"`
}
type Metadata struct {
	SourceDigest  string                `json:"source_sha256"`
	Source        string                `json:"source"`
	Compiler      string                `json:"compiler"`
	Bundle        string                `json:"runtime_bundle_sha256"`
	Toolchain     string                `json:"toolchain"`
	Target        string                `json:"target"`
	DeviceBackend device.BuildBackend   `json:"device_backend"`
	Metal         *metalconfig.Identity `json:"metal,omitempty"`
	MetalBundle   string                `json:"metal_runtime_sha256,omitempty"`
	MetalManifest string                `json:"metal_manifest_sha256,omitempty"`
}

type buildProfile uint8

const (
	productionProfile buildProfile = iota
	raceProfile
)

func Build(ctx context.Context, program ir.Program, output, target string, backend device.BuildBackend) (Receipt, diagnostic.Diagnostic) {
	return build(ctx, program, output, target, backend, productionProfile)
}
func build(ctx context.Context, program ir.Program, output, target string, backend device.BuildBackend, profile buildProfile) (receipt Receipt, failure diagnostic.Diagnostic) {
	fail := func(code diagnostic.Code, err error) (Receipt, diagnostic.Diagnostic) {
		return Receipt{}, diagnostic.New(code, diagnostic.Build, fmt.Sprintf("build %s with %s: %v", program.Source().Path(), Toolchain, err), nil)
	}
	if ctx == nil || program == nil {
		return Receipt{}, diagnostic.New(diagnostic.BuildFailed, diagnostic.Build, "build requires context and checked program", nil)
	}
	if target == "" {
		target = runtime.GOOS + "/" + runtime.GOARCH
	}
	if target != "darwin/arm64" && target != "linux/arm64" {
		return fail(diagnostic.BuildTarget, fmt.Errorf("unsupported target %q", target))
	}
	var metalArchive, metalManifest []byte
	var metalIdentity *metalconfig.Identity
	switch backend {
	case device.MetalBuild:
		if target != metalconfig.Target || runtime.GOOS+"/"+runtime.GOARCH != metalconfig.Target {
			return fail(diagnostic.BuildTarget, fmt.Errorf("Metal requires a %s host and target", metalconfig.Target))
		}
		var err error
		metalArchive, metalManifest, metalIdentity, err = metalInputs()
		if err != nil {
			return fail(diagnostic.BuildBackend, err)
		}
	case device.SoftwareBuild:
	default:
		return fail(diagnostic.BuildBackend, fmt.Errorf("unsupported device backend %q", backend))
	}
	if output == "" {
		return fail(diagnostic.BuildOutput, fmt.Errorf("an output path is required"))
	}
	destination, err := filepath.Abs(output)
	if err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	input, err := filepath.Abs(program.Source().Path())
	if err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	if input == destination {
		return fail(diagnostic.BuildOutput, fmt.Errorf("output refers to the input source"))
	}
	inputIdentity, inputError := os.Stat(input)
	outputIdentity, outputError := os.Stat(destination)
	if inputError != nil && !os.IsNotExist(inputError) {
		return fail(diagnostic.BuildOutput, inputError)
	}
	if outputError != nil && !os.IsNotExist(outputError) {
		return fail(diagnostic.BuildOutput, outputError)
	}
	if inputError == nil && outputError == nil && os.SameFile(inputIdentity, outputIdentity) {
		return fail(diagnostic.BuildOutput, fmt.Errorf("output refers to the input source"))
	}
	ctx, cancel := context.WithTimeout(ctx, BuildTimeout)
	defer cancel()
	tool, err := exec.LookPath("go")
	if err != nil {
		return fail(diagnostic.BuildToolchain, err)
	}
	paths, err := prepareBuildPaths(os.Getenv("LO_BUILD_ROOT"))
	if err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	work, err := os.MkdirTemp(paths.work, "lo-native-")
	if err != nil {
		return fail(diagnostic.BuildFailed, err)
	}
	defer func() {
		if err := os.RemoveAll(work); err != nil && failure == nil {
			receipt, failure = fail(diagnostic.BuildFailed, fmt.Errorf("remove build directory: %w", err))
		}
	}()
	temporary := filepath.Join(work, "tmp")
	if err := os.MkdirAll(temporary, os.ModePerm); err != nil {
		return fail(diagnostic.BuildFailed, err)
	}
	env := environment(work, paths.root, target)
	if profile == raceProfile {
		env = append(env, "CGO_ENABLED=1")
	}
	version := exec.CommandContext(ctx, tool, "version")
	version.Env = env
	version.Dir = work
	versionOutput, err, exceeded := runTool(version, cancel)
	if exceeded {
		return fail(diagnostic.BuildLimit, fmt.Errorf("toolchain diagnostics exceed %d bytes", MaxDiagnostics))
	}
	if ctx.Err() != nil {
		return fail(diagnostic.BuildCancelled, ctx.Err())
	}
	if err != nil || !strings.HasPrefix(string(versionOutput), "go version "+Toolchain+" ") {
		return fail(diagnostic.BuildToolchain, fmt.Errorf("require %s, received %q: %v", Toolchain, strings.TrimSpace(string(versionOutput)), err))
	}
	if backend == device.MetalBuild {
		limited := false
		run := func(args ...string) (string, error) {
			command := exec.CommandContext(ctx, args[0], args[1:]...)
			command.Dir, command.Env = work, env
			output, err, exceeded := runTool(command, cancel)
			limited = limited || exceeded
			if err != nil {
				return "", fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, output)
			}
			return string(output), nil
		}
		settings, err := metalconfig.NativeEnvironment(run)
		if limited {
			return fail(diagnostic.BuildLimit, fmt.Errorf("toolchain diagnostics exceed %d bytes", MaxDiagnostics))
		}
		if ctx.Err() != nil {
			return fail(diagnostic.BuildCancelled, ctx.Err())
		}
		if err != nil {
			return fail(diagnostic.BuildToolchain, err)
		}
		env = append(env, settings...)
	}
	generated, err := codegen.Generate(program, "generated")
	if err != nil {
		return fail(diagnostic.BuildGenerate, err)
	}
	if err := extract(work); err != nil {
		return fail(diagnostic.BuildFailed, err)
	}
	if backend == device.MetalBuild {
		if err := extractArchive(work, metalArchive); err != nil {
			return fail(diagnostic.BuildFailed, err)
		}
	}
	sum := sha256.Sum256([]byte(program.Source().Text()))
	metadata := Metadata{SourceDigest: hex.EncodeToString(sum[:]), Source: program.Source().Path(), Compiler: CompilerIdentity, Bundle: runtimebundle.Digest(), Toolchain: Toolchain, Target: target, DeviceBackend: backend, Metal: metalIdentity}
	if backend == device.MetalBuild {
		metadata.MetalBundle = digestBytes(metalArchive)
		metadata.MetalManifest = digestBytes(metalManifest)
	}
	identity, err := json.Marshal(metadata)
	if err != nil {
		return fail(diagnostic.BuildFailed, err)
	}
	files := map[string][]byte{"internal/generated/program.go": generated, "cmd/program/main.go": []byte(fmt.Sprintf("package main\nimport(\"fmt\";\"os\";\"github.com/MarcoPoloResearchLab/Lo/internal/generated\";\"github.com/MarcoPoloResearchLab/Lo/internal/runner\")\nconst metadata=%q\nfunc main(){if len(os.Args)==2&&os.Args[1]==\"metadata\"{fmt.Println(metadata);return};p,err:=generated.New();if err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(2)};runner.Main(p)}\n", string(identity)))}
	for path, data := range files {
		if err := writeFile(filepath.Join(work, path), data); err != nil {
			return fail(diagnostic.BuildFailed, err)
		}
	}
	staging, err := os.CreateTemp(filepath.Dir(destination), ".lo-artifact-")
	if err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	staged := staging.Name()
	defer func() {
		if err := os.Remove(staged); err != nil && !os.IsNotExist(err) && failure == nil {
			receipt, failure = fail(diagnostic.BuildOutput, fmt.Errorf("remove staged artifact: %w", err))
		}
	}()
	if err := staging.Close(); err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	linkFlags := "-ldflags=-buildid="
	if backend == device.MetalBuild {
		// Go uses this content identity to replace Apple's path-dependent Mach-O UUID.
		linkFlags += digestBytes(append(append(append([]byte(nil), generated...), identity...), byte(profile)))
	}
	arguments := []string{"build", "-mod=vendor", "-trimpath", "-buildvcs=false", "-pgo=off", linkFlags, "-p=2", "-o", staged}
	if profile == raceProfile {
		arguments = append(arguments, "-race")
	}
	if backend == device.MetalBuild {
		arguments = append(arguments, "-tags=metal")
	}
	arguments = append(arguments, "./cmd/program")
	command := exec.CommandContext(ctx, tool, arguments...)
	command.Dir = work
	command.Env = env
	outputText, err, exceeded := runTool(command, cancel)
	if exceeded {
		return fail(diagnostic.BuildLimit, fmt.Errorf("toolchain diagnostics exceed %d bytes", MaxDiagnostics))
	}
	if ctx.Err() != nil {
		return fail(diagnostic.BuildCancelled, ctx.Err())
	}
	if err != nil {
		return fail(diagnostic.BuildFailed, fmt.Errorf("toolchain: %w: %s", err, outputText))
	}
	file, err := os.Open(staged)
	if err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	size, readErr := io.Copy(io.Discard, io.LimitReader(file, MaxArtifactBytes+1))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	if size > MaxArtifactBytes {
		return fail(diagnostic.BuildLimit, fmt.Errorf("artifact exceeds %d bytes", MaxArtifactBytes))
	}
	if err := os.RemoveAll(work); err != nil {
		return fail(diagnostic.BuildFailed, fmt.Errorf("remove build directory: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return fail(diagnostic.BuildCancelled, err)
	}
	if err := os.Rename(staged, destination); err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	return Receipt{Command: "build", Status: "ok", Artifact: output, Target: target}, nil
}

func runTool(command *exec.Cmd, cancel context.CancelFunc) ([]byte, error, bool) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = time.Second
	output := &boundedOutput{limit: MaxDiagnostics, cancel: cancel}
	command.Stdout, command.Stderr = output, output
	err := command.Run()
	return output.buffer.Bytes(), err, output.exceeded
}
func environment(work, root, target string) []string {
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GO") || strings.HasPrefix(key, "CGO") || key == "CC" || key == "CXX" || key == "AR" || key == "SDKROOT" || key == "MACOSX_DEPLOYMENT_TARGET" || key == "TMPDIR" || key == "TMP" || key == "TEMP" || key == "XDG_CACHE_HOME" || key == "CLANG_MODULE_CACHE_PATH" || key == "CCACHE_DIR" || key == "CI" {
			continue
		}
		env = append(env, entry)
	}
	osName, arch, _ := strings.Cut(target, "/")
	return append(env, "GOTOOLCHAIN=local", "GOWORK=off", "GOENV=off", "GOFLAGS=", "CGO_ENABLED=0", "GOOS="+osName, "GOARCH="+arch, "GOPROXY=off", "GOSUMDB=off", "GONOPROXY=none", "GOPRIVATE=", "GOMODCACHE="+filepath.Join(root, "modules"), "GOCACHE="+filepath.Join(root, "go-build"), "GOPATH="+filepath.Join(root, "gopath"), "GOMAXPROCS=2", "GOTELEMETRY=off", "CI=1", "TMPDIR="+filepath.Join(work, "tmp"), "TMP="+filepath.Join(work, "tmp"), "TEMP="+filepath.Join(work, "tmp"), "XDG_CACHE_HOME="+filepath.Join(root, "cache"), "CLANG_MODULE_CACHE_PATH="+filepath.Join(root, "clang"), "CCACHE_DIR="+filepath.Join(root, "ccache"))
}
func extract(work string) error {
	return extractArchive(work, runtimebundle.Bytes())
}
func extractArchive(work string, data []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, file := range reader.File {
		if !filepath.IsLocal(file.Name) {
			return fmt.Errorf("invalid runtime bundle path %q", file.Name)
		}
		input, err := file.Open()
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(input)
		closeErr := input.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(work, file.Name), data); err != nil {
			return err
		}
	}
	return nil
}
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Close())
}

type boundedOutput struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	limit    int
	cancel   context.CancelFunc
	exceeded bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := b.limit - b.buffer.Len()
	if len(p) > remaining {
		b.buffer.Write(p[:remaining])
		b.exceeded = true
		b.cancel()
	} else {
		b.buffer.Write(p)
	}
	return n, nil
}
