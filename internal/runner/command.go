package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	BuildCommand              = "build"
	outputOption              = "--output"
	targetOption              = "--target"
	deviceBackendOption       = "--device-backend"
	emitOption                = "--emit"
	LLVMEmission              = "llvm"
	toolchainOption           = "--toolchain"
	ParseCommand              = "parse"
	CheckCommand              = "check"
	InspectCommand            = "inspect"
	RunCommand                = "run"
	ServeCommand              = "serve"
	listenOption              = "--listen"
	httpAllowOption           = "--http-allow"
	processAllowOption        = "--process-allow"
	processLauncherOption     = "--process-launcher"
	processReadRootOption     = "--process-read-root"
	processWriteRootOption    = "--process-write-root"
	processPruneRootOption    = "--process-prune-root"
	processUnixEndpointOption = "--process-unix-endpoint"
	readRootOption            = "--read-root"
	writeRootOption           = "--write-root"
	listRootOption            = "--list-root"
	removeRootOption          = "--remove-root"
	openFilesRootOption       = "--open-files-root"
	allowDeviceOption         = "--allow-device"
	maxStepsOption            = "--max-steps"
	maxValueBytesOption       = "--max-value-bytes"
	maxLiveBytesOption        = "--max-live-bytes"
	maxDataBytesOption        = "--max-data-bytes"
	maxListItemsOption        = "--max-list-items"
	maxFileEntriesOption      = "--max-file-entries"
	maxFileDirectoriesOption  = "--max-file-directories"
	formatOption              = "--format"
	TextFormat                = "text"
	JsonFormat                = "json"
	ExitSuccess               = 0
	ExitRejected              = 1
	ExitHost                  = 2
)

type invocation struct {
	command, path, format string
	output, target        string
	emit                  string
	toolchain             string
	deviceBackend         device.BuildBackend
	readRoots             []string
	writeRoots            []string
	listRoots             []string
	removeRoots           []string
	openFilesRoots        []string
	devices               device.Config
	budget                execution.Budget
	listenPolicy          adapter.ListenPolicy
	httpPolicy            adapter.HTTPPolicy
	processPolicy         adapter.ProcessPolicy
	programArguments      []string
}

