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

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type executionResult struct {
	Command string               `json:"command"`
	Status  string               `json:"status"`
	Servers []adapter.ServerInfo `json:"servers,omitempty"`
	Result  value.Value          `json:"result,omitempty"`
}

func executeProgram(inv invocation, p execution.Program, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runProgram(ctx, inv, p, stdout, stderr)
}

// RunContext executes the CLI contract with explicit backend dependencies.
func RunContext(ctx context.Context, options Options, p execution.Program, factory device.Factory, observer device.Observer, stdout, stderr io.Writer) int {
	inv := options.(invocation)
	config, err := device.NewConfig(inv.devices.Grant(), factory, observer)
	if err != nil {
		return ReportRuntime(stderr, inv.format, execution.HostFailure("configure device backend", "command", err))
	}
	inv.devices = config
	return runProgram(ctx, inv, p, stdout, stderr)
}

func runProgram(ctx context.Context, inv invocation, p execution.Program, stdout, stderr io.Writer) int {
	ctx = execution.CaptureProcessContext(ctx)
	limits := adapter.DefaultProcessLimits()
	configuration, err := processhost.NewConfig(inv.processPolicy, limits)
	if err != nil {
		return ReportRuntime(stderr, inv.format, err)
	}
	hostSession, err := processhost.Open(ctx, configuration, adapter.FileRoots{Read: inv.readRoots, Write: inv.writeRoots, List: inv.listRoots, Remove: inv.removeRoots, OpenFiles: inv.openFilesRoots})
	if err != nil {
		return ReportRuntime(stderr, inv.format, err)
	}
	closeHost := func() error {
		cleanup, cancel := context.WithTimeout(context.Background(), limits.CleanupTimeout())
		defer cancel()
		return hostSession.Close(cleanup)
	}
	// Report a bounded Close failure before joining its retained owner. This
	// function cannot return to an os.Exit caller while ownership is unresolved.
	defer func() { _ = hostSession.WaitRetained() }()
	boundaries, err := adapter.WithHTTP(hostSession.Boundary(), inv.httpPolicy)
	if err != nil {
		return ReportRuntime(stderr, inv.format, runtimeCleanup(err, closeHost()))
	}
	if inv.command == RunCommand {
		ctx, capture, err := execution.CaptureFailure(ctx)
		if err != nil {
			return ReportRuntime(stderr, inv.format, runtimeCleanup(err, closeHost()))
		}
		output, runErr := p.Run(ctx, boundaries, inv.budget, inv.devices)
		if err := capture.Preserve(runtimeCleanup(runErr, closeHost())); err != nil {
			return ReportRuntime(stderr, inv.format, err)
		}
		if err := writeExecution(stdout, inv.format, executionResult{Command: RunCommand, Status: "ok", Result: output}); err != nil {
			return ReportRuntime(stderr, inv.format, capture.Preserve(execution.HostFailure("write command result", inv.path, err)))
		}
		return ExitSuccess
	}
	servers, err := adapter.NewServers(p, boundaries, inv.listenPolicy, inv.budget, inv.devices)
	if err != nil {
		return ReportRuntime(stderr, inv.format, runtimeCleanup(err, closeHost()))
	}
	cleanup := func(cause error) error {
		shutdown, cancel := context.WithTimeout(context.Background(), adapter.ShutdownTimeout)
		defer cancel()
		serverErr := servers.Shutdown(shutdown)
		return runtimeCleanup(cause, errors.Join(serverErr, closeHost()))
	}
	output, err := servers.Start(ctx)
	if err != nil {
		return ReportRuntime(stderr, inv.format, cleanup(err))
	}
	active := servers.Snapshot()
	select {
	case err := <-servers.Failures():
		return ReportRuntime(stderr, inv.format, cleanup(err))
	default:
	}
	if err := writeExecution(stdout, inv.format, executionResult{Command: ServeCommand, Status: "ready", Servers: active, Result: output}); err != nil {
		return ReportRuntime(stderr, inv.format, cleanup(execution.HostFailure("write readiness", inv.path, err)))
	}
	var cause error
	select {
	case <-ctx.Done():
	case cause = <-servers.Failures():
	}
	if err := cleanup(cause); err != nil {
		return ReportRuntime(stderr, inv.format, err)
	}
	select {
	case err := <-servers.Failures():
		return ReportRuntime(stderr, inv.format, err)
	default:
	}
	if err := writeExecution(stdout, inv.format, executionResult{Command: ServeCommand, Status: "stopped"}); err != nil {
		return ReportRuntime(stderr, inv.format, execution.HostFailure("write shutdown result", inv.path, err))
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
