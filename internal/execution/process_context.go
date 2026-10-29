package execution

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type processContextKey struct{}
type initialProcessContext struct {
	value value.ProcessContext
	err   error
	home  string
}

// WithProcessContext supplies immutable invocation inputs to an embedded runner.
func WithProcessContext(ctx context.Context, initial value.ProcessContext) (context.Context, error) {
	if ctx == nil || initial == nil {
		return nil, fmt.Errorf("initial Process context requires a context and validated value")
	}
	home := ""
	if input, found := initial.EnvironmentValue("HOME"); found {
		home = input.Text()
	}
	return context.WithValue(ctx, processContextKey{}, initialProcessContext{value: initial, home: home}), nil
}

// CaptureProcessContext captures the environment and directory once before evaluation.
// An injected initial value is retained, including across server invocations.
func CaptureProcessContext(ctx context.Context) context.Context {
	if _, ok := ctx.Value(processContextKey{}).(initialProcessContext); ok {
		return ctx
	}
	environment := os.Environ()
	home := ""
	for _, entry := range environment {
		if name, text, ok := strings.Cut(entry, "="); ok && name == "HOME" {
			home = text
		}
	}
	initial, err := captureProcessContext(environment)
	return context.WithValue(ctx, processContextKey{}, initialProcessContext{value: initial, err: err, home: home})
}

// InheritProcessContext attaches the parent's captured inputs to a new lifetime.
func InheritProcessContext(ctx, parent context.Context) (context.Context, error) {
	if ctx == nil || parent == nil {
		return nil, fmt.Errorf("Process context inheritance requires two contexts")
	}
	parent = CaptureProcessContext(parent)
	return context.WithValue(ctx, processContextKey{}, parent.Value(processContextKey{})), nil
}

// InitialProcessContext returns captured inputs without another host observation.
func InitialProcessContext(ctx context.Context) (value.ProcessContext, error) {
	initial, ok := ctx.Value(processContextKey{}).(initialProcessContext)
	if !ok {
		return nil, fmt.Errorf("initial Process context was not captured")
	}
	return initial.value, initial.err
}

func captureProcessContext(environment []string) (value.ProcessContext, error) {
	directory, err := unix.Getwd()
	if err != nil {
		return nil, fmt.Errorf("capture Process directory: %w", err)
	}
	sort.Strings(environment)
	entries := make([]value.Entry, 0, len(environment))
	for _, entry := range environment {
		name, text, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("capture Process environment: invalid entry")
		}
		key, err := value.NewText(types.String(), name)
		if err != nil {
			return nil, fmt.Errorf("capture Process environment name: invalid text")
		}
		val, err := value.NewText(types.String(), text)
		if err != nil {
			return nil, fmt.Errorf("capture Process environment value: invalid text")
		}
		entries = append(entries, value.Entry{Key: key, Value: val})
	}
	typ, err := types.NewMap(types.String(), types.String())
	if err != nil {
		return nil, err
	}
	variables, err := value.NewMap(typ, entries)
	if err != nil {
		return nil, fmt.Errorf("capture Process environment: %w", err)
	}
	path, err := value.NewText(types.Path(), directory)
	if err != nil {
		return nil, fmt.Errorf("capture Process directory: invalid text")
	}
	return value.NewInheritedProcessContext(variables, path)
}