func parseArgs(args []string, embedded bool) (invocation, error) {
	inv := invocation{format: TextFormat, budget: execution.DefaultBudget(), devices: device.DefaultConfig(), deviceBackend: device.SoftwareBuild}
	if len(args) == 0 {
		return inv, fmt.Errorf("expected a command: parse, check, inspect, run, or serve")
	}
	inv.command = args[0]
	formatSet := false
	budgetInput := execution.DefaultBudgetInput()
	policyOptions := map[string]*uint64{
		maxStepsOption:           &budgetInput.Steps,
		maxValueBytesOption:      &budgetInput.Limits.ValueBytes,
		maxLiveBytesOption:       &budgetInput.Limits.LiveBytes,
		maxDataBytesOption:       &budgetInput.Limits.DataBytes,
		maxListItemsOption:       &budgetInput.Limits.ListItems,
		maxFileEntriesOption:     &budgetInput.Limits.FileEntries,
		maxFileDirectoriesOption: &budgetInput.Limits.FileDirectories,
	}
	policySet := map[string]bool{}
	deviceSet := false
	backendSet := false
	emitSet := false
	var ports []uint16
	var httpGrants []string
	var processGrants, processReadRoots []string
	var processWrites, processPrunes, processEndpoints []adapter.ProcessPathScopeInput
	var processLauncher string
	processLauncherSet := false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if inv.command != RunCommand && inv.command != ServeCommand {
				return inv, fmt.Errorf("program arguments require run or serve")
			}
			inv.programArguments = append([]string(nil), args[i+1:]...)
			if err := value.ValidateInvocationArguments(inv.programArguments); err != nil {
				return inv, err
			}
			break
		}
		if arg == toolchainOption {
			if embedded || inv.command != BuildCommand || i+1 == len(args) {
				return inv, fmt.Errorf("--toolchain requires a value with build")
			}
			i++
			if args[i] == "" || inv.toolchain != "" {
				return inv, fmt.Errorf("--toolchain requires one nonempty value")
			}
			inv.toolchain = args[i]
			continue
		}
		if arg == emitOption {
			if embedded || inv.command != BuildCommand || emitSet || i+1 == len(args) {
				return inv, fmt.Errorf("--emit requires one llvm value with build")
			}
			i++
			emitSet = true
			if args[i] != LLVMEmission {
				return inv, fmt.Errorf("unsupported build emission %q", args[i])
			}
			inv.emit = args[i]
			continue
		}
		if arg == processAllowOption || arg == processLauncherOption || arg == processReadRootOption || arg == processWriteRootOption || arg == processPruneRootOption || arg == processUnixEndpointOption {
			if inv.command != RunCommand && inv.command != ServeCommand || i+1 == len(args) {
				return inv, fmt.Errorf("%s requires a value with run or serve", arg)
			}
			i++
			if args[i] == "" {
				return inv, fmt.Errorf("%s requires a nonempty value", arg)
			}
			switch arg {
			case processAllowOption:
				processGrants = append(processGrants, args[i])
			case processLauncherOption:
				if processLauncherSet {
					return inv, fmt.Errorf("duplicate --process-launcher")
				}
				processLauncherSet, processLauncher = true, args[i]
			case processReadRootOption:
				processReadRoots = append(processReadRoots, args[i])
			case processWriteRootOption, processPruneRootOption, processUnixEndpointOption:
				field := "root"
				if arg == processUnixEndpointOption {
					field = "endpoint"
				}
				scope, err := parseProcessScope(args[i], field)
				if err != nil {
					return inv, fmt.Errorf("%s: %w", arg, err)
				}
				switch arg {
				case processWriteRootOption:
					processWrites = append(processWrites, scope)
				case processPruneRootOption:
					processPrunes = append(processPrunes, scope)
				case processUnixEndpointOption:
					processEndpoints = append(processEndpoints, scope)
				}
			}
			continue
		}
		if arg == httpAllowOption {
			if inv.command != RunCommand && inv.command != ServeCommand || i+1 == len(args) {
				return inv, fmt.Errorf("--http-allow requires METHOD,ORIGIN,ADDRESS with run or serve")
			}
			i++
			httpGrants = append(httpGrants, args[i])
			continue
		}
		if arg == deviceBackendOption {
			if embedded || inv.command != BuildCommand || backendSet || i+1 == len(args) {
				return inv, fmt.Errorf("--device-backend requires one software or metal value with build")
			}
			i++
			backendSet = true
			var err error
			inv.deviceBackend, err = device.ParseBuildBackend(args[i])
			if err != nil {
				return inv, err
			}
			continue
		}
		if arg == outputOption || arg == targetOption {
			if embedded || inv.command != BuildCommand || i+1 == len(args) {
				return inv, fmt.Errorf("%s requires a value with build", arg)
			}
			i++
			if args[i] == "" {
				return inv, fmt.Errorf("empty %s", arg)
			}
			if arg == outputOption {
				if inv.output != "" {
					return inv, fmt.Errorf("duplicate --output")
				}
				inv.output = args[i]
			} else {
				if inv.target != "" {
					return inv, fmt.Errorf("duplicate --target")
				}
				inv.target = args[i]
			}
			continue
		}

		if arg == allowDeviceOption {
			if deviceSet || i+1 == len(args) || inv.command != RunCommand && inv.command != ServeCommand {
				return inv, fmt.Errorf("--allow-device requires one selector with run or serve")
			}
			i++
			deviceSet = true
			if args[i] == "" || args[i] == device.CPU {
				return inv, fmt.Errorf("--allow-device requires a non-CPU selector")
			}
			var err error
			inv.devices, err = device.NewConfig(args[i], device.DefaultFactory(), nil)
			if err != nil {
				return inv, err
			}
			continue
		}
		if arg == listenOption {
			if inv.command != ServeCommand || i+1 == len(args) {
				return inv, fmt.Errorf("--listen requires a port with serve")
			}
			i++
			port, err := strconv.ParseUint(args[i], 10, 16)
			if err != nil {
				return inv, fmt.Errorf("invalid listen port: %w", err)
			}
			ports = append(ports, uint16(port))
			continue
		}
		if limit, ok := policyOptions[arg]; ok {
			if inv.command != RunCommand && inv.command != ServeCommand || i+1 == len(args) {
				return inv, fmt.Errorf("%s requires a value with run or serve", arg)
			}
			if policySet[arg] {
				return inv, fmt.Errorf("duplicate %s", arg)
			}
			policySet[arg] = true
			i++
			amount, err := strconv.ParseUint(args[i], 10, 64)
			if err != nil {
				return inv, fmt.Errorf("invalid %s: %w", arg, err)
			}
			*limit = amount
			continue
		}
		if arg == readRootOption || arg == writeRootOption || arg == listRootOption || arg == removeRootOption || arg == openFilesRootOption {
			if inv.command != RunCommand && inv.command != ServeCommand || i+1 == len(args) {
				return inv, fmt.Errorf("%s requires a value with run or serve", arg)
			}
			i++
			if args[i] == "" {
				return inv, fmt.Errorf("empty adapter root")
			}
			switch arg {
			case readRootOption:
				inv.readRoots = append(inv.readRoots, args[i])
			case writeRootOption:
				inv.writeRoots = append(inv.writeRoots, args[i])
			case listRootOption:
				inv.listRoots = append(inv.listRoots, args[i])
			case removeRootOption:
				inv.removeRoots = append(inv.removeRoots, args[i])
			case openFilesRootOption:
				inv.openFilesRoots = append(inv.openFilesRoots, args[i])
			}
			continue
		}
		if arg == formatOption {
			if formatSet || i+1 == len(args) {
				return inv, fmt.Errorf("--format requires one text or json value")
			}
			i++
			inv.format = args[i]
			formatSet = true
			if inv.format != TextFormat && inv.format != JsonFormat {
				return inv, fmt.Errorf("unknown output format %q", inv.format)
			}
			continue
		}
		if len(arg) > 0 && arg[0] == '-' {
			return inv, fmt.Errorf("unknown option %q", arg)
		}
		if inv.path != "" {
			return inv, fmt.Errorf("expected one source path")
		}
		inv.path = arg
	}
	if inv.command != BuildCommand && inv.command != ParseCommand && inv.command != CheckCommand && inv.command != InspectCommand && inv.command != RunCommand && inv.command != ServeCommand {
		return inv, fmt.Errorf("unknown command %q", inv.command)
	}
	if inv.command == BuildCommand && inv.output == "" {
		return inv, fmt.Errorf("build requires --output")
	}
	if inv.toolchain != "" && inv.emit != "" {
		return inv, fmt.Errorf("--toolchain produces an executable and cannot be combined with --emit")
	}
	if !embedded && inv.path == "" {
		return inv, fmt.Errorf("expected one source path")
	}
	if embedded && (inv.path != "" || inv.command != RunCommand && inv.command != ServeCommand) {
		return inv, fmt.Errorf("artifact requires run or serve without a source path")
	}
	var err error
	inv.budget, err = execution.NewBudget(budgetInput)
	if err != nil {
		return inv, err
	}
	inv.listenPolicy, err = adapter.NewListenPolicy(ports)
	if err != nil {
		return inv, err
	}
	inv.httpPolicy, err = adapter.NewHTTPPolicy(httpGrants)
	if err != nil {
		return inv, err
	}
	inv.processPolicy, err = adapter.NewProcessPolicy(adapter.ProcessPolicyInput{Grants: processGrants, Launcher: processLauncher, ReadRoots: processReadRoots, Writes: processWrites, Prunes: processPrunes, Endpoints: processEndpoints})
	if err != nil {
		return inv, err
	}
	return inv, nil
}

