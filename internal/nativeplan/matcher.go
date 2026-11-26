package nativeplan

import (
	"fmt"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func validateMatcher(operation string, args []ir.Argument, output types.Type, flows map[identity.FlowID]ir.Flow) error {
	var values [2]types.Type
	var callbacks [2]ir.Flow
	for _, argument := range args {
		switch argument := argument.(type) {
		case ir.ValueArgument:
			values[argument.Position()] = argument.Value().Type()
		case ir.FlowArgument:
			callbacks[argument.Position()] = flows[argument.Flow()]
		}
	}
	switch operation {
	case catalog.MatchCaseNew:
		signature, ok := output.(types.Matcher)
		if ok && signature.Kind() == types.MatchCaseKind &&
			matcherCallback(callbacks[0], signature.Input(), types.Bool()) &&
			!callbacks[0].Effects().Reads &&
			matcherCallback(callbacks[1], signature.Input(), signature.Output()) &&
			signature.Reads() == callbacks[1].Effects().Reads {
			return nil
		}
	case catalog.MatchNew:
		signature, ok := output.(types.Matcher)
		cases, isList := values[0].(types.List)
		if ok && signature.Kind() == types.MatchKind && isList {
			caseType, isCase := cases.Element().(types.Matcher)
			if isCase && caseType.Kind() == types.MatchCaseKind &&
				types.Equal(caseType.Input(), signature.Input()) && types.Equal(caseType.Output(), signature.Output()) &&
				matcherCallback(callbacks[1], signature.Input(), signature.Output()) &&
				signature.Reads() == (caseType.Reads() || callbacks[1].Effects().Reads) {
				return nil
			}
		}
	case catalog.MatchApply:
		signature, ok := values[1].(types.Matcher)
		if ok && signature.Kind() == types.MatchKind && values[0] != nil &&
			types.Equal(values[0], signature.Input()) && types.Equal(output, signature.Output()) {
			return nil
		}
	}
	return fmt.Errorf("native lowering: inconsistent signature for %s", operation)
}

func matcherCallback(flow ir.Flow, input, output types.Type) bool {
	if flow == nil || flow.Effects().Commits {
		return false
	}
	parameters := flow.Parameters()
	return len(parameters) == 1 && types.Equal(parameters[0].Type, input) && types.Equal(flow.Signature().Output(), output)
}
