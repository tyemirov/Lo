package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func registryByteFixture(t *testing.T, path string, content []byte) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write(content)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("create byte fixture: write=%v close=%v", writeErr, closeErr)
	}
}

func registryRemoveSource(root, name string) string {
	return fmt.Sprintf(`flow Select(name: String, entry: File.Entry) -> Bool { return String.eq(name, entry.name) }; flow Main() -> File.RemoveReceipt! { entries = File.list(File.Path.new(%q)); selected = List.filter(entries, %q, Select); target = File.Remove.new(selected); receipt = commit target; return receipt }; return Main()`, root, name)
}

func registryProgram(t *testing.T, root, path string) execution.Program {
	t.Helper()
	return processCompositionProgram(t, root, fmt.Sprintf(`flow Main() -> Process.Result! { executable = Process.Executable.new(File.Path.new(%q)); target = Process.Command.new(executable, List.new("argument"), Map.new(Record.new(key: "ONLY", value: "explicit")), File.Path.new(%q), UTF8.Bytes.new("input")); result = commit target; return result }; return Main()`, path, root))
}

func registryRecoveryProgram(t *testing.T, root, path string) execution.Program {
	t.Helper()
	return processCompositionProgram(t, root, fmt.Sprintf(`flow Recover(error: Error) -> Int { return 73 }; flow Main() -> Int onError Recover { executable = Process.Executable.new(File.Path.new(%q)); target = Process.Command.new(executable, List.new("argument"), Map.new(Record.new(key: "ONLY", value: "explicit")), File.Path.new(%q), UTF8.Bytes.new("input")); result = commit target; return 1 }; return Main()`, path, root))
}

func registryNoTermination(t *testing.T, progress value.Record) {
	t.Helper()
	for _, field := range progress.Fields() {
		if field.Name == "termination" {
			t.Fatal("failed Start published termination evidence")
		}
	}
}

func registryStartCount(driver *lifecycleDriver) int {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	return driver.starts
}

func registryFixture(t *testing.T) (string, string, adapter.ProcessRegistry) {
	t.Helper()
	root := fileFixtureRoot(t)
	path := filepath.Join(root, "approved-image")
	content := []byte("approved byte fixture")
	registryByteFixture(t, path, content)
	grant, err := adapter.NewProcessExecutableGrant(path, sha256.Sum256(content))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := adapter.OpenProcessRegistry(context.Background(), []adapter.ProcessExecutableGrant{grant}, adapter.DefaultProcessRegistryLimits())
	if err != nil {
		t.Fatal(err)
	}
	return root, path, registry
}

func registryInvocation(t *testing.T, root, path string) adapter.ProcessInvocation {
	t.Helper()
	var invocation adapter.ProcessInvocation
	driver := &lifecycleDriver{start: func(_ context.Context, input adapter.ProcessInvocation) (adapter.ProcessChild, error) {
		invocation = input
		return nil, errors.New("capture invocation without child resources")
	}}
	_, _ = registryProgram(t, root, path).Run(context.Background(), lifecycleBoundary(t, driver, 1024), execution.DefaultBudget(), device.DefaultConfig())
	if invocation == nil {
		t.Fatal("public lifecycle did not derive invocation")
	}
	return invocation
}

func registryChild(t *testing.T) *lifecycleChild {
	t.Helper()
	termination, err := value.NewProcessExited(value.Int64(0))
	if err != nil {
		t.Fatal(err)
	}
	return &lifecycleChild{input: &lifecycleInputPipe{}, output: io.NopCloser(bytes.NewReader(nil)), errors: io.NopCloser(bytes.NewReader(nil)), wait: func(context.Context) (value.ProcessTermination, error) { return termination, nil }, stop: func(context.Context) error { return nil }, release: func(context.Context) error { return nil }}
}

func registryBoundary(t *testing.T, registry adapter.ProcessRegistry, driver adapter.ProcessDriver) execution.Boundary {
	t.Helper()
	wrapped, err := registry.Driver(driver)
	if err != nil {
		t.Fatal(err)
	}
	return lifecycleBoundary(t, wrapped, 1024)
}

func registryFileCommit(t *testing.T, root string, files adapter.Files, code string) error {
	t.Helper()
	program := processCompositionProgram(t, root, code)
	_, err := program.Run(context.Background(), files, execution.DefaultBudget(), device.DefaultConfig())
	return err
}

