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
FILE_BENCH_WORK ?= $(LO_BUILD_ROOT)
FILE_BENCH_ARGS ?=
NATIVE_BENCH_WORK ?= $(NATIVE_QUALIFICATION_OUTPUT)/benchmark
NATIVE_BENCH_LANE ?= all
NATIVE_QUALIFICATION_TEST_ARGS ?=
NATIVE_BENCH_ARGS ?=
NATIVE_LINUX_PACKAGE ?= $(COMPILER_DISTRIBUTION_OUTPUT)/lo-linux-arm64-software
NATIVE_CAMPAIGN_WORK ?= $(NATIVE_QUALIFICATION_OUTPUT)/campaign
NATIVE_CAMPAIGN_ARGS ?=
NATIVE_BENCH_COMPILER ?= $(COMPILER_DISTRIBUTION_OUTPUT)/lo-darwin-arm64-software/bin/lo
NATIVE_BACKEND_INPUTS ?= _scratch/native-backends/inputs.json
NATIVE_LOWERING_ROOT ?= _scratch/native-lowering
NATIVE_LOWERING_TOOLS ?= $(NATIVE_LOWERING_ROOT)/tools.json
NATIVE_LOWERING_STATE ?= $(NATIVE_LOWERING_ROOT)/execution
NATIVE_RUNTIME_STATE ?= _scratch/native-runtime
NATIVE_RUNTIME_OUTPUT ?= $(NATIVE_RUNTIME_STATE)/distribution
LLVM_TOOLCHAIN ?= $(NATIVE_RUNTIME_OUTPUT)/toolchain.json
COMPILER_DISTRIBUTION_STATE ?= _scratch/compiler-distribution
COMPILER_DISTRIBUTION_OUTPUT ?= $(COMPILER_DISTRIBUTION_STATE)/packages
COMPILER_DISTRIBUTION_ACCEPTANCE ?= $(COMPILER_DISTRIBUTION_STATE)/acceptance
SEMANTIC_COMPILER ?= $(COMPILER_DISTRIBUTION_OUTPUT)/lo-darwin-arm64-software/bin/lo
SEMANTIC_TOOLCHAIN ?= $(COMPILER_DISTRIBUTION_OUTPUT)/lo-darwin-arm64-software/toolchain.json
SEMANTIC_OUTPUT ?= $(LO_BUILD_ROOT)/semantic
SEMANTIC_ARGS ?=
NATIVE_QUALIFICATION_OUTPUT ?= _scratch/native-qualification
LINUX_ACCELERATOR ?= tcg

.DEFAULT_GOAL := build

.PHONY: build fmt lint test test-integration test-conformance test-native benchmark-training benchmark-training-cli benchmark-training-profile benchmark-native-baseline benchmark-native ci

ifeq ($(shell uname -s),Darwin)
PROCESS_LAUNCHER := bin/lo-process-launcher
.PHONY: process-launcher verify-process-launcher
process-launcher: | tool-state
	python3 -B tools/processbuild/identity.py build $(PROCESS_LAUNCHER) internal/processhost/helper_identity.go runtime/native/src/process_helper_identity.rs </dev/null

verify-process-launcher: | tool-state
	python3 -B tools/processbuild/identity.py verify $(PROCESS_LAUNCHER) internal/processhost/helper_identity.go runtime/native/src/process_helper_identity.rs </dev/null
endif

build: $(if $(PROCESS_LAUNCHER),process-launcher) | tool-state
	$(GO) build -o bin/lo ./cmd/lo </dev/null

.PHONY: build-metal test-metal test-native-metal
build-metal: $(if $(PROCESS_LAUNCHER),process-launcher) | tool-state
	mkdir -p bin </dev/null
	$(GO) run ./tools/metalbuild </dev/null

test-metal: $(if $(PROCESS_LAUNCHER),process-launcher) | tool-state
	mkdir -p bin </dev/null
	$(GO) run ./tools/metalbuild -test '^TestMetalLive' -- $(TEST_ARGS) </dev/null

