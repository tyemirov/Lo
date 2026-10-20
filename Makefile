.PHONY: tool-state
GO := go
override LO_BUILD_ROOT := $(abspath $(CURDIR)/_scratch/build-state)
override HOME := $(LO_BUILD_ROOT)/home
override TMPDIR := $(LO_BUILD_ROOT)/tmp
override TMP := $(TMPDIR)
override TEMP := $(TMPDIR)
override GOCACHE := $(LO_BUILD_ROOT)/go-build
override GOMODCACHE := $(LO_BUILD_ROOT)/modules
override GOPATH := $(LO_BUILD_ROOT)/gopath
override GOTOOLCHAIN := local
override GOENV := off
override GOWORK := off
override GOTELEMETRY := off
override XDG_CACHE_HOME := $(LO_BUILD_ROOT)/cache
override XDG_CONFIG_HOME := $(LO_BUILD_ROOT)/config
override XDG_DATA_HOME := $(LO_BUILD_ROOT)/data
override XDG_STATE_HOME := $(LO_BUILD_ROOT)/state
override CLANG_MODULE_CACHE_PATH := $(LO_BUILD_ROOT)/clang
override CCACHE_DIR := $(LO_BUILD_ROOT)/ccache
override UV_CACHE_DIR := $(LO_BUILD_ROOT)/uv-cache
override UV_PYTHON_INSTALL_DIR := $(LO_BUILD_ROOT)/uv-python
override UV_TOOL_DIR := $(LO_BUILD_ROOT)/uv-tools
override UV_TOOL_BIN_DIR := $(LO_BUILD_ROOT)/uv-bin
override PIP_CACHE_DIR := $(LO_BUILD_ROOT)/pip-cache
override PYTHONPYCACHEPREFIX := $(LO_BUILD_ROOT)/python-cache
override CI := 1
export LO_BUILD_ROOT HOME TMPDIR TMP TEMP GOCACHE GOMODCACHE GOPATH GOTOOLCHAIN GOENV GOWORK GOTELEMETRY XDG_CACHE_HOME XDG_CONFIG_HOME XDG_DATA_HOME XDG_STATE_HOME CLANG_MODULE_CACHE_PATH CCACHE_DIR UV_CACHE_DIR UV_TOOL_DIR UV_TOOL_BIN_DIR UV_PYTHON_INSTALL_DIR PIP_CACHE_DIR PYTHONPYCACHEPREFIX CI
TEST_ARGS ?=
BENCH_COUNT ?= 10
BENCH_TIME ?= 300ms
NATIVE_BENCH_WORK ?= _scratch/f001-native/benchmark
NATIVE_BENCH_LANE ?= all
NATIVE_BACKEND_INPUTS ?= _scratch/native-backends/inputs.json

.DEFAULT_GOAL := build

.PHONY: build fmt lint test test-integration test-conformance test-native test-native-race runtime-bundle verify-runtime-bundle benchmark-training benchmark-training-cli benchmark-training-profile benchmark-native-baseline benchmark-native ci

ifeq ($(shell uname -s),Darwin)
PROCESS_LAUNCHER := bin/lo-process-launcher
.PHONY: process-launcher verify-process-launcher
process-launcher: | tool-state
	python3 -B tools/processbuild/identity.py build $(PROCESS_LAUNCHER) internal/processhost/helper_identity.go </dev/null

verify-process-launcher: | tool-state
	python3 -B tools/processbuild/identity.py verify $(PROCESS_LAUNCHER) internal/processhost/helper_identity.go </dev/null
endif

runtime-bundle: $(if $(PROCESS_LAUNCHER),process-launcher) | tool-state
	$(GO) run ./tools/runtimebundle </dev/null

verify-runtime-bundle: | tool-state
	$(GO) run ./tools/runtimebundle --check </dev/null

build: runtime-bundle | tool-state
	$(GO) build -o bin/lo ./cmd/lo </dev/null

.PHONY: build-metal test-metal test-native-metal
build-metal: runtime-bundle | tool-state
	mkdir -p bin </dev/null
	$(GO) run ./tools/metalbuild </dev/null

test-metal: runtime-bundle | tool-state
	mkdir -p bin </dev/null
	$(GO) run ./tools/metalbuild -test '^TestMetalLive' -- $(TEST_ARGS) </dev/null

test-native-metal: runtime-bundle | tool-state
	mkdir -p bin </dev/null
	$(GO) run ./tools/metalbuild -test '^TestNativeMetal' -- $(TEST_ARGS) </dev/null

