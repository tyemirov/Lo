package compilercli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/targetconfig"
)

const (
	ParseCommand    = "parse"
	CheckCommand    = "check"
	InspectCommand  = "inspect"
	BuildCommand    = "build"
	LLVMEmission    = "llvm"
	TextFormat      = "text"
	JsonFormat      = "json"
	ExitSuccess     = 0
	ExitRejected    = 1
	ExitHost        = 2
	formatOption    = "--format"
	outputOption    = "--output"
	targetOption    = "--target"
	emitOption      = "--emit"
	toolchainOption = "--toolchain"
	backendOption   = "--device-backend"
)

type Options interface {
	Command() string
	Path() string
	Format() string
	Output() string
	Target() string
	Emit() string
	Toolchain() string
	DeviceBackend() targetconfig.BuildBackend
	options()
}

type invocation struct {
	command, path, format, output, target, emit, toolchain string
	backend                                                targetconfig.BuildBackend
}

func (inv invocation) options()                                 {}
func (inv invocation) Command() string                          { return inv.command }
func (inv invocation) Path() string                             { return inv.path }
func (inv invocation) Format() string                           { return inv.format }
func (inv invocation) Output() string                           { return inv.output }
func (inv invocation) Target() string                           { return inv.target }
func (inv invocation) Emit() string                             { return inv.emit }
func (inv invocation) Toolchain() string                        { return inv.toolchain }
func (inv invocation) DeviceBackend() targetconfig.BuildBackend { return inv.backend }

func Parse(args []string) (Options, error) {
	inv := invocation{format: TextFormat, backend: targetconfig.SoftwareBuild}
	// Error formatting is selected before command rejection, without source I/O.
	for index, arg := range args {
		if arg == "--" {
			break
		}
		if arg == formatOption && index+1 < len(args) && args[index+1] == JsonFormat {
			inv.format = JsonFormat
			break
		}
	}
	if len(args) == 0 {
		return inv, fmt.Errorf("expected a command: parse, check, inspect, or build")
	}
	inv.command = args[0]
	switch inv.command {
	case ParseCommand, CheckCommand, InspectCommand, BuildCommand:
	default:
		return inv, fmt.Errorf("unknown compiler command %q; expected parse, check, inspect, or build", inv.command)
	}
	seen := map[string]bool{}
	for index := 1; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case formatOption, outputOption, targetOption, emitOption, toolchainOption, backendOption:
			if seen[arg] {
				return inv, fmt.Errorf("duplicate %s", arg)
			}
			if arg != formatOption && inv.command != BuildCommand {
				return inv, fmt.Errorf("%s requires build", arg)
			}
			if index+1 == len(args) {
				return inv, fmt.Errorf("%s requires one nonempty value", arg)
			}
			index++
			value := args[index]
			if value == "" {
				return inv, fmt.Errorf("empty %s", arg)
			}
			seen[arg] = true
			switch arg {
			case formatOption:
				if value != TextFormat && value != JsonFormat {
					return inv, fmt.Errorf("unknown output format %q", value)
				}
				inv.format = value
			case outputOption:
				inv.output = value
			case targetOption:
				inv.target = value
			case toolchainOption:
				inv.toolchain = value
			case emitOption:
				if value != LLVMEmission {
					return inv, fmt.Errorf("unsupported build emission %q", value)
				}
				inv.emit = value
			case backendOption:
				backend, err := targetconfig.ParseBuildBackend(value)
				if err != nil {
					return inv, err
				}
				inv.backend = backend
			}
		default:
			if strings.HasPrefix(arg, "-") {
				return inv, fmt.Errorf("unknown compiler option %q", arg)
			}
			if inv.path != "" || arg == "" {
				return inv, fmt.Errorf("expected one source path")
			}
			inv.path = arg
		}
	}
	if inv.command == BuildCommand && inv.output == "" {
		return inv, fmt.Errorf("build requires --output")
	}
	if inv.toolchain != "" && inv.emit != "" {
		return inv, fmt.Errorf("--toolchain produces an executable and cannot be combined with --emit")
	}
	if inv.path == "" {
		return inv, fmt.Errorf("expected one source path")
	}
	return inv, nil
}

func Report(stderr io.Writer, format string, failure diagnostic.Diagnostic, status int) int {
	var err error
	if format == JsonFormat {
		err = json.NewEncoder(stderr).Encode(struct {
			Diagnostics []diagnostic.Data `json:"diagnostics"`
		}{[]diagnostic.Data{failure.Data()}})
	} else {
		_, err = fmt.Fprintln(stderr, failure.Error())
	}
	if err != nil {
		return ExitHost
	}
	return status
}