func parseProcessScope(input, field string) (adapter.ProcessPathScopeInput, error) {
	if !strings.HasPrefix(strings.TrimSpace(input), "{") {
		return adapter.ProcessPathScopeInput{}, fmt.Errorf("expected one JSON object with executable and %s", field)
	}
	validation := json.NewDecoder(strings.NewReader(input))
	var document json.RawMessage
	if err := validation.Decode(&document); err != nil || !processScopeUnicode(string(document)) {
		return adapter.ProcessPathScopeInput{}, fmt.Errorf("malformed Process scope JSON")
	}
	var trailing json.RawMessage
	if err := validation.Decode(&trailing); err != io.EOF {
		return adapter.ProcessPathScopeInput{}, fmt.Errorf("Process scope must contain one JSON object")
	}
	decoder := json.NewDecoder(strings.NewReader(input))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return adapter.ProcessPathScopeInput{}, fmt.Errorf("expected one JSON object with executable and %s", field)
	}
	values := map[string]string{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return adapter.ProcessPathScopeInput{}, fmt.Errorf("read Process scope field: %w", err)
		}
		name, ok := token.(string)
		if !ok || name != "executable" && name != field {
			return adapter.ProcessPathScopeInput{}, fmt.Errorf("unknown Process scope field")
		}
		if _, exists := values[name]; exists {
			return adapter.ProcessPathScopeInput{}, fmt.Errorf("duplicate Process scope field")
		}
		var raw any
		if err := decoder.Decode(&raw); err != nil {
			return adapter.ProcessPathScopeInput{}, fmt.Errorf("malformed Process scope JSON")
		}
		value, text := raw.(string)
		if !text {
			return adapter.ProcessPathScopeInput{}, fmt.Errorf("Process scope field must contain text")
		}
		values[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		return adapter.ProcessPathScopeInput{}, fmt.Errorf("close Process scope: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return adapter.ProcessPathScopeInput{}, fmt.Errorf("Process scope must contain one JSON object")
	}
	if len(values) != 2 || values["executable"] == "" || values[field] == "" {
		return adapter.ProcessPathScopeInput{}, fmt.Errorf("Process scope requires executable and %s", field)
	}
	return adapter.ProcessPathScopeInput{Executable: values["executable"], Path: values[field]}, nil
}

