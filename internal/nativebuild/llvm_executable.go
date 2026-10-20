package nativebuild

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/llvmemit"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativeplan"
)

const llvmVersion = "21.1.8"

type llvmFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type llvmTarget struct {
	Triple   string              `json:"triple"`
	Linker   llvmFile            `json:"linker"`
	Runtime  llvmFile            `json:"runtime"`
	Inputs   map[string]llvmFile `json:"inputs"`
	LinkArgs []string            `json:"link_args"`
}

type llvmDistribution struct {
	ABI      string                `json:"abi"`
	LLVM     string                `json:"llvm"`
	Host     string                `json:"host"`
	Compiler llvmFile              `json:"compiler"`
	Targets  map[string]llvmTarget `json:"targets"`
}

type admittedLLVM struct {
	compiler, linker, archive, archiveDigest, triple string
	inputs                                           map[string]string
	linkArgs                                         []string
}

// Link recipes belong to the compiler. A manifest identifies their exact inputs.
func llvmLinkRecipe(target string) (arguments, inputs []string) {
	switch target {
	case "darwin/arm64":
		return []string{"-dead_strip", "-arch", "arm64", "-platform_version", "macos", "27.0", "27.0",
			"-o", "{output}", "{object}", "{runtime}", "{input:system}"}, []string{"system"}
	case "linux/arm64":
		return []string{"-m", "aarch64elf", "-dynamic-linker", "/lib/ld-linux-aarch64.so.1",
				"-o", "{output}", "{input:crt1}", "{input:crti}", "{object}", "{runtime}",
				"{input:libgcc}", "{input:libm}", "{input:libc}", "{input:nonshared}", "{input:crtn}"},
			[]string{"crt1", "crti", "crtn", "libc", "libm", "libgcc", "nonshared"}
	default:
		return nil, nil
	}
}

func admitLLVM(ctx context.Context, manifest, target, snapshots string) (admittedLLVM, error) {
	var admitted admittedLLVM
	path, err := filepath.Abs(manifest)
	if err != nil {
		return admitted, fmt.Errorf("toolchain manifest: %w", err)
	}
	source, err := os.Open(path)
	if err != nil {
		return admitted, fmt.Errorf("read toolchain manifest: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(source, MaxDiagnostics+1))
	if err := errors.Join(readErr, source.Close()); err != nil {
		return admitted, fmt.Errorf("read toolchain manifest: %w", err)
	}
	if len(data) > MaxDiagnostics {
		return admitted, fmt.Errorf("toolchain manifest exceeds %d bytes", MaxDiagnostics)
	}
	var distribution llvmDistribution
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&distribution); err != nil {
		return admitted, fmt.Errorf("decode toolchain manifest: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return admitted, fmt.Errorf("toolchain manifest contains trailing data")
	}
	if distribution.ABI != nativeplan.ABIIdentity || distribution.LLVM != llvmVersion ||
		distribution.Host != runtime.GOOS+"/"+runtime.GOARCH {
		return admitted, fmt.Errorf("toolchain ABI, LLVM version, or host identity does not match")
	}
	triple := map[string]string{
		"darwin/arm64": "aarch64-apple-darwin",
		"linux/arm64":  "aarch64-unknown-linux-gnu",
	}[target]
	selected, ok := distribution.Targets[target]
	if !ok || triple == "" || selected.Triple != triple {
		return admitted, fmt.Errorf("toolchain has no matching target %q", target)
	}
	arguments, names := llvmLinkRecipe(target)
	if !slices.Equal(selected.LinkArgs, arguments) || len(selected.Inputs) != len(names) {
		return admitted, fmt.Errorf("toolchain must use the compiler's exact %s link recipe and input set", target)
	}
	for _, name := range names {
		if _, ok := selected.Inputs[name]; !ok {
			return admitted, fmt.Errorf("toolchain requires platform input %q", name)
		}
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return admitted, fmt.Errorf("resolve toolchain directory: %w", err)
	}
	copied := map[string]llvmFile{}
	file := func(subject string, identity llvmFile) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !filepath.IsLocal(identity.Path) {
			return "", fmt.Errorf("%s requires a relative path and SHA-256 digest", subject)
		}
		absolute := filepath.Join(root, identity.Path)
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", subject, err)
		}
		relative, err := filepath.Rel(root, resolved)
		if err != nil || !filepath.IsLocal(relative) {
			return "", fmt.Errorf("%s escapes the toolchain directory", subject)
		}
		destination := filepath.Join(snapshots, identity.Path)
		if previous, ok := copied[destination]; ok {
			if previous.SHA256 != identity.SHA256 {
				return "", fmt.Errorf("%s has conflicting input identities", subject)
			}
			return destination, nil
		}
		if err := os.MkdirAll(filepath.Dir(destination), os.ModePerm); err != nil {
			return "", fmt.Errorf("create snapshot directory for %s: %w", subject, err)
		}
		snapshot, err := snapshotLLVMFile(ctx, resolved, destination, identity.SHA256)
		if err != nil {
			return "", fmt.Errorf("admit %s: %w", subject, err)
		}
		copied[destination] = identity
		return snapshot, nil
	}
	if admitted.compiler, err = file("LLVM compiler", distribution.Compiler); err != nil {
		return admitted, err
	}
	if admitted.linker, err = file("LLVM linker", selected.Linker); err != nil {
		return admitted, err
	}
	if admitted.archive, err = file("Lo runtime archive", selected.Runtime); err != nil {
		return admitted, err
	}
	admitted.inputs = map[string]string{}
	for name, input := range selected.Inputs {
		if name == "" || strings.ContainsAny(name, "{}") {
			return admitted, fmt.Errorf("invalid platform input name %q", name)
		}
		admitted.inputs[name], err = file("platform input "+name, input)
		if err != nil {
			return admitted, err
		}
	}
	admitted.triple, admitted.archiveDigest = triple, selected.Runtime.SHA256
	admitted.linkArgs = arguments
	return admitted, nil
}