fmt: | tool-state
	$(GO) fmt ./... </dev/null

lint: | tool-state
	$(GO) vet ./... </dev/null

test: runtime-bundle | tool-state
	$(GO) test ./... $(TEST_ARGS) </dev/null

test-integration: runtime-bundle | tool-state
	$(GO) test ./tests/integration $(TEST_ARGS) </dev/null

test-native: runtime-bundle | tool-state
	LO_TEST_BACKEND=native $(GO) test ./tests/integration $(TEST_ARGS) </dev/null

test-native-race: runtime-bundle | tool-state
	LO_TEST_BACKEND=native LO_TEST_NATIVE_RACE=1 GORACE=atexit_sleep_ms=0 $(GO) test -race ./tests/integration $(TEST_ARGS) </dev/null

test-conformance: runtime-bundle | tool-state
	$(GO) test ./tests/integration -run TestConformance $(TEST_ARGS) </dev/null

benchmark-training: | tool-state
	GOMAXPROCS=1 $(GO) test ./tests/benchmark -run '^$$' -bench BenchmarkTraining -benchmem -count=$(BENCH_COUNT) -benchtime=$(BENCH_TIME) </dev/null

benchmark-native-baseline: | tool-state
	GOMAXPROCS=1 $(GO) test ./tests/benchmark -run '^$$' -bench BenchmarkNativeBaseline -benchmem -count=$(BENCH_COUNT) -benchtime=$(BENCH_TIME) </dev/null

benchmark-native: build | tool-state
	GOMAXPROCS=1 python3 scripts/qualify_native.py --binary bin/lo --workdir $(NATIVE_BENCH_WORK) --samples $(BENCH_COUNT) --lane $(NATIVE_BENCH_LANE) </dev/null

benchmark-training-cli: build | tool-state
	GOMAXPROCS=1 python3 scripts/benchmark_training.py --binary bin/lo --samples $(BENCH_COUNT) </dev/null

benchmark-training-profile: build | tool-state
	GOMAXPROCS=1 $(GO) test ./tests/benchmark -run '^$$' -bench 'BenchmarkTraining/(MVP200|Dense20)/Execute$$' -benchtime=3s -cpuprofile bin/training-cpu.pprof -memprofile bin/training-alloc.pprof -o bin/training-benchmark.test </dev/null
	$(GO) tool pprof -top -nodecount=30 bin/training-benchmark.test bin/training-cpu.pprof </dev/null
	$(GO) tool pprof -top -cum -nodecount=30 bin/training-benchmark.test bin/training-cpu.pprof </dev/null
	$(GO) tool pprof -sample_index=alloc_space -top -nodecount=30 bin/training-benchmark.test bin/training-alloc.pprof </dev/null

.PHONY: prepare-native-backend-probe experiment-native-backends check-native-backend-probe test-native-backend-probe
prepare-native-backend-probe: | tool-state
	python3 -B experiments/native-backends/probe.py prepare --inputs "$(NATIVE_BACKEND_INPUTS)" </dev/null

experiment-native-backends: | tool-state
	python3 -B experiments/native-backends/probe.py run --inputs "$(NATIVE_BACKEND_INPUTS)" </dev/null

check-native-backend-probe: | tool-state
	python3 -B experiments/native-backends/probe.py check </dev/null

test-native-backend-probe: | tool-state
	UV_NO_CONFIG=1 uv run --offline --with pytest==8.3.5 python -B -m pytest -p no:cacheprovider -q experiments/native-backends/test_probe.py </dev/null

.PHONY: test-semantic-report test-semantic-migration
test-semantic-report: | tool-state
	python3 -B tools/semanticcoverage/test_report.py </dev/null

test-semantic-migration: runtime-bundle test-semantic-report | tool-state
	python3 -B tools/semanticcoverage/run.py run --output-root "$(LO_BUILD_ROOT)" </dev/null

ci: verify-runtime-bundle fmt lint test test-native build test-semantic-report

tool-state:
	mkdir -p "$(LO_BUILD_ROOT)" "$(HOME)" "$(TMPDIR)" "$(GOCACHE)" "$(GOMODCACHE)" "$(GOPATH)" "$(XDG_CACHE_HOME)" "$(XDG_CONFIG_HOME)" "$(XDG_DATA_HOME)" "$(XDG_STATE_HOME)" "$(CLANG_MODULE_CACHE_PATH)" "$(CCACHE_DIR)" </dev/null