func TestAdapterProcessRegistryProtectsFileAliases(t *testing.T) {
	root := fileFixtureRoot(t)
	path := filepath.Join(root, "approved-image")
	original := []byte("registered immutable byte fixture")
	registryByteFixture(t, path, original)
	hardlink := filepath.Join(root, "hardlink-image")
	symlink := filepath.Join(root, "symlink-image")
	if err := os.Link(path, hardlink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(path), symlink); err != nil {
		t.Fatal(err)
	}
	grant, err := adapter.NewProcessExecutableGrant(path, sha256.Sum256(original))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := adapter.OpenProcessRegistry(context.Background(), []adapter.ProcessExecutableGrant{grant}, adapter.DefaultProcessRegistryLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := registry.Close(); err != nil {
			t.Error(err)
		}
	})
	for index := 0; index < 2; index++ {
		files, err := registry.OpenFiles(adapter.FileRoots{Write: []string{root}, List: []string{root}, Remove: []string{root}}, adapter.NativeFileIO())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := files.Close(); err != nil {
				t.Error(err)
			}
		})
		for _, selected := range []string{path, hardlink, symlink} {
			for _, operation := range []string{"Write", "Append"} {
				t.Run(fmt.Sprintf("adapter-%d/%s/%s", index, filepath.Base(selected), operation), func(t *testing.T) {
					code := fmt.Sprintf(`flow Main() -> File.%sReceipt! { target = File.%s.new(File.Path.new(%q), UTF8.Bytes.new("modified")); receipt = commit target; return receipt }; return Main()`, operation, operation, selected)
					if err := registryFileCommit(t, root, files, code); err == nil || execution.BoundaryHostError(err) == nil {
						t.Fatalf("protected mutation must fail at host boundary, got %v", err)
					}
					data, err := os.ReadFile(path)
					if err != nil || string(data) != string(original) {
						t.Fatalf("protected bytes changed before rejection: %q, %v", data, err)
					}
				})
			}
			if selected != symlink {
				if err := registryFileCommit(t, root, files, registryRemoveSource(root, filepath.Base(selected))); err == nil || execution.BoundaryHostError(err) == nil {
					t.Fatalf("protected removal must fail at host boundary: %v", err)
				}
				if _, err := os.Lstat(selected); err != nil {
					t.Fatalf("protected name removed: %v", err)
				}
			} else {
				program := processCompositionProgram(t, root, registryRemoveSource(root, filepath.Base(symlink)))
				output, runErr := program.Run(context.Background(), files, execution.DefaultBudget(), device.DefaultConfig())
				var failure execution.Failure
				if output != nil || !errors.As(runErr, &failure) || failure.Value().Kind() != "File.InvalidSelection" || execution.BoundaryHostError(runErr) != nil {
					t.Fatal("symlink removal bypassed the regular-entry selection contract")
				}
				if _, err := os.Readlink(symlink); err != nil {
					t.Fatalf("invalid symlink selection mutated the alias: %v", err)
				}
			}
		}
		sibling := filepath.Join(root, fmt.Sprintf("allowed-%d", index))
		code := fmt.Sprintf(`flow Main() -> File.WriteReceipt! { target = File.Write.new(File.Path.new(%q), UTF8.Bytes.new("allowed")); receipt = commit target; return receipt }; return Main()`, sibling)
		if err := registryFileCommit(t, root, files, code); err != nil {
			t.Fatalf("unrelated sibling write: %v", err)
		}
		data, err := os.ReadFile(sibling)
		if err != nil || string(data) != "allowed" {
			t.Fatalf("unrelated sibling bytes: %q, %v", data, err)
		}
	}
}

