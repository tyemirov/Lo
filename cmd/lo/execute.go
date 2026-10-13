package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type executionResult struct {
	Command string               `json:"command"`
	Status  string               `json:"status"`
	Servers []adapter.ServerInfo `json:"servers,omitempty"`
	Result  value.Value          `json:"result,omitempty"`
}

func executeProgram(inv invocation, p ir.Program, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	files, err := adapter.OpenFiles(inv.readRoots, inv.writeRoots)
	if err != nil {
		return reportRuntime(stderr, inv.format, err)
	}
	if inv.command == runCommand {
		output, runErr := eval.Run(ctx, p, files, inv.budget)
		if err := runtimeCleanup(runErr, files.Close()); err != nil {
			return reportRuntime(stderr, inv.format, err)
		}
		if err := writeExecution(stdout, inv.format, executionResult{Command: runCommand, Status: "ok", Result: output}); err != nil {
			return reportRuntime(stderr, inv.format, eval.HostFailure("write command result", inv.path, err))
		}
		return exitSuccess
	}
	servers, err := adapter.NewServers(p, files, inv.listenPolicy, inv.budget)
	if err != nil {
		return reportRuntime(stderr, inv.format, runtimeCleanup(err, files.Close()))
	}
	cleanup := func(cause error) error {
		shutdown, cancel := context.WithTimeout(context.Background(), adapter.ShutdownTimeout)
		defer cancel()
		serverErr := servers.Shutdown(shutdown)
		return runtimeCleanup(cause, errors.Join(serverErr, files.Close()))
	}
	output, err := servers.Start(ctx)
	if err != nil {
		return reportRuntime(stderr, inv.format, cleanup(err))
	}
	active := servers.Snapshot()
	select {
	case err := <-servers.Failures():
		return reportRuntime(stderr, inv.format, cleanup(err))
	default:
	}
	if err := writeExecution(stdout, inv.format, executionResult{Command: serveCommand, Status: "ready", Servers: active, Result: output}); err != nil {
		return reportRuntime(stderr, inv.format, cleanup(eval.HostFailure("write readiness", inv.path, err)))
	}
	var cause error
	select {
	case <-ctx.Done():
	case cause = <-servers.Failures():
	}
	if err := cleanup(cause); err != nil {
		return reportRuntime(stderr, inv.format, err)
	}
	select {
	case err := <-servers.Failures():
		return reportRuntime(stderr, inv.format, err)
	default:
	}
	if err := writeExecution(stdout, inv.format, executionResult{Command: serveCommand, Status: "stopped"}); err != nil {
		return reportRuntime(stderr, inv.format, eval.HostFailure("write shutdown result", inv.path, err))
	}
	return exitSuccess
}
func runtimeCleanup(cause, cleanup error) error {
	if cleanup != nil {
		return eval.HostFailure("close runtime resources", "program", errors.Join(cause, cleanup))
	}
	return cause
}
func writeExecution(w io.Writer, format string, result executionResult) error {
	if format == jsonFormat {
		encoder := json.NewEncoder(w)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(result)
	}
	if result.Command == runCommand {
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