test-native-metal: $(if $(PROCESS_LAUNCHER),process-launcher) | tool-state
	mkdir -p bin </dev/null
	LO_LLVM_TOOLCHAIN="$(abspath $(LLVM_TOOLCHAIN))" $(GO) run ./tools/metalbuild -test '^TestNativeMetal' -- $(TEST_ARGS) </dev/null

fmt: | tool-state
	$(GO) fmt ./... </dev/null

lint: | tool-state
	$(GO) vet ./... </dev/null

test: $(if $(PROCESS_LAUNCHER),process-launcher) | tool-state
	LO_LLVM_TOOLCHAIN="$(abspath $(LLVM_TOOLCHAIN))" $(GO) test -timeout=120m ./... $(TEST_ARGS) </dev/null

test-integration: $(if $(PROCESS_LAUNCHER),process-launcher) | tool-state
	LO_LLVM_TOOLCHAIN="$(abspath $(LLVM_TOOLCHAIN))" $(GO) test -timeout=120m ./tests/integration $(TEST_ARGS) </dev/null

test-native: $(if $(PROCESS_LAUNCHER),process-launcher) | tool-state
	LO_TEST_BACKEND=llvm LO_LLVM_TOOLCHAIN="$(abspath $(LLVM_TOOLCHAIN))" LO_NATIVE_PREPARATION="$(abspath $(NATIVE_RUNTIME_STATE))" $(GO) test -timeout=120m ./tests/integration $(TEST_ARGS) </dev/null

.PHONY: prepare-native-runtime build-native-runtime build-native-runtime-metal test-native-runtime-tools
prepare-native-runtime: | tool-state
	python3 -B tools/nativeruntime/prepare.py --state "$(NATIVE_RUNTIME_STATE)" --download --platform </dev/null

build-native-runtime: $(if $(PROCESS_LAUNCHER),process-launcher) | tool-state
	python3 -B tools/nativeruntime/build.py --preparation "$(NATIVE_RUNTIME_STATE)" --output "$(NATIVE_RUNTIME_OUTPUT)" </dev/null

build-native-runtime-metal: $(if $(PROCESS_LAUNCHER),process-launcher) | tool-state
	python3 -B tools/nativeruntime/build.py --preparation "$(NATIVE_RUNTIME_STATE)" --output "$(NATIVE_RUNTIME_OUTPUT)" --metal </dev/null

test-native-runtime-tools: | tool-state
	LO_NATIVE_PREPARATION="$(abspath $(NATIVE_RUNTIME_STATE))" UV_NO_CONFIG=1 uv run --offline --with pytest==8.3.5 python -B -m pytest -p no:cacheprovider -q tools/nativeruntime </dev/null

.PHONY: prepare-compiler-distribution package-compiler-distributions test-compiler-distribution-tools test-compiler-distributions
prepare-compiler-distribution: | tool-state
	python3 -B tools/lodistribution/prepare.py --state "$(COMPILER_DISTRIBUTION_STATE)" --download </dev/null

package-compiler-distributions: build-native-runtime-metal | tool-state
	python3 -B tools/lodistribution/package.py --prepared "$(COMPILER_DISTRIBUTION_STATE)/prepared.json" --runtime "$(NATIVE_RUNTIME_OUTPUT)" --host darwin/arm64 --variant software --go "$(shell command -v $(GO))" --output "$(COMPILER_DISTRIBUTION_OUTPUT)/lo-darwin-arm64-software" </dev/null
	python3 -B tools/lodistribution/package.py --prepared "$(COMPILER_DISTRIBUTION_STATE)/prepared.json" --runtime "$(NATIVE_RUNTIME_OUTPUT)" --host linux/arm64 --variant software --go "$(shell command -v $(GO))" --output "$(COMPILER_DISTRIBUTION_OUTPUT)/lo-linux-arm64-software" </dev/null
	python3 -B tools/lodistribution/package.py --prepared "$(COMPILER_DISTRIBUTION_STATE)/prepared.json" --runtime "$(NATIVE_RUNTIME_OUTPUT)" --host darwin/arm64 --variant metal --go "$(shell command -v $(GO))" --output "$(COMPILER_DISTRIBUTION_OUTPUT)/lo-darwin-arm64-metal" </dev/null

