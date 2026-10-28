package inspect

import (
	"fmt"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
)

// processData describes source inputs without a configured host profile.
type processData struct {
	Context    processContextDescription `json:"context"`
	Executable description               `json:"executable"`
	Arguments  description               `json:"arguments"`
	Stdin      description               `json:"stdin"`
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

func (a *analyzer) processInputs(values []abstract) *processData {
	return &processData{
		Context: a.describeProcessContext(values[0]), Executable: a.describe(values[1]), Arguments: a.describe(values[2]), Stdin: a.describe(values[3]),
		Profile: executionProfileData{Required: true, Status: "unavailable"},
	}
}

func (a *analyzer) dynamicProcessInputs(command abstract, operation catalog.Operation) *processData {
	parameters := operation.Parameters()
	values := make([]abstract, len(parameters))
	for index, parameter := range parameters {
		values[index] = a.unknown(command.node, parameter.Type)
		values[index].dependencies = command.dependencies
	}
	return a.processInputs(values)
}

func (process *processData) writeText(output *strings.Builder) {
	fmt.Fprintf(output, " execution_profile=required/%s", process.Profile.Status)
	for _, input := range []struct {
		name  string
		value description
	}{{"context", process.Context.Source}, {"executable", process.Executable}, {"arguments", process.Arguments}, {"stdin", process.Stdin}} {
		text := input.value.Expression
		if input.value.Value != nil {
			text = *input.value.Value
		}
		fmt.Fprintf(output, " %s=%q node=%s dependencies=%v", input.name, text, input.value.Node, input.value.Dependencies)
	}
	fmt.Fprintf(output, " context_origin=%s", process.Context.Origin)
}
