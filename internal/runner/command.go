package runner

import (
	"encoding/json"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"io"
	"os"
	"strconv"
)

const (
	BuildCommand        = "build"
	outputOption        = "--output"
	targetOption        = "--target"
	deviceBackendOption = "--device-backend"
	ParseCommand        = "parse"
	CheckCommand        = "check"
	InspectCommand      = "inspect"
	RunCommand          = "run"
	ServeCommand        = "serve"
	listenOption        = "--listen"
	readRootOption      = "--read-root"
	writeRootOption     = "--write-root"
	allowDeviceOption   = "--allow-device"
	maxStepsOption      = "--max-steps"
	formatOption        = "--format"
	TextFormat          = "text"
	JsonFormat          = "json"
	ExitSuccess         = 0
	ExitRejected        = 1
	ExitHost            = 2
)

type invocation struct {
	command, path, format string
	output, target        string
	deviceBackend         device.BuildBackend
	readRoots             []string
	writeRoots            []string
	devices               device.Config
	budget                execution.Budget
	listenPolicy          adapter.ListenPolicy
}

func parseArgs(args []string, embedded bool) (invocation, error) {
	inv := invocation{format: TextFormat, budget: execution.DefaultBudget(), devices: device.DefaultConfig(), deviceBackend: device.SoftwareBuild}
	if len(args) == 0 {
		return inv, fmt.Errorf("expected a command: parse, check, inspect, run, or serve")
	}
	inv.command = args[0]
	formatSet := false
	stepsSet := false
	deviceSet := false
	backendSet := false
	var ports []uint16
	for i := 1; i < len(args); i++ {
		arg := args[i]
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
		if arg == readRootOption || arg == writeRootOption || arg == maxStepsOption {
			if inv.command != RunCommand && inv.command != ServeCommand || i+1 == len(args) {
				return inv, fmt.Errorf("%s requires a value with run or serve", arg)
			}
			i++
			if arg == readRootOption || arg == writeRootOption {
				if args[i] == "" {
					return inv, fmt.Errorf("empty adapter root")
				}
				if arg == readRootOption {
					inv.readRoots = append(inv.readRoots, args[i])
				} else {
					inv.writeRoots = append(inv.writeRoots, args[i])
				}
			} else {
				if stepsSet {
					return inv, fmt.Errorf("duplicate --max-steps")
				}
				stepsSet = true
				steps, err := strconv.ParseUint(args[i], 10, 64)
				if err != nil {
					return inv, fmt.Errorf("invalid --max-steps: %w", err)
				}
				inv.budget, err = execution.NewBudget(steps)
				if err != nil {
					return inv, err
				}
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
	if !embedded && inv.path == "" {
		return inv, fmt.Errorf("expected one source path")
	}
	if embedded && (inv.path != "" || inv.command != RunCommand && inv.command != ServeCommand) {
		return inv, fmt.Errorf("artifact requires run or serve without a source path")
	}
	var err error
	inv.listenPolicy, err = adapter.NewListenPolicy(ports)
	if err != nil {
		return inv, err
	}
	return inv, nil
}

type Options interface {
	Command() string
	Path() string
	Format() string
	Output() string
	Target() string
	DeviceBackend() device.BuildBackend
	options()
}

func (inv invocation) options()                    {}
func (inv invocation) Command() string             { return inv.command }
func (inv invocation) Path() string                { return inv.path }
func (inv invocation) Format() string              { return inv.format }
func ParseCompiler(args []string) (Options, error) { return parseArgs(args, false) }
func Execute(program execution.Program, args []string, stdout, stderr io.Writer) int {
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
	if failure, ok := err.(execution.Failure); ok {
		v := failure.Value()
		d := diagnostic.New(diagnostic.RuntimeUnhandled, diagnostic.Runtime, err.Error(), v.Span())
		if format == JsonFormat {
			type runtimeDiagnostic struct {
				diagnostic.Data
				Error value.Error `json:"error"`
			}
			payload := runtimeDiagnostic{d.Data(), v}
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
	if host, ok := err.(execution.HostError); ok {
		code = host.Code()
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
func (inv invocation) DeviceBackend() device.BuildBackend { return inv.deviceBackend }
