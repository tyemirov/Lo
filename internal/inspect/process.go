package inspect

import (
	"fmt"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

// processData describes source inputs without a configured host profile.
type processData struct {
	Context    processContextDescription `json:"context"`
	Executable description               `json:"executable"`
	Arguments  description               `json:"arguments"`
	Stdin      description               `json:"stdin"`
	TimeoutMS  description               `json:"timeout_ms"`
	Output     description               `json:"output"`
	Profile    executionProfileData      `json:"execution_profile"`
}

type processContextDescription struct {
	Source      description                `json:"source"`
	Origin      string                     `json:"origin"`
	Environment *description               `json:"environment,omitempty"`
	Directory   *description               `json:"directory,omitempty"`
	Base        *processContextDescription `json:"base,omitempty"`
	Updates     *description               `json:"updates,omitempty"`
	Removed     *description               `json:"removed,omitempty"`
}

func (a *analyzer) describeProcessContext(value abstract) processContextDescription {
	if value.processContext != nil {
		result := *value.processContext
		result.Source = a.describe(value)
		return result
	}
	return processContextDescription{Source: a.describe(value), Origin: "dynamic"}
}

type executionProfileData struct {
	Required bool   `json:"required"`
	Status   string `json:"status"`
}

func (a *analyzer) processInputs(values []abstract, node nodeID) *processData {
	timeout := a.unknown(node, types.Int())
	zero := "0"
	timeout.known = &zero
	output := a.unknown(node, types.String())
	capture := "capture"
	output.known = &capture
	return &processData{
		Context: a.describeProcessContext(values[0]), Executable: a.describe(values[1]), Arguments: a.describe(values[2]), Stdin: a.describe(values[3]),
		TimeoutMS: a.describe(timeout), Output: a.describe(output), Profile: executionProfileData{Required: true, Status: "unavailable"},
	}
}

func (a *analyzer) dynamicProcessInputs(command abstract, operation catalog.Operation) *processData {
	parameters := operation.Parameters()
	values := make([]abstract, len(parameters))
	for index, parameter := range parameters {
		values[index] = a.unknown(command.node, parameter.Type)
		values[index].dependencies = command.dependencies
	}
	process := a.processInputs(values, command.node)
	timeout, output := a.unknown(command.node, types.Int()), a.unknown(command.node, types.String())
	timeout.dependencies, output.dependencies = command.dependencies, command.dependencies
	process.TimeoutMS, process.Output = a.describe(timeout), a.describe(output)
	return process
}

func (a *analyzer) transformCommand(c *context, node nodeID, operation string, command, option abstract) []targetReference {
	targets := command.targets
	if len(targets) == 0 {
		targets = a.dynamicTargets(c, command)
	}
	transformed := make([]targetReference, 0, len(targets))
	for _, target := range targets {
		resource := a.targets[target.ID]
		process := *resource.Process
		switch operation {
		case catalog.ProcessCommandWithTimeout:
			process.TimeoutMS = a.describe(option)
		case catalog.ProcessCommandWithOutput:
			process.Output = a.describe(option)
		}
		resource.Process = &process
		resource.ID = nextID("t", len(a.data.Targets))
		resource.Origin = "transformation"
		resource.Node, resource.Context, resource.Entry = node, c.id, c.entry
		resource.Conditions = mergeConditions(target.Conditions, c.conditions)
		a.data.Targets = append(a.data.Targets, resource)
		a.targets[targetID(resource.ID)] = resource
		transformed = append(transformed, targetReference{ID: targetID(resource.ID), Conditions: resource.Conditions})
	}
	return transformed
}

func (process *processData) writeText(output *strings.Builder) {
	fmt.Fprintf(output, " execution_profile=required/%s", process.Profile.Status)
	for _, input := range []struct {
		name  string
		value description
	}{{"context", process.Context.Source}, {"executable", process.Executable}, {"arguments", process.Arguments}, {"stdin", process.Stdin}, {"timeout_ms", process.TimeoutMS}, {"output", process.Output}} {
		text := input.value.Expression
		if input.value.Value != nil {
			text = *input.value.Value
		}
		fmt.Fprintf(output, " %s=%q node=%s dependencies=%v", input.name, text, input.value.Node, input.value.Dependencies)
	}
	fmt.Fprintf(output, " context_origin=%s", process.Context.Origin)
}
