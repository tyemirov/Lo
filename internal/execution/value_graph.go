package execution

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

const valueGraphSeedCount = 3

type valueGraph struct {
	root       uint64
	identities map[uint64]struct{}
	listWidth  uint64
}
type valueGraphs struct {
	entries map[uint64]*valueGraph
}

func (graphs *valueGraphs) lookup(identity uint64) *valueGraph {
	return graphs.entries[identity]
}
func (graphs *valueGraphs) add(graph *valueGraph) {
	if graphs.entries == nil {
		graphs.entries = make(map[uint64]*valueGraph)
	}
	graphs.entries[graph.root] = graph
}
func (graphs *valueGraphs) remove(identity uint64) { delete(graphs.entries, identity) }
func graphEligible(root value.Value) bool {
	switch root.Type().Kind() {
	case types.IntKind, types.FloatKind, types.StringKind, types.ScalarKind, types.BoolKind, types.NullKind, types.BytesKind, types.TensorKind, types.DeviceKind, types.PathKind, types.URLKind, types.MethodKind:
		return false
	}
	return true
}

// inheritedGraph caches only immutable identity metadata, never values or resources.
func (scope *resourceScope) inheritedGraph(root value.Value) *valueGraph {
	if !graphEligible(root) {
		return nil
	}
	identity := value.Identity(root)
	entry := scope.session.values.lookup(identity)
	if entry == nil || entry.owner == &scope.values {
		return nil
	}
	graphs := &scope.session.graphs
	if graph := graphs.lookup(identity); graph != nil {
		return graph
	}
	graph, descendants := graphs.build(root)
	if graph == nil {
		return nil
	}
	graphs.add(graph)
	for _, descendant := range descendants {
		if descendant == nil {
			continue
		}
		identity := value.Identity(descendant)
		entry := scope.session.values.lookup(identity)
		if entry != nil && entry.owner != &scope.values && graphs.lookup(identity) == nil {
			if child, _ := graphs.build(descendant); child != nil {
				graphs.add(child)
			}
		}
	}
	return graph
}
func (graphs *valueGraphs) build(root value.Value) (*valueGraph, [valueGraphSeedCount]value.Value) {
	var local [8]uint64
	count := 0
	var identities map[uint64]struct{}
	var listWidth uint64
	var descendants [valueGraphSeedCount]value.Value
	reused := false
	rootID := value.Identity(root)
	value.VisitWhile([]value.Value{root}, func(current value.Value) bool {
		if reused {
			return false
		}
		identity := value.Identity(current)
		if identity != rootID && graphs.lookup(identity) != nil {
			reused = true
			return false
		}
		if list, ok := current.(value.List); ok {
			listWidth = max(listWidth, uint64(list.Len()))
		}
		if identity != rootID && graphEligible(current) {
			for index, previous := range descendants {
				if previous == nil || current.Weight() > previous.Weight() {
					copy(descendants[index+1:], descendants[index:])
					descendants[index] = current
					break
				}
			}
		}
		if count < len(local) {
			local[count] = identity
		} else {
			if identities == nil {
				identities = make(map[uint64]struct{}, 16)
				for _, previous := range local {
					identities[previous] = struct{}{}
				}
			}
			identities[identity] = struct{}{}
		}
		count++
		return true
	})
	if reused || identities == nil {
		return nil, [valueGraphSeedCount]value.Value{}
	}
	return &valueGraph{root: rootID, identities: identities, listWidth: listWidth}, descendants
}
func (scope *resourceScope) visitValues(roots []value.Value, retain bool) {
	value.VisitWhile(roots, func(current value.Value) bool {
		if graph := scope.inheritedGraph(current); graph != nil {
			localCount := len(scope.values.candidates)
			if retain {
				localCount += scope.values.pinnedKeys
			}
			if len(graph.identities) < localCount {
				for identity := range graph.identities {
					if retain {
						scope.values.retainKey(identity)
					} else {
						scope.values.keepKey(identity)
					}
				}
				return false
			}
			for index := 0; index < len(scope.values.candidates); {
				identity := scope.values.candidates[index]
				if _, found := graph.identities[identity]; found {
					if retain {
						scope.values.retainKey(identity)
						continue
					}
					scope.values.keepKey(identity)
				}
				index++
			}
			if retain {
				for _, pin := range scope.values.pins {
					for _, identity := range pin.keys {
						if _, found := graph.identities[identity]; found {
							scope.values.retainKey(identity)
						}
					}
				}
			}
			return false
		}
		if retain {
			scope.values.retainKey(value.Identity(current))
		} else {
			scope.values.keepKey(value.Identity(current))
		}
		return true
	})
}
func (session *session) maximumListWidth(root value.Value) uint64 {
	if !graphEligible(root) {
		return 0
	}
	var maximum uint64
	value.VisitWhile([]value.Value{root}, func(current value.Value) bool {
		if graph := session.graphs.lookup(value.Identity(current)); graph != nil {
			maximum = max(maximum, graph.listWidth)
			return false
		}
		if list, ok := current.(value.List); ok {
			maximum = max(maximum, uint64(list.Len()))
		}
		return true
	})
	return maximum
}