test-compiler-distribution-tools: | tool-state
	UV_NO_CONFIG=1 uv run --offline --with pytest==8.3.5 python -B -m pytest -p no:cacheprovider -q tools/lodistribution </dev/null

test-compiler-distributions: | tool-state
	python3 -B tools/lodistribution/acceptance.py --darwin "$(COMPILER_DISTRIBUTION_OUTPUT)/lo-darwin-arm64-software" --linux "$(COMPILER_DISTRIBUTION_OUTPUT)/lo-linux-arm64-software" --metal "$(COMPILER_DISTRIBUTION_OUTPUT)/lo-darwin-arm64-metal" --preparation "$(NATIVE_RUNTIME_STATE)" --output "$(COMPILER_DISTRIBUTION_ACCEPTANCE)" </dev/null

test-conformance: $(if $(PROCESS_LAUNCHER),process-launcher) | tool-state
	$(GO) test ./tests/integration -run TestConformance $(TEST_ARGS) </dev/null

benchmark-training: | tool-state
	GOMAXPROCS=1 $(GO) test ./tests/benchmark -run '^$$' -bench BenchmarkTraining -benchmem -count=$(BENCH_COUNT) -benchtime=$(BENCH_TIME) </dev/null

benchmark-native-baseline: | tool-state
	GOMAXPROCS=1 $(GO) test ./tests/benchmark -run '^$$' -bench BenchmarkNativeBaseline -benchmem -count=$(BENCH_COUNT) -benchtime=$(BENCH_TIME) </dev/null

benchmark-native: build | tool-state
	GOMAXPROCS=1 python3 -B scripts/qualify_native.py --binary bin/lo --toolchain "$(LLVM_TOOLCHAIN)" --workdir "$(NATIVE_BENCH_WORK)" --lane "$(NATIVE_BENCH_LANE)" </dev/null

.PHONY: benchmark-file-io test-filebench
benchmark-file-io: build | tool-state
	$(GO) run ./tools/filebench -work "$(FILE_BENCH_WORK)" -compiler "$(abspath bin/lo)" -toolchain "$(abspath $(LLVM_TOOLCHAIN))" $(FILE_BENCH_ARGS) </dev/null

test-filebench: | tool-state
	$(GO) test ./tools/filebench $(TEST_ARGS) </dev/null

.PHONY: benchmark-standalone
benchmark-standalone: | tool-state
	GOMAXPROCS=1 python3 -B scripts/qualify_native.py --binary "$(NATIVE_BENCH_COMPILER)" --workdir "$(NATIVE_BENCH_WORK)" --lane "$(NATIVE_BENCH_LANE)" $(NATIVE_BENCH_ARGS) </dev/null

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
	LO_TEST_COMPILER="$(abspath $(SEMANTIC_COMPILER))" LO_LLVM_TOOLCHAIN="$(abspath $(SEMANTIC_TOOLCHAIN))" python3 -B tools/semanticcoverage/test_report.py </dev/null

.PHONY: test-native-qualification-tools
test-native-qualification-tools: | tool-state
	$(GO) test ./tools/loqualification/performance-runner </dev/null
	$(GO) vet ./tools/loqualification/performance-runner </dev/null
	LO_NATIVE_PREPARATION="$(abspath $(NATIVE_RUNTIME_STATE))" LO_LLVM_TOOLCHAIN="$(abspath $(LLVM_TOOLCHAIN))" UV_NO_CONFIG=1 uv run --offline --with pytest==8.3.5 python -B -m pytest -p no:cacheprovider --basetemp "$(LO_BUILD_ROOT)/qualification-tests" -q tools/loqualification tools/nativebench $(NATIVE_QUALIFICATION_TEST_ARGS) </dev/null

