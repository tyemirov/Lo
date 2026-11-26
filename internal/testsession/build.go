package testsession

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const cliEntry = `declare i32 @lo_cli_main(i32, ptr, ptr)
define i32 @main(i32 %argc, ptr %argv, ptr %envp) {
entry:
  %status = call i32 @lo_cli_main(i32 %argc, ptr %argv, ptr %envp)
  ret i32 %status
}
`

// CommandError retains the first failed fixture command before output decoding.
type CommandError struct {
	Operation string   `json:"operation"`
	Arguments []string `json:"arguments"`
	Status    int      `json:"status"`
	Stdout    string   `json:"stdout"`
	Stderr    string   `json:"stderr"`
	Cause     string   `json:"cause"`
}

func (failure *CommandError) Error() string {
	return fmt.Sprintf("%s: command=%q status=%d cause=%q stdout=%q stderr=%q", failure.Operation, failure.Arguments, failure.Status, failure.Cause, failure.Stdout, failure.Stderr)
}

func runCommand(ctx context.Context, operation, directory string, environment []string, executable string, arguments ...string) error {
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir, command.Env = directory, environment
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return err
		}
		return nil
	}
	command.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s: %w", operation, ctx.Err())
		}
		status := -1
		if command.ProcessState != nil {
			status = command.ProcessState.ExitCode()
		}
		return &CommandError{Operation: operation, Arguments: command.Args, Status: status, Stdout: stdout.String(), Stderr: stderr.String(), Cause: err.Error()}
	}
	return nil
}

func (server *Server) backendCommand(ctx context.Context, operation, work, tool string, arguments ...string) error {
	root := filepath.Dir(server.inputs.Manifest)
	if runtime.GOOS == "linux" {
		libraryDirectory := ""
		for _, identity := range server.distribution.HostLibraries {
			libraryDirectory = filepath.Dir(filepath.Join(root, identity.Path))
			break
		}
		arguments = append([]string{"--inhibit-cache", "--library-path", libraryDirectory, "--argv0", filepath.Base(tool), tool}, arguments...)
		tool = "/lib/ld-linux-aarch64.so.1"
	}
	environment := []string{"CI=1", "PATH=", "HOME=" + work, "TMPDIR=" + work, "TMP=" + work, "TEMP=" + work, "LC_ALL=C", "LANG=C", "ZERO_AR_DATE=1"}
	return runCommand(ctx, operation, work, environment, tool, arguments...)
}

func compileFlags(target targetInputs, selectedTarget string) []string {
	flags := []string{"-target", target.Triple, "-O2", "-g", "-fPIC", "-ffp-contract=off"}
	if selectedTarget == "darwin/arm64" {
		flags = append(flags, "-mmacosx-version-min=27.0")
	}
	return flags
}

func writeBytes(path string, data []byte) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Close())
}

func (server *Server) entryObject(ctx context.Context, selectedTarget string, target targetInputs) (string, error) {
	server.entryMu.Lock()
	defer server.entryMu.Unlock()
	if object := server.entries[selectedTarget]; object != "" {
		return object, nil
	}
	work := filepath.Join(server.directory, "entry", strings.ReplaceAll(selectedTarget, "/", "-"))
	if err := os.MkdirAll(work, os.ModePerm); err != nil {
		return "", fmt.Errorf("create CLI entry workspace: %w", err)
	}
	source, object := filepath.Join(work, "entry.ll"), filepath.Join(work, "entry.o")
	if err := writeBytes(source, []byte(cliEntry)); err != nil {
		return "", fmt.Errorf("write CLI entry module: %w", err)
	}
	tool := filepath.Join(filepath.Dir(server.inputs.Manifest), server.distribution.Compiler.Path)
	flags := append(compileFlags(target, selectedTarget), "-c", source, "-o", object)
	if err := server.backendCommand(ctx, "compile native CLI entry", work, tool, flags...); err != nil {
		return "", err
	}
	server.entries[selectedTarget] = object
	return object, nil
}