// BuildLLVM compiles and links using only admitted, explicitly supplied inputs.
func BuildLLVM(ctx context.Context, program ir.Program, output, target string, backend device.BuildBackend, manifest string) (receipt Receipt, failure diagnostic.Diagnostic) {
	fail := func(code diagnostic.Code, err error) (Receipt, diagnostic.Diagnostic) {
		return Receipt{}, diagnostic.New(code, diagnostic.Build, fmt.Sprintf("build LLVM executable: %v", err), nil)
	}
	if ctx == nil || program == nil {
		return fail(diagnostic.BuildFailed, fmt.Errorf("context and checked program are required"))
	}
	if target == "" {
		target = runtime.GOOS + "/" + runtime.GOARCH
	}
	if target != "darwin/arm64" && target != "linux/arm64" {
		return fail(diagnostic.BuildTarget, fmt.Errorf("unsupported target %q", target))
	}
	if backend != device.SoftwareBuild {
		return fail(diagnostic.BuildBackend, fmt.Errorf("LLVM execution requires the software build configuration"))
	}
	ctx, cancel := context.WithTimeout(ctx, BuildTimeout)
	defer cancel()
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
	inputInfo, inputErr := os.Stat(input)
	outputInfo, outputErr := os.Stat(destination)
	if inputErr != nil && !os.IsNotExist(inputErr) {
		return fail(diagnostic.BuildOutput, inputErr)
	}
	if outputErr != nil && !os.IsNotExist(outputErr) {
		return fail(diagnostic.BuildOutput, outputErr)
	}
	if input == destination || inputErr == nil && outputErr == nil && os.SameFile(inputInfo, outputInfo) {
		return fail(diagnostic.BuildOutput, fmt.Errorf("output refers to the input source"))
	}
	paths, err := prepareBuildPaths(os.Getenv("LO_BUILD_ROOT"))
	if err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	work, err := os.MkdirTemp(paths.work, "lo-llvm-")
	if err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	defer func() {
		if err := os.RemoveAll(work); err != nil && failure == nil {
			receipt, failure = fail(diagnostic.BuildOutput, fmt.Errorf("remove build directory: %w", err))
		}
	}()
	toolchain, err := admitLLVM(ctx, manifest, target, filepath.Join(work, "toolchain"))
	if err != nil {
		if ctx.Err() != nil {
			return fail(diagnostic.BuildCancelled, ctx.Err())
		}
		return fail(diagnostic.BuildToolchain, err)
	}
	if err := os.MkdirAll(filepath.Join(work, "tmp"), os.ModePerm); err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	plan, err := nativeplan.Lower(ctx, program)
	if err != nil {
		if ctx.Err() != nil {
			return fail(diagnostic.BuildCancelled, ctx.Err())
		}
		if errors.Is(err, nativeplan.ErrLimit) {
			return fail(diagnostic.BuildLimit, err)
		}
		return fail(diagnostic.BuildGenerate, err)
	}
	module, err := llvmemit.Emit(ctx, plan, target)
	if err != nil {
		if ctx.Err() != nil {
			return fail(diagnostic.BuildCancelled, ctx.Err())
		}
		if errors.Is(err, nativeplan.ErrLimit) {
			return fail(diagnostic.BuildLimit, err)
		}
		return fail(diagnostic.BuildGenerate, err)
	}
	module = append(module, []byte(`
declare i32 @lo_cli_main(i32, ptr)
define i32 @main(i32 %argc, ptr %argv) {
entry:
  %status = call i32 @lo_cli_main(i32 %argc, ptr %argv)
  ret i32 %status
}
`)...)
	modulePath, objectPath := filepath.Join(work, "program.ll"), filepath.Join(work, "program.o")
	if err := writeFile(modulePath, module); err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	staging, err := os.CreateTemp(filepath.Dir(destination), ".lo-llvm-")
	if err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	staged := staging.Name()
	defer func() {
		if err := os.Remove(staged); err != nil && !os.IsNotExist(err) && failure == nil {
			receipt, failure = fail(diagnostic.BuildOutput, fmt.Errorf("remove staged output: %w", err))
		}
	}()
	if err := staging.Close(); err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	// No inherited compiler flags, SDK selection, or search PATH enters this lane.
	env := []string{"CI=1", "PATH=", "HOME=" + work, "TMPDIR=" + filepath.Join(work, "tmp"),
		"TMP=" + filepath.Join(work, "tmp"), "TEMP=" + filepath.Join(work, "tmp"),
		"LC_ALL=C", "LANG=C", "ZERO_AR_DATE=1"}
	run := func(tool string, args []string) diagnostic.Diagnostic {
		command := exec.CommandContext(ctx, tool, args...)
		command.Dir, command.Env = work, env
		data, err, exceeded := runTool(command, cancel)
		if exceeded {
			_, failure := fail(diagnostic.BuildLimit, fmt.Errorf("tool diagnostics exceed %d bytes", MaxDiagnostics))
			return failure
		}
		if ctx.Err() != nil {
			_, failure := fail(diagnostic.BuildCancelled, ctx.Err())
			return failure
		}
		if err != nil {
			_, failure := fail(diagnostic.BuildFailed, fmt.Errorf("%s: %w: %s", filepath.Base(tool), err, data))
			return failure
		}
		return nil
	}
	compileArgs := []string{"-target", toolchain.triple, "-O0", "-g", "-fPIC", "-ffp-contract=off", "-c", modulePath, "-o", objectPath}
	if target == "darwin/arm64" {
		compileArgs = append(compileArgs, "-mmacosx-version-min=27.0")
	}
	if failure := run(toolchain.compiler, compileArgs); failure != nil {
		return Receipt{}, failure
	}
	args := make([]string, len(toolchain.linkArgs))
	for i, arg := range toolchain.linkArgs {
		switch arg {
		case "{object}":
			args[i] = objectPath
		case "{runtime}":
			args[i] = toolchain.archive
		case "{output}":
			args[i] = staged
		default:
			if strings.HasPrefix(arg, "{input:") {
				name := strings.TrimSuffix(strings.TrimPrefix(arg, "{input:"), "}")
				args[i] = toolchain.inputs[name]
			} else {
				args[i] = arg
			}
		}
	}
	if failure := run(toolchain.linker, args); failure != nil {
		return Receipt{}, failure
	}
	artifact, err := os.Stat(staged)
	if err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	if artifact.Size() > MaxArtifactBytes {
		return fail(diagnostic.BuildLimit, fmt.Errorf("artifact exceeds %d bytes", MaxArtifactBytes))
	}
	if err := ctx.Err(); err != nil {
		return fail(diagnostic.BuildCancelled, err)
	}
	if err := publishLLVM(ctx, staged, destination, work, os.RemoveAll); err != nil {
		if ctx.Err() != nil {
			return fail(diagnostic.BuildCancelled, ctx.Err())
		}
		return fail(diagnostic.BuildOutput, err)
	}
	return Receipt{Command: "build", Status: "ok", Artifact: output, Target: target,
		Codegen: "llvm", ABI: nativeplan.ABIIdentity, Runtime: toolchain.archiveDigest}, nil
}

func publishLLVM(ctx context.Context, staged, destination, work string, removeWork func(string) error) (err error) {
	defer func() {
		if err != nil {
			if removal := os.Remove(staged); removal != nil && !os.IsNotExist(removal) {
				err = errors.Join(err, fmt.Errorf("remove staged executable: %w", removal))
			}
		}
	}()
	if err := removeWork(work); err != nil {
		return fmt.Errorf("remove build directory: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(staged, destination); err != nil {
		return fmt.Errorf("publish executable: %w", err)
	}
	return nil
}