func processScopeUnicode(input string) bool {
	if !utf8.ValidString(input) {
		return false
	}
	for offset := 0; offset < len(input); {
		if input[offset] != '\\' {
			offset++
			continue
		}
		if offset+1 == len(input) {
			return false
		}
		if input[offset+1] != 'u' {
			offset += 2
			continue
		}
		if offset+6 > len(input) {
			return false
		}
		first, err := strconv.ParseUint(input[offset+2:offset+6], 16, 16)
		if err != nil {
			return false
		}
		offset += 6
		if first >= 0xD800 && first <= 0xDBFF {
			if offset+6 > len(input) || input[offset:offset+2] != `\u` {
				return false
			}
			second, err := strconv.ParseUint(input[offset+2:offset+6], 16, 16)
			if err != nil || second < 0xDC00 || second > 0xDFFF {
				return false
			}
			offset += 6
		} else if first >= 0xDC00 && first <= 0xDFFF {
			return false
		}
	}
	return true
}

type Options interface {
	Command() string
	Path() string
	Format() string
	Output() string
	Target() string
	Emit() string
	Toolchain() string
	DeviceBackend() device.BuildBackend
	options()
}

func (inv invocation) options()                    {}
func (inv invocation) Command() string             { return inv.command }
func (inv invocation) Path() string                { return inv.path }
func (inv invocation) Format() string              { return inv.format }
func ParseCompiler(args []string) (Options, error) { return parseArgs(args, false) }
func Execute(program execution.Program, args []string, stdout, stderr io.Writer) int {
	if adapter.HandleFileProbe(args) {
		return ExitSuccess
	}
	inv, err := parseArgs(args, true)
	if err != nil {
		return Report(stderr, inv.format, diagnostic.New(diagnostic.Invocation, diagnostic.CLI, err.Error(), nil), ExitHost)
	}
	return executeProgram(inv, program, stdout, stderr)
}
func Run(inv Options, program execution.Program, stdout, stderr io.Writer) int {
	return executeProgram(inv.(invocation), program, stdout, stderr)
}
func Main(program execution.Program) { os.Exit(Execute(program, os.Args[1:], os.Stdout, os.Stderr)) }
func ReportRuntime(stderr io.Writer, format string, err error) int {
	var failure execution.Failure
	if errors.As(err, &failure) {
		v := failure.Value()
		d := diagnostic.New(diagnostic.RuntimeUnhandled, diagnostic.Runtime, err.Error(), v.Span())
		if format == JsonFormat {
			type runtimeDiagnostic struct {
				diagnostic.Data
				Error     value.Error                  `json:"error"`
				Flows     []value.Record               `json:"flow_progress,omitempty"`
				Completed execution.ActivationProgress `json:"last_completed_activation,omitempty"`
				Cleanup   []string                     `json:"cleanup_errors,omitempty"`
			}
			payload := runtimeDiagnostic{d.Data(), v, execution.FlowDetails(err), execution.ActivationDetails(err), execution.CleanupDetails(err)}
			if err := json.NewEncoder(stderr).Encode(struct {
				Diagnostics []runtimeDiagnostic `json:"diagnostics"`
			}{[]runtimeDiagnostic{payload}}); err != nil {
				return ExitHost
			}
			return ExitRejected
		}
		if Report(stderr, format, d, ExitRejected) == ExitHost {
			return ExitHost
		}
		for _, frame := range v.Trace() {
			if _, err := fmt.Fprintf(stderr, "  flow %q, operation %q, input %s\n", frame.Field("flow").(value.Text).Text(), frame.Field("operation").(value.Text).Text(), value.TextResult(frame.Field("input"))); err != nil {
				return ExitHost
			}
		}
		return ExitRejected
	}
	code := diagnostic.RuntimeHost
	var host execution.HostError
	if errors.As(err, &host) {
		code = host.Code()
	}
	entries, progress, removal := execution.RemovalDetails(err)
	flows := execution.FlowDetails(err)
	completed := execution.ActivationDetails(err)
	cleanup := execution.CleanupDetails(err)
	httpProgress, progressErr := execution.HTTPProgress(err)
	processProgress := execution.ProcessProgress(err)
	if progressErr != nil {
		return Report(stderr, format, diagnostic.New(diagnostic.RuntimeHost, diagnostic.Runtime, fmt.Sprintf("HTTP progress: %v; original failure: %v", progressErr, err), nil), ExitHost)
	}
	var writeProgress value.Record
	if confirmed, stage, ok := execution.WriteProgress(err); ok {
		stageValue, valueErr := value.NewText(types.String(), stage)
		if valueErr == nil {
			writeProgress, valueErr = value.NewRecord([]value.Field{{Name: "bytes_written", Value: value.Int64(int64(confirmed))}, {Name: "stage", Value: stageValue}})
		}
		if valueErr != nil {
			return Report(stderr, format, diagnostic.New(diagnostic.RuntimeHost, diagnostic.Runtime, fmt.Sprintf("write progress: %v; original failure: %v", valueErr, err), nil), ExitHost)
		}
	}
	if removal || len(flows) > 0 || writeProgress != nil || httpProgress != nil || processProgress != nil || completed != nil || len(cleanup) > 0 {
		data := diagnostic.New(code, diagnostic.Runtime, err.Error(), nil).Data()
		if format == JsonFormat {
			type removalDiagnostic struct {
				diagnostic.Data
				Entries   value.Value                  `json:"removal_entries,omitempty"`
				Progress  value.Record                 `json:"removal_progress,omitempty"`
				Flows     []value.Record               `json:"flow_progress,omitempty"`
				Write     value.Record                 `json:"write_progress,omitempty"`
				HTTP      value.Record                 `json:"http_progress,omitempty"`
				Process   value.Record                 `json:"process_progress,omitempty"`
				Completed execution.ActivationProgress `json:"last_completed_activation,omitempty"`
				Cleanup   []string                     `json:"cleanup_errors,omitempty"`
			}
			var snapshot value.Value
			if entries != nil {
				snapshot = value.Snapshot(entries)
			}
			payload := struct {
				Diagnostics []removalDiagnostic `json:"diagnostics"`
			}{[]removalDiagnostic{{data, snapshot, progress, flows, writeProgress, httpProgress, processProgress, completed, cleanup}}}
			if err := json.NewEncoder(stderr).Encode(payload); err != nil {
				return ExitHost
			}
			return ExitHost
		}
		message := err.Error()
		if removal {
			message += fmt.Sprintf(" (completed %s removals, stage %s)", value.TextResult(progress.Field("completed_count")), value.TextResult(progress.Field("stage")))
		}
		if writeProgress != nil {
			message += fmt.Sprintf(" (wrote %s bytes, stage %s)", value.TextResult(writeProgress.Field("bytes_written")), value.TextResult(writeProgress.Field("stage")))
		}
		if httpProgress != nil {
			message += fmt.Sprintf(" (HTTP stage %s, request written %s, remote outcome %s)", value.TextResult(httpProgress.Field("stage")), value.TextResult(httpProgress.Field("request_written")), value.TextResult(httpProgress.Field("remote_outcome")))
		}
		if processProgress != nil {
			message += " (Process progress " + value.TextResult(processProgress) + ")"
		}
		if completed != nil {
			message += fmt.Sprintf(" (last completed %s at %s:%d: %s)", completed.Operation(), completed.Span().File(), completed.Span().Start().Line, completed.Summary())
		}
		for _, flow := range flows {
			operation := flow.Field("operation").(value.Text).Text()
			if operation == catalog.FlowWorklist {
				message += fmt.Sprintf(" (%s completed %s, pending %s, state %s)", operation, value.TextResult(flow.Field("completed_count")), value.TextResult(flow.Field("pending_count")), value.TextResult(flow.Field("state")))
			} else {
				message += fmt.Sprintf(" (%s completed %s/%s, state %s)", operation, value.TextResult(flow.Field("completed_count")), value.TextResult(flow.Field("total_count")), value.TextResult(flow.Field("state")))
			}
		}
		return Report(stderr, format, diagnostic.New(code, diagnostic.Runtime, message, nil), ExitHost)
	}
	return Report(stderr, format, diagnostic.New(code, diagnostic.Runtime, err.Error(), nil), ExitHost)
}
func Report(stderr io.Writer, format string, d diagnostic.Diagnostic, status int) int {
	var err error
	if format == JsonFormat {
		err = json.NewEncoder(stderr).Encode(struct {
			Diagnostics []diagnostic.Data `json:"diagnostics"`
		}{[]diagnostic.Data{d.Data()}})
	} else {
		_, err = fmt.Fprintln(stderr, d.Error())
	}
	if err != nil {
		return ExitHost
	}
	return status
}

func (inv invocation) Output() string                     { return inv.output }
func (inv invocation) Target() string                     { return inv.target }
func (inv invocation) Emit() string                       { return inv.emit }
func (inv invocation) Toolchain() string                  { return inv.toolchain }
func (inv invocation) DeviceBackend() device.BuildBackend { return inv.deviceBackend }