func TestAdapterProcessRegistryAdmission(t *testing.T) {
	for _, scenario := range []string{"digest-change", "identity-replacement", "unregistered", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			root, path, registry := registryFixture(t)
			defer registry.Close()
			selected := path
			switch scenario {
			case "digest-change":
				registryByteFixture(t, path, []byte("changed bytes"))
			case "identity-replacement":
				replacement := filepath.Join(root, "replacement")
				registryByteFixture(t, replacement, []byte("approved byte fixture"))
				if err := os.Rename(replacement, path); err != nil {
					t.Fatal(err)
				}
			case "unregistered":
				selected = filepath.Join(root, "unregistered")
				registryByteFixture(t, selected, []byte("approved byte fixture"))
			}
			driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) {
				t.Error("admission failure reached underlying Start")
				return nil, errors.New("unexpected Start")
			}}
			boundary := registryBoundary(t, registry, driver)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "canceled" {
				cancel()
				wrapped, err := registry.Driver(driver)
				if err != nil {
					t.Fatal(err)
				}
				invocation := registryInvocation(t, root, selected)
				child, err := wrapped.Start(ctx, invocation)
				if child != nil || execution.BoundaryHostError(err) == nil || registryStartCount(driver) != 0 {
					t.Fatal("canceled direct registry admission delegated Start")
				}
			}
			output, runErr := registryProgram(t, root, selected).Run(ctx, boundary, execution.DefaultBudget(), device.DefaultConfig())
			lifecycleHostFailure(t, output, runErr)
			if registryStartCount(driver) != 0 {
				t.Fatal("registry delegated a rejected invocation")
			}
		})
	}
}

func TestAdapterProcessRegistryQuarantine(t *testing.T) {
	root, path, registry := registryFixture(t)
	invocation := registryInvocation(t, root, path)
	child := registryChild(t)
	allowRelease := false
	child.release = func(context.Context) error {
		if !allowRelease {
			return errors.New("injected Release failure")
		}
		return nil
	}
	driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) {
		return child, errors.New("partial Start")
	}}
	wrapped, err := registry.Driver(driver)
	if err != nil {
		t.Fatal(err)
	}
	owned, startErr := wrapped.Start(context.Background(), invocation)
	if owned == nil || startErr == nil {
		t.Fatal("partial Start did not transfer child ownership")
	}
	if err := owned.Release(context.Background()); err == nil {
		t.Fatal("Release failure disappeared")
	}
	if next, err := wrapped.Start(context.Background(), invocation); next != nil || execution.BoundaryHostError(err) == nil {
		t.Fatal("quarantined registry admitted a new child")
	}
	allowRelease = true
	if err := owned.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if next, err := wrapped.Start(context.Background(), invocation); next != nil || execution.BoundaryHostError(err) == nil {
		t.Fatal("successful later Release cleared sticky quarantine")
	}
	if registryStartCount(driver) != 1 {
		t.Fatal("quarantine delegated a later Start")
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	if err := registry.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestAdapterProcessRegistryNestedUnresolvedStart(t *testing.T) {
	for _, contextual := range []bool{false, true} {
		t.Run(fmt.Sprintf("contextual-%t", contextual), func(t *testing.T) {
			root, path, innerRegistry := registryFixture(t)
			grant, err := adapter.NewProcessExecutableGrant(path, sha256.Sum256([]byte("approved byte fixture")))
			if err != nil {
				t.Fatal(err)
			}
			outerRegistry, err := adapter.OpenProcessRegistry(context.Background(), []adapter.ProcessExecutableGrant{grant}, adapter.DefaultProcessRegistryLimits())
			if err != nil {
				t.Fatal(err)
			}
			invocation := registryInvocation(t, root, path)
			driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) { return nil, nil }}
			inner, err := innerRegistry.Driver(driver)
			if err != nil {
				t.Fatal(err)
			}
			bridge := &lifecycleDriver{start: func(ctx context.Context, input adapter.ProcessInvocation) (adapter.ProcessChild, error) {
				child, err := inner.Start(ctx, input)
				if err != nil && contextual {
					return child, fmt.Errorf("trusted nested context: %w", err)
				}
				return child, err
			}}
			outer, err := outerRegistry.Driver(bridge)
			if err != nil {
				t.Fatal(err)
			}
			if child, err := outer.Start(context.Background(), invocation); child != nil || execution.BoundaryHostError(err) == nil {
				t.Fatal("nested nil/nil did not remain a host contract failure")
			}
			fresh := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) {
				return nil, errors.New("must not delegate")
			}}
			if wrapped, err := outerRegistry.Driver(fresh); wrapped != nil || execution.BoundaryHostError(err) == nil {
				t.Error("outer registry factory did not enforce its own quarantine")
			}
			for _, selected := range []adapter.ProcessDriver{inner, outer} {
				if child, err := selected.Start(context.Background(), invocation); child != nil || execution.BoundaryHostError(err) == nil {
					t.Error("nested registry admitted a subsequent activation")
				}
			}
			if registryStartCount(driver) != 1 || registryStartCount(bridge) != 1 || registryStartCount(fresh) != 0 {
				t.Error("nested registry quarantine delegated after unresolved Start")
			}
			for _, registry := range []adapter.ProcessRegistry{innerRegistry, outerRegistry} {
				if err := registry.Close(); err == nil {
					t.Error("nested registry lost unresolved activation ownership")
				}
			}
			// Both unresolved registries intentionally remain until test-process exit.
		})
	}
}

