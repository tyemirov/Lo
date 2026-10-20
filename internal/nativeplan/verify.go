package nativeplan

import (
	"context"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"slices"
	"strings"
)

type definition struct {
	block BlockID
	index int
}

func successors(t Terminator) []BlockID {
	switch t.Kind {
	case Jump:
		return t.Targets[:1]
	case Status:
		return t.Targets[:]
	case Boolean, Arity:
		return t.Targets[:2]
	default:
		return nil
	}
}

func verify(ctx context.Context, module Module) error {
	functions := map[string]Function{}
	for _, f := range module.Functions {
		if _, ok := functions[f.Symbol]; ok {
			return fmt.Errorf("duplicate function %s", f.Symbol)
		}
		functions[f.Symbol] = f
	}
	for index, site := range module.Locations {
		if site.ID != uint64(index) || site.File == "" || site.Start.Line < 1 || site.Start.Column < 1 || site.End.Offset < site.Start.Offset {
			return fmt.Errorf("invalid native source site %d", index)
		}
	}
	for _, f := range module.Functions {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := verifyFunction(ctx, f, module.Locations, functions); err != nil {
			return fmt.Errorf("%s: %w", f.Symbol, err)
		}
	}
	return nil
}

func verifyFunction(ctx context.Context, f Function, sites []Location, functions map[string]Function) error {
	if len(f.Blocks) == 0 || f.Symbol == "" || f.Arity < 0 {
		return fmt.Errorf("incomplete function")
	}
	preds := make([][]BlockID, len(f.Blocks))
	values := map[ValueID]definition{}
	results := map[ResultID]definition{}
	callSymbols := map[ResultID]string{}
	resultValues := map[ResultID]ValueID{}
	adoptions := map[ValueID][]definition{}
	helperArity := map[string]int{LiteralSymbol: 0, StepSymbol: 0, ChargeSymbol: 1, ScopeEnterSymbol: 0, FlowEnterSymbol: 0, CauseSymbol: 1, InvalidStatusSymbol: 0, CommitSymbol: 1, "lo_project": 1, "lo_match": 2, "lo_flow_ref": 0, "lo_convert_success": 1, "lo_convert_failure": 1, "lo_convert_propagate": 1, "lo_convert_record_view": 1, "lo_convert_scalar_list_view": 1, "lo_convert_fallible_record_view": 1}
	for index, block := range f.Blocks {
		if block.ID != BlockID(index) || block.End.Kind < Jump || block.End.Kind > Return {
			return fmt.Errorf("invalid block or missing terminator %d", index)
		}
		if block.End.Site >= uint64(len(sites)) {
			return fmt.Errorf("missing terminator source")
		}
		for _, target := range successors(block.End) {
			if int(target) >= len(f.Blocks) {
				return fmt.Errorf("missing successor %d", target)
			}
			if !slices.Contains(preds[target], block.ID) {
				preds[target] = append(preds[target], block.ID)
			}
		}
		for position, instruction := range block.Instructions {
			if instruction.Site >= uint64(len(sites)) {
				return fmt.Errorf("missing instruction source")
			}
			if instruction.Kind < Call || instruction.Kind > ReleaseAll {
				return fmt.Errorf("invalid instruction")
			}
			if instruction.Value != 0 && instruction.Kind != AdoptResult {
				if int(instruction.Value) > len(f.Values) {
					return fmt.Errorf("invalid value definition")
				}
				if _, ok := values[instruction.Value]; ok {
					return fmt.Errorf("repeated value definition")
				}
				values[instruction.Value] = definition{block.ID, position}
				if f.Values[instruction.Value-1] != (instruction.Kind != Parameter) {
					return fmt.Errorf("inconsistent value ownership")
				}
			}
			if instruction.Kind == Call {
				if instruction.Result == 0 || uint32(instruction.Result) > f.Results || instruction.Value == 0 {
					return fmt.Errorf("invalid call result")
				}
				if _, ok := results[instruction.Result]; ok {
					return fmt.Errorf("repeated result definition")
				}
				results[instruction.Result] = definition{block.ID, position}
				callSymbols[instruction.Result] = instruction.Symbol
				resultValues[instruction.Result] = instruction.Value
				if instruction.Symbol != InvalidStatusSymbol && (block.End.Kind != Status || block.End.Result != instruction.Result) {
					return fmt.Errorf("call result must be status-tested in its block")
				}
				if callee, ok := functions[instruction.Symbol]; ok && len(instruction.Inputs) != callee.Arity {
					return fmt.Errorf("call to %s has incorrect arity", instruction.Symbol)
				}
				if _, ok := functions[instruction.Symbol]; !ok {
					if arity, ok := helperArity[instruction.Symbol]; ok {
						if len(instruction.Inputs) != arity {
							return fmt.Errorf("helper %s has incorrect arity", instruction.Symbol)
						}
					} else if instruction.Symbol == FlowErrorSymbol {
						if instruction.Index != uint64(f.Flow) || len(instruction.Inputs) != f.Arity+1 || !instruction.CallerLocation {
							return fmt.Errorf("invalid flow framing call")
						}
					} else {
						op, ok := catalog.Lookup(sites[instruction.Site].Operation)
						if !ok || OperationSymbol(op.Name()) != instruction.Symbol {
							return fmt.Errorf("unknown runtime helper %s", instruction.Symbol)
						}
					}
				}
			}
			if instruction.Kind == AdoptResult {
				adoptions[instruction.Value] = append(adoptions[instruction.Value], definition{block.ID, position})
			}
			if instruction.Kind == Parameter && instruction.Index >= uint64(f.Arity) {
				return fmt.Errorf("invalid parameter position")
			}
			if instruction.Kind == Phi && len(instruction.Inputs) != len(instruction.Predecessors) {
				return fmt.Errorf("incomplete phi")
			}
		}
	}
	if len(values) != len(f.Values) || len(results) != int(f.Results) {
		return fmt.Errorf("unbound native values or results")
	}
	// Compute immediate dominators with linear storage; checked programs can be large.
	visited := make([]bool, len(f.Blocks))
	post := []BlockID{}
	var walk func(BlockID)
	walk = func(id BlockID) {
		if visited[id] {
			return
		}
		visited[id] = true
		for _, next := range successors(f.Blocks[id].End) {
			walk(next)
		}
		post = append(post, id)
	}
	walk(0)
	slices.Reverse(post)
	rank := make([]int, len(f.Blocks))
	for i, id := range post {
		rank[id] = i
	}
	idom := make([]int, len(f.Blocks))
	for i := range idom {
		idom[i] = -1
	}
	idom[0] = 0
	intersect := func(left, right int) int {
		for left != right {
			for rank[left] > rank[right] {
				left = idom[left]
			}
			for rank[right] > rank[left] {
				right = idom[right]
			}
		}
		return left
	}
	changed := true
	for changed {
		changed = false
		for _, id := range post[1:] {
			if err := ctx.Err(); err != nil {
				return err
			}
			parent := -1
			for _, pred := range preds[id] {
				if idom[pred] < 0 {
					continue
				}
				if parent < 0 {
					parent = int(pred)
				} else {
					parent = intersect(parent, int(pred))
				}
			}
			if parent != idom[id] {
				idom[id] = parent
				changed = true
			}
		}
	}
	dominates := func(def, use BlockID) bool {
		cursor := int(use)
		for cursor >= 0 {
			if cursor == int(def) {
				return true
			}
			if cursor == idom[cursor] {
				break
			}
			cursor = idom[cursor]
		}
		return false
	}
	checkValue := func(value ValueID, block BlockID, index int) error {
		def, ok := values[value]
		if !ok {
			return fmt.Errorf("unbound operand %d", value)
		}
		if def.block == block && def.index >= index || !dominates(def.block, block) {
			return fmt.Errorf("operand %d is not available in block %d", value, block)
		}
		if f.Blocks[def.block].Instructions[def.index].Kind == Call {
			available := false
			for _, adopt := range adoptions[value] {
				if dominates(adopt.block, block) && (adopt.block != block || adopt.index < index) {
					available = true
				}
			}
			if !available {
				return fmt.Errorf("operand %d used before status-gated adoption", value)
			}
		}
		return nil
	}
	checkResult := func(result ResultID, block BlockID, index int) error {
		if result == 0 {
			return nil
		}
		def, ok := results[result]
		if !ok || def.block == block && def.index >= index || !dominates(def.block, block) {
			return fmt.Errorf("result %d is not available", result)
		}
		return nil
	}
	for _, block := range f.Blocks {
		if !visited[block.ID] {
			continue
		}
		for index, instruction := range block.Instructions {
			if instruction.Kind == AdoptResult {
				if resultValues[instruction.Result] != instruction.Value || len(preds[block.ID]) == 0 {
					return fmt.Errorf("invalid result adoption")
				}
				for _, pred := range preds[block.ID] {
					end := f.Blocks[pred].End
					if end.Kind != Status || end.Result != instruction.Result {
						return fmt.Errorf("adoption without status branch")
					}
					for status, target := range end.Targets {
						if target == block.ID && status >= 2 {
							return fmt.Errorf("host or unknown payload adopted")
						}
					}
				}
				if err := checkResult(instruction.Result, block.ID, index); err != nil {
					return err
				}
			}
			if instruction.Kind == Phi {
				if len(instruction.Predecessors) != len(preds[block.ID]) {
					return fmt.Errorf("phi does not cover predecessors")
				}
				seen := map[BlockID]bool{}
				for i, pred := range instruction.Predecessors {
					if seen[pred] || !slices.Contains(preds[block.ID], pred) {
						return fmt.Errorf("invalid phi predecessor")
					}
					seen[pred] = true
					if err := checkValue(instruction.Inputs[i], pred, len(f.Blocks[pred].Instructions)); err != nil {
						return err
					}
				}
			} else {
				for _, value := range instruction.Inputs {
					if err := checkValue(value, block.ID, index); err != nil {
						return err
					}
				}
			}
			if instruction.Kind == CopyResult {
				if err := checkResult(instruction.Result, block.ID, index); err != nil {
					return err
				}
				if instruction.Payload {
					if err := checkValue(resultValues[instruction.Result], block.ID, index); err != nil {
						return err
					}
				}
			}
		}
		if block.End.Kind == Status {
			if err := checkResult(block.End.Result, block.ID, len(block.Instructions)); err != nil {
				return err
			}
		}
		if block.End.Kind == Boolean {
			if err := checkValue(block.End.Value, block.ID, len(block.Instructions)); err != nil {
				return err
			}
		}
	}
	// Verify scope and flow lifetimes on every reachable status path.
	type state struct {
		block                         BlockID
		flow, scope, released, closed bool
	}
	queue := []state{{block: 0, released: true}}
	seen := map[state]bool{}
	blockPhases := map[BlockID]uint8{}
	valuePhases := map[ValueID]uint8{}
	for len(queue) > 0 {
		current := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[current] {
			continue
		}
		seen[current] = true
		if err := ctx.Err(); err != nil {
			return err
		}
		block := f.Blocks[current.block]
		phase := uint8(0)
		if current.scope {
			phase = 1
		} else if current.closed {
			phase = 2
		}
		blockPhases[block.ID] |= 1 << phase
		for index, instruction := range block.Instructions {
			if instruction.Value != 0 && instruction.Kind != Call && f.Values[instruction.Value-1] {
				current.released = false
				valuePhases[instruction.Value] |= 1 << phase
			}
			switch instruction.Kind {
			case Call:
				if instruction.Symbol == FlowEnterSymbol && (current.flow || current.scope) {
					return fmt.Errorf("flow entered twice")
				}
				if instruction.Symbol == ScopeEnterSymbol && (current.scope || current.closed || f.Flow >= 0 && !current.flow) {
					return fmt.Errorf("invalid body scope entry")
				}
				if strings.HasPrefix(instruction.Symbol, "lo_op_") || instruction.Symbol == LiteralSymbol || instruction.Symbol == StepSymbol || instruction.Symbol == ChargeSymbol || instruction.Symbol == CommitSymbol {
					if !current.scope {
						return fmt.Errorf("operation outside body scope")
					}
				}
			case ScopeLeave:
				if !current.scope {
					return fmt.Errorf("body scope left without entry")
				}
				current.scope = false
				current.closed = true
				phase = 2
			case FlowLeave:
				if !current.flow || current.scope {
					return fmt.Errorf("flow left before body cleanup")
				}
				current.flow = false
			case ReleaseAll:
				if index != len(block.Instructions)-1 {
					return fmt.Errorf("operand use after owned cleanup")
				}
				if block.End.Kind != Return {
					if index == 0 || block.Instructions[index-1].Kind != ScopeLeave {
						return fmt.Errorf("owned cleanup outside body or final exit")
					}
					if block.End.Kind != Jump && (block.End.Kind != Status || block.End.Result != 0) {
						return fmt.Errorf("body cleanup terminator uses released value")
					}
				}
				current.released = true
			}
		}
		if block.End.Kind == Return {
			if current.flow || current.scope || !current.released {
				return fmt.Errorf("exit without complete cleanup")
			}
			continue
		}
		for position, next := range successors(block.End) {
			state := current
			state.block = next
			if block.End.Kind == Status && position == 0 && results[block.End.Result].block == block.ID {
				switch callSymbols[block.End.Result] {
				case FlowEnterSymbol:
					state.flow = true
				case ScopeEnterSymbol:
					state.scope = true
				}
			}
			queue = append(queue, state)
		}
	}
	for _, block := range f.Blocks {
		for _, instruction := range block.Instructions {
			inputs := instruction.Inputs
			if instruction.Kind == CopyResult && instruction.Payload {
				inputs = append(slices.Clone(inputs), resultValues[instruction.Result])
			}
			for _, value := range inputs {
				if f.Values[value-1] && blockPhases[block.ID]&(1<<2) != 0 && valuePhases[value] != 1<<2 {
					return fmt.Errorf("owned operand %d used after body cleanup", value)
				}
			}
		}
	}
	return nil
}