test-semantic-migration: $(if $(PROCESS_LAUNCHER),process-launcher) test-semantic-report | tool-state
	python3 -B tools/semanticcoverage/run.py run --output-root "$(SEMANTIC_OUTPUT)" --compiler "$(SEMANTIC_COMPILER)" --toolchain "$(SEMANTIC_TOOLCHAIN)" $(SEMANTIC_ARGS) </dev/null

.PHONY: test-native-linux benchmark-native-linux
test-native-linux: | tool-state
	python3 -B tools/loqualification/suite.py --preparation "$(NATIVE_RUNTIME_STATE)" --package "$(COMPILER_DISTRIBUTION_OUTPUT)/lo-linux-arm64-software" --matrix testdata/semantic-migration/matrix.json --work "$(NATIVE_QUALIFICATION_OUTPUT)/linux" --go "$(shell command -v $(GO))" --accelerator "$(LINUX_ACCELERATOR)" </dev/null

benchmark-native-linux: | tool-state
	python3 -B tools/loqualification/performance.py --preparation "$(NATIVE_RUNTIME_STATE)" --package "$(NATIVE_LINUX_PACKAGE)" --frontend "$(SEMANTIC_COMPILER)" --work "$(NATIVE_BENCH_WORK)" --go "$(shell command -v $(GO))" --accelerator "$(LINUX_ACCELERATOR)" --lane "$(NATIVE_BENCH_LANE)" $(NATIVE_BENCH_ARGS) </dev/null

.PHONY: prepare-native-lowering test-native-lowering test-native-lowering-tools
prepare-native-lowering: | tool-state
	python3 -B tools/nativelowering/prepare.py --state "$(NATIVE_LOWERING_ROOT)" --download </dev/null

test-native-lowering: build | tool-state
	python3 -B tools/nativelowering/run.py --lo bin/lo --tools "$(NATIVE_LOWERING_TOOLS)" --state "$(NATIVE_LOWERING_STATE)" </dev/null

test-native-lowering-tools: | tool-state
	UV_NO_CONFIG=1 uv run --offline --with pytest==8.3.5 python -B -m pytest -p no:cacheprovider -q tools/nativelowering/test_cli.py </dev/null

ci: fmt lint test test-native build test-semantic-report test-native-qualification-tools

tool-state:
	mkdir -p "$(LO_BUILD_ROOT)" "$(HOME)" "$(TMPDIR)" "$(GOCACHE)" "$(GOMODCACHE)" "$(GOPATH)" "$(XDG_CACHE_HOME)" "$(XDG_CONFIG_HOME)" "$(XDG_DATA_HOME)" "$(XDG_STATE_HOME)" "$(CLANG_MODULE_CACHE_PATH)" "$(CCACHE_DIR)" </dev/null

.PHONY: test-native-campaign
test-native-campaign: | tool-state
	UV_NO_CONFIG=1 uv run --offline --with pytest==8.3.5 python -B -m pytest -p no:cacheprovider --basetemp "$(LO_BUILD_ROOT)/campaign-tests" -q tools/nativebench/test_campaign.py </dev/null

.PHONY: benchmark-native-campaign
benchmark-native-campaign: | tool-state
	python3 -B tools/nativebench/campaign.py --work "$(NATIVE_CAMPAIGN_WORK)" --darwin-package "$(abspath $(dir $(NATIVE_BENCH_COMPILER))/..)" --linux-package "$(NATIVE_LINUX_PACKAGE)" --preparation "$(NATIVE_RUNTIME_STATE)" --frontend "$(SEMANTIC_COMPILER)" --accelerator "$(LINUX_ACCELERATOR)" $(NATIVE_CAMPAIGN_ARGS) </dev/null