func TestAdapterProcessRegistryNilChildOwnership(t *testing.T) {
	for _, scenario := range []string{"nil-nil", "nil-error"} {
		t.Run(scenario, func(t *testing.T) {
			root, path, registry := registryFixture(t)
			invocation := registryInvocation(t, root, path)
			driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) {
				if scenario == "nil-error" {
					return nil, errors.New("trusted Start failure without owned resources")
				}
				return nil, nil
			}}
			wrapped, err := registry.Driver(driver)
			if err != nil {
				t.Fatal(err)
			}
			child, startErr := wrapped.Start(context.Background(), invocation)
			if child != nil || startErr == nil || registryStartCount(driver) != 1 {
				t.Fatal("failed Start did not preserve its ownership contract")
			}
			if scenario == "nil-error" {
				if err := registry.Close(); err != nil {
					t.Fatalf("trusted no-child failure retained a lease: %v", err)
				}
				return
			}
			if execution.BoundaryHostError(startErr) == nil {
				t.Fatal("nil/nil Start was not a host contract failure")
			}
			if next, err := wrapped.Start(context.Background(), invocation); next != nil || execution.BoundaryHostError(err) == nil {
				t.Fatal("nil/nil contract violation did not quarantine admission")
			}
			if registryStartCount(driver) != 1 {
				t.Fatal("quarantined registry called underlying Start again")
			}
			if err := registry.Close(); err == nil {
				t.Fatal("Close claimed cleanup without a child ownership handle")
			}
			// No recovery API can prove cleanup after this trusted-host violation.
			// The unresolved registry intentionally survives until this test process exits.
		})
	}
}

func TestAdapterProcessRegistryLifecycleNilChildOwnership(t *testing.T) {
	for _, contextual := range []bool{false, true} {
		t.Run(fmt.Sprintf("contextual-%t", contextual), func(t *testing.T) {
			root, path, registry := registryFixture(t)
			driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) {
				return nil, nil
			}}
			boundary := registryBoundary(t, registry, driver)
			if contextual {
				wrapped, err := registry.Driver(driver)
				if err != nil {
					t.Fatal(err)
				}
				contextualDriver := &lifecycleDriver{start: func(ctx context.Context, invocation adapter.ProcessInvocation) (adapter.ProcessChild, error) {
					child, err := wrapped.Start(ctx, invocation)
					if err != nil {
						return child, fmt.Errorf("trusted wrapper context: %w", err)
					}
					return child, nil
				}}
				boundary = lifecycleBoundary(t, contextualDriver, 1024)
			}
			program := registryRecoveryProgram(t, root, path)
			output, runErr := program.Run(context.Background(), boundary, execution.DefaultBudget(), device.DefaultConfig())
			lifecycleHostFailure(t, output, runErr)
			var failure adapter.ProcessFailure
			if !errors.As(runErr, &failure) {
				t.Fatalf("public Run lost ProcessFailure evidence: %v", runErr)
			}
			if failure.ProcessProgress().Field("cleanup_complete").(value.Boolean).Bool() {
				t.Fatal("nil/nil registry Start falsely claimed lifecycle cleanup")
			}
			registryNoTermination(t, failure.ProcessProgress())
			output, runErr = program.Run(context.Background(), boundary, execution.DefaultBudget(), device.DefaultConfig())
			lifecycleHostFailure(t, output, runErr)
			if registryStartCount(driver) != 1 {
				t.Fatal("quarantined lifecycle delegated another Start")
			}
			if err := registry.Close(); err == nil {
				t.Fatal("lifecycle lost unresolved registry ownership")
			}
		})
	}
}

