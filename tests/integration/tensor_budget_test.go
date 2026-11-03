package integration_test

import (
	"context"

	"sync/atomic"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"

	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

func checkedTensor(t *testing.T, text string) ir.Program {
	t.Helper()
	s, err := source.New("tensor-budget.lo", text)
	if err != nil {
		t.Fatal(err)
	}
	p, d := compiler.Check(s)
	if d != nil {
		t.Fatal(d)
	}
	return p
}

// Broadcast plus elementwise work.

// Two broadcasts add 5 steps per k; the six dot products add 12 per k.

type tensorCancellation struct {
	context.Context
	calls  atomic.Int64
	cancel context.CancelFunc
}

func (c *tensorCancellation) Err() error {
	if c.calls.Add(1) == 256 {
		c.cancel()
	}
	return c.Context.Err()
}
