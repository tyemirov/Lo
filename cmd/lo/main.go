package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/inspect"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

const (
	parseCommand    = "parse"
	checkCommand    = "check"
	inspectCommand  = "inspect"
	runCommand      = "run"
	serveCommand    = "serve"
	listenOption    = "--listen"
	readRootOption  = "--read-root"
	writeRootOption = "--write-root"
	maxStepsOption  = "--max-steps"
	formatOption    = "--format"
	textFormat      = "text"
	jsonFormat      = "json"
	exitSuccess     = 0
	exitRejected    = 1
	exitHost        = 2
)

type invocation struct {
	command, path, format string
	readRoots             []string
	writeRoots            []string
	budget                eval.Budget
	listenPolicy          adapter.ListenPolicy
}

func main() { os.Exit(execute(os.Args[1:], os.Stdout, os.Stderr)) }
func parseArgs(args []string) (invocation, error) {
	inv := invocation{format: textFormat, budget: eval.DefaultBudget()}
	if len(args) == 0 {
		return inv, fmt.Errorf("expected a command: parse, check, inspect, run, or serve")
	}
	inv.command = args[0]
	formatSet := false
	stepsSet := false
	var ports []uint16
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == listenOption {
			if inv.command != serveCommand || i+1 == len(args) {
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
			if inv.command != runCommand && inv.command != serveCommand || i+1 == len(args) {
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
				inv.budget, err = eval.NewBudget(steps)
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
			if inv.format != textFormat && inv.format != jsonFormat {
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
	if inv.command != parseCommand && inv.command != checkCommand && inv.command != inspectCommand && inv.command != runCommand && inv.command != serveCommand {
		return inv, fmt.Errorf("unknown command %q", inv.command)
	}
	if inv.path == "" {
		return inv, fmt.Errorf("expected one source path")
	}
	var err error
	inv.listenPolicy, err = adapter.NewListenPolicy(ports)
	if err != nil {
		return inv, err
	}
	return inv, nil
}
func execute(args []string, stdout, stderr io.Writer) int {
	inv, err := parseArgs(args)
	if err != nil {
		return report(stderr, inv.format, diagnostic.New(diagnostic.Invocation, diagnostic.CLI, err.Error(), nil), exitHost)
	}
	file, err := readSource(inv.path)
	if err != nil {
		return report(stderr, inv.format, diagnostic.New(diagnostic.ReadFailed, diagnostic.Source, err.Error(), nil), exitHost)
	}
	src, err := source.New(inv.path, file)
	if err != nil {
		return report(stderr, inv.format, diagnostic.New(diagnostic.SourceLimit, diagnostic.Source, err.Error(), nil), exitRejected)
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	if inv.format == textFormat {
		encoder.SetIndent("", "  ")
	}
	if inv.command == inspectCommand {
		result, d := compiler.Inspect(src)
		if d != nil {
			return report(stderr, inv.format, d, exitRejected)
		}
		if inv.format == jsonFormat {
			err = encoder.Encode(struct {
				Command string         `json:"command"`
				Report  inspect.Report `json:"report"`
			}{inspectCommand, result})
		} else {
			_, err = io.WriteString(stdout, result.Text())
		}
	} else if inv.command == parseCommand {
		program, d := compiler.Parse(src)
		if d != nil {
			return report(stderr, inv.format, d, exitRejected)
		}
		err = encoder.Encode(struct {
			Command string         `json:"command"`
			Program syntax.Program `json:"program"`
		}{parseCommand, program})
	} else {
		result, d := compiler.Check(src)
		if d != nil {
			return report(stderr, inv.format, d, exitRejected)
		}
		if inv.command == runCommand || inv.command == serveCommand {
			return executeProgram(inv, result, stdout, stderr)
		}
		output := ""
		if t := result.ResultType(); t != nil {
			output = t.String()
		}
		if inv.format == jsonFormat {
			err = encoder.Encode(struct {
				Command    string `json:"command"`
				Status     string `json:"status"`
				File       string `json:"file"`
				ResultType string `json:"result_type,omitempty"`
			}{checkCommand, "ok", inv.path, output})
		} else {
			_, err = fmt.Fprintf(stdout, "%s: ok\n", inv.path)
		}
	}
	if err != nil {
		return report(stderr, inv.format, diagnostic.New(diagnostic.Invocation, diagnostic.CLI, "write command result: "+err.Error(), nil), exitHost)
	}
	return exitSuccess
}
func reportRuntime(stderr io.Writer, format string, err error) int {
	if failure, ok := err.(eval.Failure); ok {
		v := failure.Value()
		d := diagnostic.New(diagnostic.RuntimeUnhandled, diagnostic.Runtime, err.Error(), v.Span())
		if format == jsonFormat {
			type runtimeDiagnostic struct {
				diagnostic.Data
				Error value.Error `json:"error"`
			}
			payload := runtimeDiagnostic{d.Data(), v}
			if err := json.NewEncoder(stderr).Encode(struct {
				Diagnostics []runtimeDiagnostic `json:"diagnostics"`
			}{[]runtimeDiagnostic{payload}}); err != nil {
				return exitHost
			}
			return exitRejected
		}
		if report(stderr, format, d, exitRejected) == exitHost {
			return exitHost
		}
		for _, frame := range v.Trace() {
			if _, err := fmt.Fprintf(stderr, "  flow %q, operation %q, input %s\n", frame.Field("flow").(value.Text).Text(), frame.Field("operation").(value.Text).Text(), value.TextResult(frame.Field("input"))); err != nil {
				return exitHost
			}
		}
		return exitRejected
	}
	code := diagnostic.RuntimeHost
	if host, ok := err.(eval.HostError); ok {
		code = host.Code()
	}
	return report(stderr, format, diagnostic.New(code, diagnostic.Runtime, err.Error(), nil), exitHost)
}
func readSource(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read source %s: %w", path, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(f, source.MaxBytes+1))
	closeErr := f.Close()
	if readErr != nil {
		return "", fmt.Errorf("read source %s: %w", path, readErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close source %s: %w", path, closeErr)
	}
	return string(data), nil
}
func report(stderr io.Writer, format string, d diagnostic.Diagnostic, status int) int {
	var err error
	if format == jsonFormat {
		err = json.NewEncoder(stderr).Encode(struct {
			Diagnostics []diagnostic.Data `json:"diagnostics"`
		}{[]diagnostic.Data{d.Data()}})
	} else {
		_, err = fmt.Fprintln(stderr, d.Error())
	}
	if err != nil {
		return exitHost
	}
	return status
}