func TestAdapterProcessRegistryDirectLifecycleNilChildOwnership(t *testing.T) {
	for _, scenario := range []string{"nil-nil", "nil-error"} {
		t.Run(scenario, func(t *testing.T) {
			driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) {
				if scenario == "nil-error" {
					return nil, errors.New("trusted failure with no resources")
				}
				return nil, nil
			}}
			lifecycle, err := adapter.NewProcessLifecycle(driver, adapter.DefaultProcessLimits())
			if err != nil {
				t.Fatal(err)
			}
			output, runErr := lifecycle.Execute(context.Background(), lifecycleCommand(t))
			var failure adapter.ProcessFailure
			if output != nil || !errors.As(runErr, &failure) {
				t.Fatalf("failed direct lifecycle lost evidence: output=%v, error=%v", output, runErr)
			}
			complete := failure.ProcessProgress().Field("cleanup_complete").(value.Boolean).Bool()
			registryNoTermination(t, failure.ProcessProgress())
			if complete != (scenario == "nil-error") {
				t.Fatalf("%s cleanup_complete=%t", scenario, complete)
			}
			if scenario == "nil-nil" && execution.BoundaryHostError(runErr) == nil {
				t.Fatal("direct nil/nil Start did not fail as host contract error")
			}
			if registryStartCount(driver) != 1 {
				t.Fatal("direct lifecycle repeated Start")
			}
			if scenario == "nil-nil" {
				root := fileFixtureRoot(t)
				program := registryRecoveryProgram(t, root, filepath.Join(root, "description-only"))
				boundary, err := adapter.WithProcessLifecycle(invalidReceiptBoundary{}, lifecycle)
				if err != nil {
					t.Fatal(err)
				}
				result, err := program.Run(context.Background(), boundary, execution.DefaultBudget(), device.DefaultConfig())
				lifecycleHostFailure(t, result, err)
			}
		})
	}
}

