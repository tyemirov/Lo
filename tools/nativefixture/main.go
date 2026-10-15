//go:build nativequalification

// Command nativefixture builds race-instrumented fixtures for repository tests.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativebuild"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"os"
)

func main() { os.Exit(run()) }
func run() int {
	inv, err := runner.ParseCompiler(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if inv.Command() != runner.BuildCommand {
		fmt.Fprintln(os.Stderr, "qualification harness requires build")
		return 2
	}
	data, err := os.ReadFile(inv.Path())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	file, err := source.New(inv.Path(), string(data))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	p, d := compiler.Check(file)
	if d != nil {
		return runner.Report(os.Stderr, inv.Format(), d, 1)
	}
	receipt, d := nativebuild.BuildRace(context.Background(), p, inv.Output(), inv.Target(), inv.DeviceBackend())
	if d != nil {
		return runner.Report(os.Stderr, inv.Format(), d, 2)
	}
	if err := json.NewEncoder(os.Stdout).Encode(receipt); err != nil {
		return runner.Report(os.Stderr, inv.Format(), diagnostic.New(diagnostic.Invocation, diagnostic.CLI, err.Error(), nil), 2)
	}
	return 0
}