func (server *Server) buildFixture(ctx context.Context, key string, selected CompileRequest, record *buildRecord) (_ CompileResult, failure error) {
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	workRoot := filepath.Join(server.directory, "work")
	artifactRoot := filepath.Join(server.directory, "artifacts")
	for _, directory := range []string{workRoot, artifactRoot} {
		if err := os.MkdirAll(directory, os.ModePerm); err != nil {
			return CompileResult{}, fmt.Errorf("create fixture workspace: %w", err)
		}
	}
	work, err := os.MkdirTemp(workRoot, "compile-")
	if err != nil {
		return CompileResult{}, fmt.Errorf("create native fixture workspace: %w", err)
	}
	defer func() {
		if work != "" {
			failure = errors.Join(failure, os.RemoveAll(work))
		}
	}()
	module, object := filepath.Join(work, "program.ll"), filepath.Join(work, "program.o")
	arguments := []string{"build", selected.Source, "--emit", "llvm", "--target", selected.Target, "--output", module, "--format", "json"}
	if selected.Backend == "metal" {
		arguments = append(arguments, "--device-backend", "metal")
	}
	if err := runCommand(ctx, "emit native fixture", server.root, os.Environ(), server.inputs.Compiler, arguments...); err != nil {
		return CompileResult{}, err
	}
	target := server.distribution.Targets[selected.Target]
	var metalIdentity json.RawMessage
	if selected.Backend == "metal" {
		target = server.distribution.Metal.targetInputs
		metalIdentity = server.distribution.Metal.Identity
	}
	data, err := os.ReadFile(module)
	if err != nil {
		return CompileResult{}, fmt.Errorf("read native fixture module: %w", err)
	}
	data, err = artifactMetadata(data, selected.Source, record.sourceDigest, target.Runtime.SHA256, metalIdentity)
	if err != nil {
		return CompileResult{}, fmt.Errorf("native fixture metadata: %w", err)
	}
	if err := writeBytes(module, data); err != nil {
		return CompileResult{}, fmt.Errorf("write native fixture module: %w", err)
	}
	inputRoot := filepath.Dir(server.inputs.Manifest)
	compiler := filepath.Join(inputRoot, server.distribution.Compiler.Path)
	objects := []string{object}
	if selected.Purpose == RuntimeValues {
		code, private, err := splitRuntimeValueModule(data)
		if err != nil {
			return CompileResult{}, fmt.Errorf("split runtime value fixture: %w", err)
		}
		if err := server.runtimeValueCode(ctx, selected, target, compiler, code, object); err != nil {
			return CompileResult{}, err
		}
		privateSource, privateObject := filepath.Join(work, "metadata.ll"), filepath.Join(work, "metadata.o")
		if err := writeBytes(privateSource, private); err != nil {
			return CompileResult{}, err
		}
		flags := append(compileFlags(target, selected.Target), "-c", privateSource, "-o", privateObject)
		if err := server.backendCommand(ctx, "compile private fixture metadata", work, compiler, flags...); err != nil {
			return CompileResult{}, err
		}
		objects = append(objects, privateObject)
	} else {
		flags := append(compileFlags(target, selected.Target), "-c", module, "-o", object)
		if err := server.backendCommand(ctx, "compile native fixture", work, compiler, flags...); err != nil {
			return CompileResult{}, err
		}
	}
	entry, err := server.entryObject(ctx, selected.Target, target)
	if err != nil {
		return CompileResult{}, err
	}
	staged := filepath.Join(work, "program")
	var linkArguments []string
	for _, argument := range target.LinkArgs {
		switch argument {
		case "{object}":
			linkArguments = append(append(linkArguments, objects...), entry)
		case "{runtime}":
			linkArguments = append(linkArguments, filepath.Join(inputRoot, target.Runtime.Path))
		case "{output}":
			linkArguments = append(linkArguments, staged)
		default:
			if strings.HasPrefix(argument, "{input:") && strings.HasSuffix(argument, "}") {
				name := strings.TrimSuffix(strings.TrimPrefix(argument, "{input:"), "}")
				identity, ok := target.Inputs[name]
				if !ok {
					return CompileResult{}, fmt.Errorf("fixture link input missing: %s", name)
				}
				argument = filepath.Join(inputRoot, identity.Path)
			}
			linkArguments = append(linkArguments, argument)
		}
	}
	if selected.Target == "darwin/arm64" {
		linkArguments = append(linkArguments, "-oso_prefix", work+string(filepath.Separator))
	}
	if err := server.backendCommand(ctx, "link native fixture", work, filepath.Join(inputRoot, target.Linker.Path), linkArguments...); err != nil {
		return CompileResult{}, err
	}
	info, err := os.Stat(staged)
	if err != nil {
		return CompileResult{}, fmt.Errorf("read native fixture output: %w", err)
	}
	staging, err := os.CreateTemp(artifactRoot, ".fixture-")
	if err != nil {
		return CompileResult{}, fmt.Errorf("stage native fixture output: %w", err)
	}
	stagedOutput := staging.Name()
	defer func() {
		if stagedOutput != "" {
			failure = errors.Join(failure, os.Remove(stagedOutput))
		}
	}()
	if err := staging.Close(); err != nil {
		return CompileResult{}, fmt.Errorf("close native fixture staging file: %w", err)
	}
	if err := os.Rename(staged, stagedOutput); err != nil {
		return CompileResult{}, fmt.Errorf("stage native fixture executable: %w", err)
	}
	if err := os.RemoveAll(work); err != nil {
		return CompileResult{}, fmt.Errorf("remove native fixture workspace before publication: %w", err)
	}
	work = ""
	result, err := server.publishArtifact(ctx, key, selected, record, stagedOutput, info.Size(), target.Runtime.SHA256)
	if err != nil {
		return CompileResult{}, err
	}
	stagedOutput = ""
	return result, nil
}