func TestAdapterProcessRegistrySymlinkAncestorWriteRoot(t *testing.T) {
	root := fileFixtureRoot(t)
	path := filepath.Join(root, "approved-image")
	content := []byte("approved byte fixture")
	registryByteFixture(t, path, content)
	aliasRoot := filepath.Join(root, "namespace-alias")
	if err := os.Symlink(root, aliasRoot); err != nil {
		t.Fatal(err)
	}
	grant, err := adapter.NewProcessExecutableGrant(path, sha256.Sum256(content))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := adapter.OpenProcessRegistry(context.Background(), []adapter.ProcessExecutableGrant{grant}, adapter.DefaultProcessRegistryLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	for _, writeRoot := range []string{root, aliasRoot} {
		files, err := registry.OpenFiles(adapter.FileRoots{Write: []string{writeRoot}}, adapter.NativeFileIO())
		if err != nil {
			t.Fatal(err)
		}
		defer files.Close()
		for _, operation := range []string{"Write", "Append"} {
			selected := filepath.Join(writeRoot, filepath.Base(path))
			code := fmt.Sprintf(`flow Main() -> File.%sReceipt! { target = File.%s.new(File.Path.new(%q), UTF8.Bytes.new("forbidden")); receipt = commit target; return receipt }; return Main()`, operation, operation, selected)
			if err := registryFileCommit(t, root, files, code); err == nil || execution.BoundaryHostError(err) == nil {
				t.Fatalf("%s through root %q bypassed namespace protection: %v", operation, writeRoot, err)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "approved byte fixture" {
				t.Fatalf("alias-root mutation changed protected bytes: %q, %v", data, err)
			}
		}
		sibling := filepath.Join(writeRoot, "allowed-sibling")
		code := fmt.Sprintf(`flow Main() -> File.WriteReceipt! { target = File.Write.new(File.Path.new(%q), UTF8.Bytes.new("allowed")); receipt = commit target; return receipt }; return Main()`, sibling)
		if err := registryFileCommit(t, root, files, code); err != nil {
			t.Fatalf("unrelated sibling through alias root: %v", err)
		}
		if data, err := os.ReadFile(filepath.Join(root, "allowed-sibling")); err != nil || string(data) != "allowed" {
			t.Fatalf("unrelated alias-root write: %q, %v", data, err)
		}
	}
}

func TestAdapterProcessRegistryRejectsReplacedCreationParent(t *testing.T) {
	root, image, registry := registryFixture(t)
	defer registry.Close()
	writeRoot, err := os.MkdirTemp(root, "granted-parent-")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := os.MkdirTemp(root, "replacement-parent-")
	if err != nil {
		t.Fatal(err)
	}
	files, err := registry.OpenFiles(adapter.FileRoots{Write: []string{writeRoot}}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	moved := filepath.Join(root, "retained-parent")
	if err := os.Rename(writeRoot, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, writeRoot); err != nil {
		t.Fatal(err)
	}
	// Replace the trusted namespace before the operation, not concurrently with it.
	for _, operation := range []string{"Write", "Append"} {
		name := "new-" + operation
		target := filepath.Join(writeRoot, name)
		code := fmt.Sprintf(`flow Main() -> File.%sReceipt! { target = File.%s.new(File.Path.new(%q), UTF8.Bytes.new("forbidden")); receipt = commit target; return receipt }; return Main()`, operation, operation, target)
		if err := registryFileCommit(t, root, files, code); err == nil || execution.BoundaryHostError(err) == nil {
			t.Fatalf("%s accepted a replaced creation parent: %v", operation, err)
		}
		for _, directory := range []string{writeRoot, moved} {
			if _, err := os.Lstat(filepath.Join(directory, name)); !os.IsNotExist(err) {
				t.Fatalf("%s created a file in %q before rejection: %v", operation, directory, err)
			}
		}
		if data, err := os.ReadFile(image); err != nil || string(data) != "approved byte fixture" {
			t.Fatalf("creation-parent replacement changed approved image: %q, %v", data, err)
		}
	}
}

func TestAdapterProcessRegistryRegistrationLimits(t *testing.T) {
	root := fileFixtureRoot(t)
	path := filepath.Join(root, "image")
	data := []byte("four")
	registryByteFixture(t, path, data)
	grant, err := adapter.NewProcessExecutableGrant(path, sha256.Sum256(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"cap", "cancel", "digest"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			maximum := int64(len(data))
			if scenario == "cap" {
				maximum--
			} else {
				if scenario == "cancel" {
					cancel()
				}
			}
			limits, err := adapter.NewProcessRegistryLimits(maximum)
			if err != nil {
				t.Fatal(err)
			}
			selectedGrant := grant
			if scenario == "digest" {
				selectedGrant, err = adapter.NewProcessExecutableGrant(path, sha256.Sum256([]byte("not the fixture")))
				if err != nil {
					t.Fatal(err)
				}
			}
			registry, err := adapter.OpenProcessRegistry(ctx, []adapter.ProcessExecutableGrant{selectedGrant}, limits)
			if registry != nil || err == nil {
				t.Fatalf("registration must fail: registry=%v, error=%v", registry, err)
			}
		})
	}
}

func TestAdapterProcessRegistryCloseActiveLease(t *testing.T) {
	root, path, registry := registryFixture(t)
	files, err := registry.OpenFiles(adapter.FileRoots{Write: []string{root}}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	entered, finish := make(chan struct{}), make(chan struct{})
	child := registryChild(t)
	child.wait = func(ctx context.Context) (value.ProcessTermination, error) {
		select {
		case <-finish:
			return value.NewProcessExited(value.Int64(0))
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	driver := &lifecycleDriver{start: func(context.Context, adapter.ProcessInvocation) (adapter.ProcessChild, error) {
		close(entered)
		return child, nil
	}}
	boundary := registryBoundary(t, registry, driver)
	program := registryProgram(t, root, path)
	done := make(chan error, 1)
	go func() {
		_, err := program.Run(context.Background(), boundary, execution.DefaultBudget(), device.DefaultConfig())
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(finish)
		t.Fatal("injected child did not enter")
	}
	if err := registry.Close(); err == nil {
		close(finish)
		t.Fatal("Close succeeded during activation lease")
	}
	close(finish)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("activation did not release its lease")
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	code := fmt.Sprintf(`flow Main() -> File.WriteReceipt! { target = File.Write.new(File.Path.new(%q), UTF8.Bytes.new("forbidden")); receipt = commit target; return receipt }; return Main()`, filepath.Join(root, "after-close"))
	if err := registryFileCommit(t, root, files, code); err == nil || execution.BoundaryHostError(err) == nil {
		t.Fatalf("retained adapter bypassed registry Close: %v", err)
	}
}

type registryCallbackIO struct {
	adapter.FileIO
	write  func(context.Context, *os.File, []byte) (int, error)
	remove func(*os.Root, string) error
}

func (f *registryCallbackIO) Write(ctx context.Context, file *os.File, data []byte) (int, error) {
	if f.write != nil {
		return f.write(ctx, file, data)
	}
	return f.FileIO.Write(ctx, file, data)
}

func (f *registryCallbackIO) Remove(root *os.Root, name string) error {
	if f.remove != nil {
		return f.remove(root, name)
	}
	return f.FileIO.Remove(root, name)
}

func TestAdapterProcessRegistryReentrantRemoval(t *testing.T) {
	root, path, registry := registryFixture(t)
	defer registry.Close()
	victim := filepath.Join(root, "victim")
	registryByteFixture(t, victim, []byte("remove this sibling"))
	second, err := registry.OpenFiles(adapter.FileRoots{Write: []string{root}}, adapter.NativeFileIO())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	callback := &registryCallbackIO{FileIO: adapter.NativeFileIO()}
	called := make(chan struct{}, 1)
	callback.remove = func(directory *os.Root, name string) error {
		allowed := fmt.Sprintf(`flow Main() -> File.WriteReceipt! { target = File.Write.new(File.Path.new(%q), UTF8.Bytes.new("nested")); receipt = commit target; return receipt }; return Main()`, filepath.Join(root, "nested-write"))
		if err := registryFileCommit(t, root, second, allowed); err != nil {
			return fmt.Errorf("reentrant sibling write: %w", err)
		}
		protected := fmt.Sprintf(`flow Main() -> File.WriteReceipt! { target = File.Write.new(File.Path.new(%q), UTF8.Bytes.new("forbidden")); receipt = commit target; return receipt }; return Main()`, path)
		if err := registryFileCommit(t, root, second, protected); err == nil || execution.BoundaryHostError(err) == nil {
			return errors.New("reentrant callback bypassed executable protection")
		}
		called <- struct{}{}
		return directory.Remove(name)
	}
	first, err := registry.OpenFiles(adapter.FileRoots{List: []string{root}, Remove: []string{root}}, callback)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	program := processCompositionProgram(t, root, registryRemoveSource(root, "victim"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := program.Run(ctx, first, execution.DefaultBudget(), device.DefaultConfig())
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("registry held a mutex across reentrant FileIO.Remove")
	}
	select {
	case <-called:
	default:
		t.Fatal("real File removal did not reach callback")
	}
	if _, err := os.ReadFile(victim); !os.IsNotExist(err) {
		t.Fatalf("unrelated victim was not removed: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "nested-write")); err != nil || string(data) != "nested" {
		t.Fatalf("nested sibling write failed: %q, %v", data, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "approved byte fixture" {
		t.Fatalf("protected bytes changed in callback: %q, %v", data, err)
	}
}

func TestAdapterProcessRegistryCloseDuringMutation(t *testing.T) {
	root, _, registry := registryFixture(t)
	entered, finish := make(chan struct{}), make(chan struct{})
	filesystem := &registryCallbackIO{FileIO: adapter.NativeFileIO()}
	filesystem.write = func(ctx context.Context, file *os.File, data []byte) (int, error) {
		close(entered)
		select {
		case <-finish:
			return file.Write(data)
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	files, err := registry.OpenFiles(adapter.FileRoots{Write: []string{root}}, filesystem)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	path := filepath.Join(root, "allowed-write")
	program := processCompositionProgram(t, root, fmt.Sprintf(`flow Main() -> File.WriteReceipt! { target = File.Write.new(File.Path.new(%q), UTF8.Bytes.new("allowed")); receipt = commit target; return receipt }; return Main()`, path))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := program.Run(ctx, files, execution.DefaultBudget(), device.DefaultConfig())
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(finish)
		t.Fatal("mutation callback did not enter")
	}
	if err := registry.Close(); err == nil {
		close(finish)
		t.Fatal("Close succeeded while mutation callback owned a lease")
	}
	close(finish)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("mutation did not release its lease")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "allowed" {
		t.Fatalf("mutation did not finish: %q, %v", data, err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
}
