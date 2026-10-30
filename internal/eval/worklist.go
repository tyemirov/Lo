package eval

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/runtimeops"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// Each discovered list contributes one chunk. Advancing a chunk never copies
// the remaining queue, and pushing a chunk preserves the callback's item order.
type worklistChunk struct {
	items    []value.Value
	next     int
	releases []func()
}

func (m *machine) worklistProcessor(call ir.BuiltinCall, args []value.Value, callback identity.FlowID, f frame) (result value.Value, resultErr error) {
	scope := m.Scope()
	defer func() { scope.Close(result) }()
	state := args[1]
	context := args[2 : len(args)-1]
	pending, completed := args[0].(value.List).Len(), 0
	progress, err := m.RecordWorklistProgress(completed, pending, state)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = execution.WithFlowProgress(resultErr, progress) }()
	for _, v := range context {
		release, err := scope.Pin(v)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	params := make([]value.Value, len(context)+2)
	copy(params, context)
	var chunks []worklistChunk
	queued := 0
	push := func(list value.List) error {
		if err := m.ReserveWorklist(list.Len()); err != nil {
			return err
		}
		items := list.Items()
		if len(items) != 0 {
			releases := make([]func(), len(items))
			for index, item := range items {
				release, err := scope.Pin(item)
				if err != nil {
					for _, release := range releases[:index] {
						release()
					}
					m.ReleaseWorklist(len(items))
					return err
				}
				releases[index] = release
			}
			chunks = append(chunks, worklistChunk{items: items, releases: releases})
			queued += len(items)
		}
		return nil
	}
	clear := func() {
		for _, chunk := range chunks {
			for index := chunk.next; index < len(chunk.items); index++ {
				chunk.releases[index]()
			}
		}
		m.ReleaseWorklist(queued)
		chunks, queued = nil, 0
	}
	defer clear()
	if err := push(args[0].(value.List)); err != nil {
		return nil, err
	}
	for len(chunks) != 0 {
		if err := m.Step(); err != nil {
			return nil, err
		}
		last := len(chunks) - 1
		chunk := &chunks[last]
		item := chunk.items[chunk.next]
		params[len(context)], params[len(context)+1] = state, item
		v, err := m.invoke(callback, params, call.Span())
		if err != nil {
			return nil, err
		}
		if fallible, ok := v.(value.Fallible); ok {
			var cause value.Error
			v, cause = fallible.Outcome()
			if cause != nil {
				cause = value.WithFlowProgress(cause, progress.Record())
				fields := []value.Field{
					{Name: "index", Value: value.Int64(int64(completed))},
					{Name: "element", Value: item},
					{Name: "state", Value: state},
				}
				cause = value.WithFrame(cause, f.name, catalog.FlowWorklist, must(value.NewRecord(fields)))
				return must(value.Failure(call.Type().(types.Fallible), value.At(cause, catalog.FlowWorklist, call.Span()))), nil
			}
		}
		record := v.(value.Record)
		state = record.Field("state")
		chunk.releases[chunk.next]()
		chunk.items[chunk.next] = nil
		chunk.releases[chunk.next] = nil
		chunk.next++
		pending--
		queued--
		m.ReleaseWorklist(1)
		completed++
		if chunk.next == len(chunk.items) {
			chunks[last] = worklistChunk{}
			chunks = chunks[:last]
		}
		stop := record.Field("stop").(value.Boolean).Bool()
		children := record.Field("items").(value.List)
		if stop {
			pending = 0
			clear()
			scope.Keep(state)
		} else {
			pending += children.Len()
			scope.Keep(state, children)
		}
		next, err := m.RecordWorklistProgress(completed, pending, state)
		if err != nil {
			return nil, err
		}
		progress = next
		if stop {
			break
		}
		if err := push(children); err != nil {
			return nil, err
		}
	}
	return runtimeops.Success(call.Type(), state), nil
}
