package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/processhost"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type executionResult struct {
	Command  string               `json:"command"`
	Status   string               `json:"status"`
	Servers  []adapter.ServerInfo `json:"servers,omitempty"`
	Result   value.Value          `json:"result,omitempty"`
	ExitCode *int                 `json:"exit_code,omitempty"`
}

const consoleProtocolTimeout = time.Second

func executeProgram(inv invocation, p execution.Program, console *adapter.Console) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runProgram(ctx, inv, p, console)
}

// RunContext executes the CLI contract with explicit backend dependencies.
func RunContext(ctx context.Context, options Options, p execution.Program, factory device.Factory, observer device.Observer, console *adapter.Console) int {
	inv := options.(invocation)
	config, err := device.NewConfig(inv.devices.Grant(), factory, observer)
	if err != nil {
		return finishConsole(console, reportConsole(console, inv.format, execution.HostFailure("configure device backend", "command", err)))
	}
	inv.devices = config
	return runProgram(ctx, inv, p, console)
}

func reportConsole(console *adapter.Console, format string, err error) int {
	reporting, cancel := context.WithTimeout(context.Background(), consoleProtocolTimeout)
	defer cancel()
	return ReportRuntime(console.Writer(reporting, adapter.ConsoleStderr), format, err)
}

func finishConsole(console *adapter.Console, status int) int {
	cleanup, cancel := context.WithTimeout(context.Background(), consoleProtocolTimeout)
	defer cancel()
	err := console.Close(cleanup)
	err = execution.PreserveCleanup(err, console.WaitRetained())
	if err != nil {
		return ExitHost
	}
	return status
}

func runProgram(ctx context.Context, inv invocation, p execution.Program, console *adapter.Console) int {
	if err := console.SetRendering(inv.command, inv.format); err != nil {
		return finishConsole(console, reportConsole(console, inv.format, err))
	}
	return finishConsole(console, runProgramOutput(ctx, inv, p, console))
}

func runProgramOutput(ctx context.Context, inv invocation, p execution.Program, console *adapter.Console) int {
	stdout := console.Writer(ctx, adapter.ConsoleStdout)
	report := func(err error) int { return reportConsole(console, inv.format, err) }
	resultType := p.ResultType()
	if fallible, ok := resultType.(types.Fallible); ok {
		resultType = fallible.Base()
	}
	if inv.command == ServeCommand && resultType != nil && types.Equal(resultType, types.ProgramExit()) {
		return report(execution.HostFailure("serve", "entry result", errors.New("Program.Exit requires run")))
	}
	ctx = execution.CaptureProcessContext(ctx)
	ctx = execution.CaptureHostInvocation(ctx, inv.programArguments)
	limits := adapter.DefaultProcessLimits()
	configuration, err := processhost.NewConfig(inv.processPolicy, limits)
	if err != nil {
		return report(err)
	}
	configuration, err = processhost.WithProtectedImages(configuration, console.ProtectedImages())
	if err != nil {
		return report(err)
	}
	hostSession, err := processhost.Open(ctx, configuration, adapter.FileRoots{Read: inv.readRoots, Write: inv.writeRoots, List: inv.listRoots, Remove: inv.removeRoots, OpenFiles: inv.openFilesRoots})
	if err != nil {
		return report(err)
	}
	closeHost := func() error {
		cleanup, cancel := context.WithTimeout(context.Background(), limits.CleanupTimeout())
		defer cancel()
		return execution.PreserveCleanup(hostSession.Close(cleanup), hostSession.WaitRetained())
	}
	boundaries, err := adapter.WithHTTP(hostSession.Boundary(), inv.httpPolicy)
	if err != nil {
		return report(runtimeCleanup(err, closeHost()))
	}
	boundaries = console.Wrap(boundaries)
	if inv.command == RunCommand {
		ctx, capture, err := execution.CaptureFailure(ctx)
		if err != nil {
			return report(runtimeCleanup(err, closeHost()))
		}
		output, runErr := p.Run(ctx, boundaries, inv.budget, inv.devices)
		if err := capture.Preserve(runtimeCleanup(runErr, closeHost())); err != nil {
			return report(err)
		}
		terminalOutput := output
		if fallible, ok := output.(value.Fallible); ok {
			terminalOutput, _ = fallible.Outcome()
		}
		if terminal, ok := terminalOutput.(value.ProgramExit); ok {
			if err := console.WriteTerminal(ctx, terminal.Stdout().Bytes(), terminal.Stderr().Bytes()); err != nil {
				return report(capture.Preserve(err))
			}
			code := terminal.Code()
			if inv.format == JsonFormat {
				if err := writeExecution(stdout, inv.format, executionResult{Command: RunCommand, Status: "ok", ExitCode: &code}); err != nil {
					return report(capture.Preserve(execution.HostFailure("write terminal result", inv.path, err)))
				}
			}
			return code
		}
		if err := writeExecution(stdout, inv.format, executionResult{Command: RunCommand, Status: "ok", Result: output}); err != nil {
			return report(capture.Preserve(execution.HostFailure("write command result", inv.path, err)))
		}
		return ExitSuccess
	}
	servers, err := adapter.NewServers(p, boundaries, inv.listenPolicy, inv.budget, inv.devices)
	if err != nil {
		return report(runtimeCleanup(err, closeHost()))
	}
	cleanup := func(cause error) error {
		shutdown, cancel := context.WithTimeout(context.Background(), adapter.ShutdownTimeout)
		defer cancel()
		serverErr := servers.Shutdown(shutdown)
		return runtimeCleanup(cause, errors.Join(serverErr, closeHost()))
	}
	output, err := servers.Start(ctx)
	if err != nil {
		return report(cleanup(err))
	}
	active := servers.Snapshot()
	select {
	case err := <-servers.Failures():
		return report(cleanup(err))
	default:
	}
	readyContext, cancelReady := context.WithTimeout(context.Background(), consoleProtocolTimeout)
	readyErr := writeExecution(console.Writer(readyContext, adapter.ConsoleStdout), inv.format, executionResult{Command: ServeCommand, Status: "ready", Servers: active, Result: output})
	cancelReady()
	if err := readyErr; err != nil {
		return report(cleanup(execution.HostFailure("write readiness", inv.path, err)))
	}
	var cause error
	select {
	case <-ctx.Done():
	case cause = <-servers.Failures():
	}
	if err := cleanup(cause); err != nil {
		return report(err)
	}
	select {
	case err := <-servers.Failures():
		return report(err)
	default:
	}
	stoppedContext, cancelStopped := context.WithTimeout(context.Background(), consoleProtocolTimeout)
	defer cancelStopped()
	if err := writeExecution(console.Writer(stoppedContext, adapter.ConsoleStdout), inv.format, executionResult{Command: ServeCommand, Status: "stopped"}); err != nil {
		return report(execution.HostFailure("write shutdown result", inv.path, err))
	}
	return ExitSuccess
}
func runtimeCleanup(cause, cleanup error) error {
	return execution.PreserveCleanup(cause, cleanup)
}
func writeExecution(w io.Writer, format string, result executionResult) error {
	if format == JsonFormat {
		encoder := json.NewEncoder(w)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(result)
	}
	if result.Command == RunCommand {
		if result.Result == nil {
			return nil
		}
		_, err := fmt.Fprintln(w, value.TextResult(result.Result))
		return err
	}
	if result.Status == "ready" {
		for _, server := range result.Servers {
			if _, err := fmt.Fprintf(w, "listening %s\n", server.Address); err != nil {
				return err
			}
		}
		return nil
	}
	_, err := fmt.Fprintln(w, "stopped")
	return err
}
