package runtimeops

import (
	"errors"
	"fmt"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// FileObservation obtains directory metadata or a scan from the authorized adapter.
func FileObservation(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	observer, ok := session.Boundary().(execution.FileObserver)
	if !ok {
		return nil, execution.HostFailure(call.Operation().Name(), args[0].(value.Text).Text(), fmt.Errorf("File boundary has no observation adapter"))
	}
	var result value.Value
	var err error
	kind := catalog.DirectoryFailed
	if call.Operation().Name() == catalog.FileDirectory {
		result, err = observer.Directory(session.Context(), args[0].(value.Text))
	} else {
		kind = catalog.ListFailed
		result, err = observer.Scan(session.Context(), args[0].(value.Text))
	}
	if err != nil {
		if host := execution.BoundaryHostError(err); host != nil {
			return nil, host
		}
		var file execution.FileError
		if errors.As(err, &file) {
			kind = file.Kind()
		}
		return OperationFailure(call, args, kind, err.Error()), nil
	}
	return Success(call.Type(), result), nil
}

// FileRemoveChecked captures the resolved predicate and contexts without invoking it.
func FileRemoveChecked(session execution.Session, call Call, args []value.Value, flows map[int]identity.FlowID) (value.Value, error) {
	entries := args[0].(value.List)
	if entries.Len() > execution.MaxFileEntries {
		return nil, execution.Limit("file removal entries")
	}
	flow := flows[len(args)-1]
	contexts := args[1 : len(args)-1]
	weight := entries.Weight() + 32 + uint64(len(catalog.FileRemove)) + uint64(len(flow)) + 32
	for _, context := range contexts {
		weight += context.Weight()
	}
	if err := session.ReserveFile(uint64(entries.Len()), weight); err != nil {
		return nil, err
	}
	target, err := value.NewCheckedRemovalEffect(session.FileSession(), entries, flow, contexts)
	if err != nil {
		return OperationFailure(call, args, catalog.InvalidSelection, err.Error()), nil
	}
	return Success(call.Type(), target), nil
}
