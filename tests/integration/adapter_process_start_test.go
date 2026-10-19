package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type startEvidenceChild struct {
	*lifecycleChild
	confirmed atomic.Bool
}

func (child *startEvidenceChild) StartConfirmed() bool { return child.confirmed.Load() }

func startEvidenceFlag(t *testing.T, progress value.Record) bool {
	t.Helper()
	var found bool
	var confirmed bool
	for _, field := range progress.Fields() {
		if field.Name == "started" {
			t.Fatal("obsolete started evidence remains")
		}
		if field.Name == "start_confirmed" {
			found = true
			confirmed = field.Value.(value.Boolean).Bool()
		}
	}
	if !found {
		t.Fatal("missing start_confirmed evidence")
	}
	return confirmed
}

func TestAdapterProcessStartPendingOwnership(t *testing.T) {
	for _, scenario := range []string{"settled-never-created", "late-creation-termination", "unresolved-creation", "creation-during-failed-cleanup"} {
		t.Run(scenario, func(t *testing.T) {
			root, path, registry := registryFixture(t)
			child := &startEvidenceChild{lifecycleChild: registryChild(t)}
			child.wait = func(context.Context) (value.ProcessTermination, error) {
				if scenario == "late-creation-termination" {
					return value.NewProcessExited(value.Int64(0))
				}
				return nil, context.Canceled
			}
			child.release = func(context.Context) error {
				if scenario == "creation-during-failed-cleanup" {
					child.confirmed.Store(true)
				}
				if scenario == "unresolved-creation" || scenario == "creation-during-failed-cleanup" {
					return errors.New("creation ownership not settled")
				}
				return nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) {
				cancel()
				return child, context.Canceled
			}}
			boundary := registryBoundary(t, registry, driver)
			output, runErr := registryRecoveryProgram(t, root, path).Run(ctx, boundary, execution.DefaultBudget(), device.DefaultConfig())
			lifecycleHostFailure(t, output, runErr)
			var failure adapter.ProcessFailure
			if !errors.As(runErr, &failure) {
				t.Fatalf("pending start lost failure evidence: %v", runErr)
			}
			progress := failure.ProcessProgress()
			wantConfirmed := scenario == "late-creation-termination" || scenario == "creation-during-failed-cleanup"
			if got := startEvidenceFlag(t, progress); got != wantConfirmed {
				t.Fatalf("start_confirmed=%t, want %t", got, wantConfirmed)
			}
			wantCleanup := scenario == "settled-never-created" || scenario == "late-creation-termination"
			if got := progress.Field("cleanup_complete").(value.Boolean).Bool(); got != wantCleanup {
				t.Fatalf("cleanup_complete=%t, want %t", got, wantCleanup)
			}
			if scenario == "settled-never-created" {
				var host execution.HostError
				if !errors.As(runErr, &host) || fmt.Sprint(host.Code()) != "runtime.cancelled" {
					t.Fatalf("settled cancellation was replaced by cleanup failure: %v", runErr)
				}
			}
			if scenario != "late-creation-termination" {
				registryNoTermination(t, progress)
			}
			before, err := json.Marshal(progress)
			if err != nil {
				t.Fatal(err)
			}
			if !wantCleanup {
				child.confirmed.Store(true)
				after, err := json.Marshal(failure.ProcessProgress())
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("returned evidence changed after late ownership observation: %v", err)
				}
				fresh := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) {
					return nil, errors.New("must not start")
				}}
				if wrapped, err := registry.Driver(fresh); wrapped != nil || execution.BoundaryHostError(err) == nil {
					t.Fatal("failed pending cleanup did not quarantine registry")
				}
				if err := registry.Close(); err == nil {
					t.Fatal("unresolved creation ownership was discarded")
				}
				return
			}
			if err := registry.Close(); err != nil {
				t.Fatalf("settled creation retained registry ownership: %v", err)
			}
		})
	}
}

func TestAdapterProcessStartNilWaitCannotPublishResult(t *testing.T) {
	child := &startEvidenceChild{lifecycleChild: registryChild(t)}
	child.wait = func(context.Context) (value.ProcessTermination, error) { return nil, nil }
	driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) { return child, nil }}
	lifecycle, err := adapter.NewProcessLifecycle(driver, adapter.DefaultProcessLimits())
	if err != nil {
		t.Fatal(err)
	}
	result, runErr := lifecycle.Execute(context.Background(), lifecycleCommand(t))
	var failure adapter.ProcessFailure
	if result != nil || !errors.As(runErr, &failure) {
		t.Fatalf("nil/nil Wait published success: result=%v error=%v", result, runErr)
	}
	progress := failure.ProcessProgress()
	if startEvidenceFlag(t, progress) || !progress.Field("cleanup_complete").(value.Boolean).Bool() {
		t.Fatal("settled unconfirmed child falsely claimed creation or unverified cleanup")
	}
	registryNoTermination(t, progress)
}

func TestAdapterProcessStartNestedRegistryGetter(t *testing.T) {
	root, path, innerRegistry := registryFixture(t)
	defer innerRegistry.Close()
	// Register the same exact bytes in an independent outer registry.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := adapter.NewProcessExecutableGrant(path, sha256.Sum256(data))
	if err != nil {
		t.Fatal(err)
	}
	outerRegistry, err := adapter.OpenProcessRegistry(context.Background(), []adapter.ProcessExecutableGrant{grant}, adapter.DefaultProcessRegistryLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer outerRegistry.Close()
	child := &startEvidenceChild{lifecycleChild: registryChild(t)}
	driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) { return child, nil }}
	inner, err := innerRegistry.Driver(driver)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := outerRegistry.Driver(inner)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := outer.Start(context.Background(), registryInvocation(t, root, path))
	if err != nil {
		t.Fatal(err)
	}
	getter, ok := owned.(interface{ StartConfirmed() bool })
	if !ok {
		t.Fatal("nested registry child lost mandatory StartConfirmed getter")
	}
	if getter.StartConfirmed() {
		t.Fatal("ownership transfer falsely confirmed OS creation")
	}
	child.confirmed.Store(true)
	if !getter.StartConfirmed() {
		t.Fatal("nested registry lost positive creation observation")
	}
	if _, err := owned.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := owned.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !getter.StartConfirmed() {
		t.Fatal("StartConfirmed stopped working after Release")
	}
}
