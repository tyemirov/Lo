package inspect

import (
	"fmt"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
)

// processData describes source inputs without a configured host profile.
type processData struct {
	Executable  description          `json:"executable"`
	Arguments   description          `json:"arguments"`
	Environment description          `json:"environment"`
	Directory   description          `json:"directory"`
	Stdin       description          `json:"stdin"`
	Profile     executionProfileData `json:"execution_profile"`
}

type executionProfileData struct {
	Required bool   `json:"required"`
	Status   string `json:"status"`
}

func (a *analyzer) processInputs(values []abstract) *processData {
	return &processData{
		Executable: a.describe(values[0]), Arguments: a.describe(values[1]),
		Environment: a.describe(values[2]), Directory: a.describe(values[3]), Stdin: a.describe(values[4]),
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
	}{{"executable", process.Executable}, {"arguments", process.Arguments}, {"environment", process.Environment}, {"directory", process.Directory}, {"stdin", process.Stdin}} {
		text := input.value.Expression
		if input.value.Value != nil {
			text = *input.value.Value
		}
		fmt.Fprintf(output, " %s=%q node=%s dependencies=%v", input.name, text, input.value.Node, input.value.Dependencies)
	}
}
