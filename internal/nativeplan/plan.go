// Package nativeplan lowers checked Lo graphs into verified native control flow.
package nativeplan

import (
	"errors"
	"fmt"
	"slices"

	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

const ABIIdentity = "lo-native-2"
const MaxModuleBytes = 64 << 20

var ErrUnsupported = errors.New("unsupported native lowering")
var ErrLimit = errors.New("native module size limit")

type ValueID uint32
type ResultID uint32
type BlockID uint32

// Plan can only be produced by the lowering verifier.
type Plan interface {
	Snapshot() Module
	verified()
}

type verifiedPlan struct{ module Module }

func (*verifiedPlan) verified() {}
func (p *verifiedPlan) Snapshot() Module {
	m := p.module
	m.Metadata = slices.Clone(m.Metadata)
	m.Locations = slices.Clone(m.Locations)
	for i := range m.Locations {
		m.Locations[i].Labels = slices.Clone(m.Locations[i].Labels)
		m.Locations[i].Arguments = slices.Clone(m.Locations[i].Arguments)
	}
	m.Functions = slices.Clone(m.Functions)
	for i := range m.Functions {
		f := &m.Functions[i]
		f.Values = slices.Clone(f.Values)
		f.Blocks = slices.Clone(f.Blocks)
		for j := range f.Blocks {
			b := &f.Blocks[j]
			b.Instructions = slices.Clone(b.Instructions)
			for k := range b.Instructions {
				b.Instructions[k].Inputs = slices.Clone(b.Instructions[k].Inputs)
				b.Instructions[k].Predecessors = slices.Clone(b.Instructions[k].Predecessors)
				b.Instructions[k].Literals = slices.Clone(b.Instructions[k].Literals)
			}
		}
	}
	return m
}

type Module struct {
	SourcePath string
	Metadata   []byte
	Locations  []Location
	Functions  []Function
}

// Read checks the opaque proof token without repeating graph verification.
func Read(plan Plan) (Module, error) {
	verified, ok := plan.(*verifiedPlan)
	if !ok || verified == nil {
		return Module{}, fmt.Errorf("native emitter requires a verified plan")
	}
	return verified.Snapshot(), nil
}

type Location struct {
	ID              uint64             `json:"id"`
	File            string             `json:"file"`
	Start           source.Position    `json:"start"`
	End             source.Position    `json:"end"`
	Action          string             `json:"action"`
	Type            uint64             `json:"type"`
	Text            string             `json:"text"`
	FloatBits       string             `json:"float_bits"`
	Field           string             `json:"field"`
	Flow            uint64             `json:"flow"`
	Operation       string             `json:"operation"`
	Labels          []string           `json:"labels"`
	Owner           string             `json:"owner"`
	Arguments       []ArgumentMetadata `json:"arguments"`
	DerivativePlan  uint64             `json:"derivative_plan"`
	DerivativeNode  uint64             `json:"derivative_node"`
	DerivativeFrame uint64             `json:"derivative_frame"`
}

// ArgumentMetadata identifies a checked value or callback in parameter order.
type ArgumentMetadata struct {
	Kind string `json:"kind"`
	Type uint64 `json:"type"`
	Flow uint64 `json:"flow"`
}

type Function struct {
	Symbol  string
	Name    string
	Flow    int
	Arity   int
	Site    uint64
	Values  []bool // true means one locally owned handle, false means a borrowed parameter.
	Results uint32
	Blocks  []Block
}

type Block struct {
	ID           BlockID
	Instructions []Instruction
	End          Terminator
}

type Opcode uint8

const (
	Call Opcode = iota + 1
	AdoptResult
	Parameter
	Alias
	Phi
	CopyResult
	OutputValue
	ScopeLeave
	FlowLeave
	ReleaseAll
	Release
	TapeLeave
	LiteralSequence
)

// LiteralTriple preserves the three original runtime sites for one Int argument.
type LiteralTriple struct {
	Step, Literal, Charge uint64
}

type Instruction struct {
	Kind           Opcode
	Value          ValueID
	Result         ResultID
	Inputs         []ValueID
	Predecessors   []BlockID
	Symbol         string
	Site           uint64
	CallerLocation bool
	Payload        bool
	Index          uint64
	OwnedBase      ValueID
	Literals       []LiteralTriple
}

type TerminatorKind uint8

const (
	Jump TerminatorKind = iota + 1
	Status
	Boolean
	Arity
	Return
)

type Terminator struct {
	Kind    TerminatorKind
	Result  ResultID // zero denotes the externally supplied output result.
	Value   ValueID
	Targets [4]BlockID // status 0,1,2,unknown; or true,false.
	Site    uint64
}

const (
	LiteralSymbol       = "lo_literal"
	StepSymbol          = "lo_step"
	ChargeSymbol        = "lo_charge_value"
	ScopeEnterSymbol    = "lo_scope_enter"
	FlowEnterSymbol     = "lo_flow_enter"
	FlowErrorSymbol     = "lo_flow_error"
	CauseSymbol         = "lo_cause"
	InvalidStatusSymbol = "lo_invalid_status"
	CommitSymbol        = "lo_commit"
)

// OperationSymbol maps catalog identities injectively to C identifiers.
func OperationSymbol(name string) string {
	const hex = "0123456789ABCDEF"
	out := []byte("lo_op_")
	for _, ch := range []byte(name) {
		if ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' {
			out = append(out, ch)
		} else {
			out = append(out, '_', hex[ch>>4], hex[ch&15])
		}
	}
	return string(out)
}
