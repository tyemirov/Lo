package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/inspect"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativebuild"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() { os.Exit(execute(os.Args[1:], os.Stdout, os.Stderr)) }
func execute(args []string, stdout, stderr io.Writer) int {
	if adapter.HandleFileProbe(args) {
		return 0
	}
	inv, err := runner.ParseCompiler(args)
	if err != nil {
		return runner.Report(stderr, inv.Format(), diagnostic.New(diagnostic.Invocation, diagnostic.CLI, err.Error(), nil), runner.ExitHost)
	}
	file, err := readSource(inv.Path())
	if err != nil {
		return runner.Report(stderr, inv.Format(), diagnostic.New(diagnostic.ReadFailed, diagnostic.Source, err.Error(), nil), runner.ExitHost)
	}
	src, err := source.New(inv.Path(), file)
	if err != nil {
		return runner.Report(stderr, inv.Format(), diagnostic.New(diagnostic.SourceLimit, diagnostic.Source, err.Error(), nil), runner.ExitRejected)
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	if inv.Format() == runner.TextFormat {
		encoder.SetIndent("", "  ")
	}
	if inv.Command() == runner.InspectCommand {
		result, d := compiler.Inspect(src)
		if d != nil {
			return runner.Report(stderr, inv.Format(), d, runner.ExitRejected)
		}
		if inv.Format() == runner.JsonFormat {
			err = encoder.Encode(struct {
				Command string         `json:"command"`
				Report  inspect.Report `json:"report"`
			}{runner.InspectCommand, result})
		} else {
			_, err = io.WriteString(stdout, result.Text())
		}
	} else if inv.Command() == runner.ParseCommand {
		program, d := compiler.Parse(src)
		if d != nil {
			return runner.Report(stderr, inv.Format(), d, runner.ExitRejected)
		}
		err = encoder.Encode(struct {
			Command string         `json:"command"`
			Program syntax.Program `json:"program"`
		}{runner.ParseCommand, program})
	} else {
		result, d := compiler.Check(src)
		if d != nil {
			return runner.Report(stderr, inv.Format(), d, runner.ExitRejected)
		}
		if inv.Command() == runner.BuildCommand {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			var receipt nativebuild.Receipt
			var d diagnostic.Diagnostic
			if inv.Emit() == runner.LLVMEmission {
				receipt, d = nativebuild.EmitLLVM(ctx, result, inv.Output(), inv.Target(), inv.DeviceBackend())
			} else {
				receipt, d = nativebuild.Build(ctx, result, inv.Output(), inv.Target(), inv.DeviceBackend())
			}
			if d != nil {
				return runner.Report(stderr, inv.Format(), d, runner.ExitHost)
			}
			if inv.Format() == runner.JsonFormat {
				err = encoder.Encode(receipt)
			} else {
				if receipt.Emit == runner.LLVMEmission {
					_, err = fmt.Fprintf(stdout, "%s: emitted LLVM module for %s (%s)\n", receipt.Artifact, receipt.Target, receipt.ABI)
				} else {
					_, err = fmt.Fprintf(stdout, "%s: built %s\n", receipt.Artifact, receipt.Target)
				}
			}
			if err != nil {
				return runner.Report(stderr, inv.Format(), diagnostic.New(diagnostic.Invocation, diagnostic.CLI, err.Error(), nil), runner.ExitHost)
			}
			return runner.ExitSuccess
		}
		if inv.Command() == runner.RunCommand || inv.Command() == runner.ServeCommand {
			program, err := eval.Program(result)
			if err != nil {
				return runner.ReportRuntime(stderr, inv.Format(), err)
			}
			return runner.Run(inv, program, stdout, stderr)
		}
		output := ""
		if t := result.ResultType(); t != nil {
			output = t.String()
		}
		if inv.Format() == runner.JsonFormat {
			err = encoder.Encode(struct {
				Command    string `json:"command"`
				Status     string `json:"status"`
				File       string `json:"file"`
				ResultType string `json:"result_type,omitempty"`
			}{runner.CheckCommand, "ok", inv.Path(), output})
		} else {
			_, err = fmt.Fprintf(stdout, "%s: ok\n", inv.Path())
		}
	}
	if err != nil {
		return runner.Report(stderr, inv.Format(), diagnostic.New(diagnostic.Invocation, diagnostic.CLI, "write command result: "+err.Error(), nil), runner.ExitHost)
	}
	return runner.ExitSuccess
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
