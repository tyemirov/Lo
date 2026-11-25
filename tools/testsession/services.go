package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/testsession"
)

func startServices(ctx context.Context, root, work string, base []string, slots, guests int, groups []jobSpec, output *reporter) ([]string, func() error, error) {
	needFixture, needGuest := false, false
	for _, entry := range groups {
		if strings.HasPrefix(entry.ID, "go/tests/integration/") {
			needFixture, needGuest = true, true
		}
		if entry.ID == "acceptance/distributions" {
			needGuest = true
		}
	}
	closeFixture := func() error { return nil }
	if !needGuest {
		return base, closeFixture, nil
	}
	if needFixture {
		manifest := ""
		for _, item := range base {
			if value, ok := strings.CutPrefix(item, "LO_LLVM_TOOLCHAIN="); ok {
				manifest = value
			}
		}
		if manifest == "" {
			manifest = filepath.Join(root, "_scratch/native-runtime/distribution/toolchain.json")
		}
		service, err := testsession.Start(ctx, testsession.Config{Directory: filepath.Join(work, "fixture"), Toolchain: manifest, Compiler: filepath.Join(work, "bin/lo"), BuildSlots: slots, MaxBytes: 768 << 20})
		if err != nil {
			return nil, nil, fmt.Errorf("start fixture coordinator: %w", err)
		}
		closeFixture = func() error {
			stats := service.Stats()
			output.event("service", fmt.Sprintf("fixture-builds=%d,reused=%d,evicted=%d,retained=%d,bytes=%d", stats.Builds, stats.Reused, stats.Evicted, stats.Retained, stats.Bytes), "stop")
			return service.Close()
		}
		base = environment(base, map[string]string{"LO_TEST_SESSION": service.Address()}, work)
	}
	stop := closeFixture
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
		return base, stop, nil
	}
	preparation := os.Getenv("LO_NATIVE_PREPARATION")
	if preparation == "" {
		preparation = filepath.Join(root, "_scratch/native-runtime")
	}
	socket := filepath.Join(work, "guest.sock")
	ready := filepath.Join(work, "guest-ready.json")
	command := ownedCommand(ctx, []string{"python3", "-B", filepath.Join(root, "tools/nativeruntime/session.py"), "--preparation", preparation, "--work", filepath.Join(work, "guest"), "--socket", socket, "--workers", strconv.Itoa(guests), "--ready", ready})
	command.Env = base
	command.Dir = root
	diagnostics := newTailBuffer(64 << 10)
	command.Stdout = diagnostics
	command.Stderr = diagnostics
	if err := command.Start(); err != nil {
		_ = closeFixture()
		return nil, nil, err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	stop = func() error {
		if command.Process != nil {
			_ = command.Process.Signal(os.Interrupt)
		}
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = command.Cancel()
			<-done
		}
		return closeFixture()
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = stop()
			return nil, nil, ctx.Err()
		case err := <-done:
			_ = closeFixture()
			return nil, nil, fmt.Errorf("guest pool exited: %w\n%s", err, diagnostics.String())
		case <-ticker.C:
			data, err := os.ReadFile(ready)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				_ = stop()
				return nil, nil, err
			}
			var readiness struct {
				Socket  string `json:"socket"`
				Workers int    `json:"workers"`
				Boots   int    `json:"guest_boots"`
			}
			if err = json.Unmarshal(data, &readiness); err != nil {
				_ = stop()
				return nil, nil, err
			}
			if readiness.Socket != socket || readiness.Workers != guests {
				_ = stop()
				return nil, nil, fmt.Errorf("guest pool returned invalid readiness")
			}
			output.event("service", "linux-guests", "ready")
			base = environment(base, map[string]string{"LO_TEST_GUEST_SOCKET": socket}, work)
			return base, stop, nil
		}
	}
}