func (server *Server) publishArtifact(ctx context.Context, key string, selected CompileRequest, record *buildRecord, staged string, size int64, runtimeDigest string) (CompileResult, error) {
	server.mu.Lock()
	defer server.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return CompileResult{}, fmt.Errorf("publish native fixture: %w", err)
	}
	if server.builds[key] != record || record.waiters == 0 {
		return CompileResult{}, fmt.Errorf("publish native fixture: retired build generation for %q", selected.Source)
	}
	if err := server.makeSpaceLocked(size); err != nil {
		return CompileResult{}, fmt.Errorf("reserve native fixture output: %w", err)
	}
	artifact := filepath.Join(server.directory, "artifacts", key)
	if err := os.Rename(staged, artifact); err != nil {
		return CompileResult{}, fmt.Errorf("publish native fixture: %w", err)
	}
	server.stats.Bytes += size
	record.bytes = size
	record.result = CompileResult{Artifact: artifact, Runtime: runtimeDigest}
	close(record.done)
	return record.result, nil
}

var metadataDeclaration = regexp.MustCompile(`(?m)^@lo_program_metadata = constant \[([0-9]+) x i8\] c"([^"\n]*)", align 1$`)
var metadataLength = regexp.MustCompile(`(?m)^@lo_program_metadata_size = constant i64 [0-9]+$`)

func artifactMetadata(module []byte, sourcePath, sourceDigest, runtimeDigest string, metal json.RawMessage) ([]byte, error) {
	location := metadataDeclaration.FindSubmatchIndex(module)
	if location == nil {
		return nil, fmt.Errorf("emitted module lacks program metadata")
	}
	encoded := module[location[4]:location[5]]
	decoded := make([]byte, 0, len(encoded))
	for index := 0; index < len(encoded); index++ {
		if encoded[index] != '\\' {
			decoded = append(decoded, encoded[index])
			continue
		}
		if index+2 >= len(encoded) {
			return nil, fmt.Errorf("truncated LLVM metadata escape")
		}
		value, err := hex.DecodeString(string(encoded[index+1 : index+3]))
		if err != nil {
			return nil, fmt.Errorf("decode LLVM metadata escape: %w", err)
		}
		decoded = append(decoded, value[0])
		index += 2
	}
	declared, err := strconv.Atoi(string(module[location[2]:location[3]]))
	if err != nil || declared != len(decoded) {
		return nil, fmt.Errorf("LLVM metadata length differs from its declaration")
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(decoded, &metadata); err != nil {
		return nil, err
	}
	var artifact map[string]json.RawMessage
	if err := json.Unmarshal(metadata["artifact"], &artifact); err != nil {
		return nil, err
	}
	var emittedSource, emittedDigest string
	if err := json.Unmarshal(artifact["source"], &emittedSource); err != nil {
		return nil, fmt.Errorf("decode emitted source identity: %w", err)
	}
	if err := json.Unmarshal(artifact["source_sha256"], &emittedDigest); err != nil {
		return nil, fmt.Errorf("decode emitted source identity: %w", err)
	}
	if emittedSource != sourcePath || emittedDigest != sourceDigest {
		return nil, fmt.Errorf("emitted source identity differs from admitted source: path=%q digest=%s expected_path=%q expected_digest=%s", emittedSource, emittedDigest, sourcePath, sourceDigest)
	}
	artifact["runtime_sha256"], _ = json.Marshal(runtimeDigest)
	if len(metal) != 0 {
		artifact["metal"] = metal
	}
	metadata["artifact"], _ = json.Marshal(artifact)
	data, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	const hexDigits = "0123456789ABCDEF"
	var replacement strings.Builder
	fmt.Fprintf(&replacement, "@lo_program_metadata = constant [%d x i8] c\"", len(data))
	for _, value := range data {
		if value >= 32 && value < 127 && value != '"' && value != '\\' {
			replacement.WriteByte(value)
		} else {
			replacement.WriteByte('\\')
			replacement.WriteByte(hexDigits[value>>4])
			replacement.WriteByte(hexDigits[value&15])
		}
	}
	replacement.WriteString("\", align 1")
	result := make([]byte, 0, len(module)+replacement.Len())
	result = append(result, module[:location[0]]...)
	result = append(result, replacement.String()...)
	result = append(result, module[location[1]:]...)
	return metadataLength.ReplaceAll(result, []byte(fmt.Sprintf("@lo_program_metadata_size = constant i64 %d", len(data)))), nil
}
