package nativebuild

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/llvmemit"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativeplan"
)

// EmitLLVM writes a verified LLVM module without executing a compiler toolchain.
func EmitLLVM(ctx context.Context, program ir.Program, output, target string, backend device.BuildBackend) (receipt Receipt, failure diagnostic.Diagnostic) {
	fail := func(code diagnostic.Code, err error) (Receipt, diagnostic.Diagnostic) {
		return Receipt{}, diagnostic.New(code, diagnostic.Build, fmt.Sprintf("emit LLVM: %v", err), nil)
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
		return fail(diagnostic.BuildBackend, fmt.Errorf("LLVM module emission requires the software build configuration"))
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
	if len(module) > MaxArtifactBytes {
		return fail(diagnostic.BuildLimit, fmt.Errorf("module exceeds %d bytes", MaxArtifactBytes))
	}
	if err := ctx.Err(); err != nil {
		return fail(diagnostic.BuildCancelled, err)
	}
	staging, err := os.CreateTemp(filepath.Dir(destination), ".lo-module-")
	if err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	staged := staging.Name()
	defer func() {
		if err := os.Remove(staged); err != nil && !os.IsNotExist(err) && failure == nil {
			receipt, failure = fail(diagnostic.BuildOutput, fmt.Errorf("remove staged module: %w", err))
		}
	}()
	_, writeErr := staging.Write(module)
	if err := errors.Join(writeErr, staging.Close()); err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	if err := ctx.Err(); err != nil {
		return fail(diagnostic.BuildCancelled, err)
	}
	if err := os.Rename(staged, destination); err != nil {
		return fail(diagnostic.BuildOutput, err)
	}
	return Receipt{Command: "build", Status: "ok", Artifact: output, Target: target, Emit: "llvm", ABI: nativeplan.ABIIdentity}, nil
}
