GO := go
export GOTOOLCHAIN := go1.27.1
TEST_ARGS ?=
BENCH_COUNT ?= 10
BENCH_TIME ?= 300ms
NATIVE_BENCH_WORK ?= _scratch/f001-native/benchmark
NATIVE_BENCH_LANE ?= all

.DEFAULT_GOAL := build

.PHONY: build fmt lint test test-integration test-conformance test-native test-native-race runtime-bundle verify-runtime-bundle benchmark-training benchmark-training-cli benchmark-training-profile benchmark-native-baseline benchmark-native ci

runtime-bundle:
	$(GO) run ./tools/runtimebundle

verify-runtime-bundle:
	$(GO) run ./tools/runtimebundle --check

build: runtime-bundle
	$(GO) build -o bin/lo ./cmd/lo

.PHONY: build-metal test-metal test-native-metal
build-metal: runtime-bundle
	mkdir -p bin
	$(GO) run ./tools/metalbuild

test-metal: runtime-bundle
	mkdir -p bin
	$(GO) run ./tools/metalbuild -test '^TestMetalLive' -- $(TEST_ARGS)

test-native-metal: runtime-bundle
	mkdir -p bin
	$(GO) run ./tools/metalbuild -test '^TestNativeMetal' -- $(TEST_ARGS)

fmt:
	$(GO) fmt ./...

lint:
	$(GO) vet ./...

test: runtime-bundle
	$(GO) test ./... $(TEST_ARGS)

test-integration: runtime-bundle
	$(GO) test ./tests/integration $(TEST_ARGS)

test-native: runtime-bundle
	LO_TEST_BACKEND=native $(GO) test ./tests/integration $(TEST_ARGS)

test-native-race: runtime-bundle
	LO_TEST_BACKEND=native LO_TEST_NATIVE_RACE=1 GORACE=atexit_sleep_ms=0 $(GO) test -race ./tests/integration $(TEST_ARGS)

test-conformance: runtime-bundle
	$(GO) test ./tests/integration -run TestConformance $(TEST_ARGS)

benchmark-training:
	GOMAXPROCS=1 $(GO) test ./tests/benchmark -run '^$$' -bench BenchmarkTraining -benchmem -count=$(BENCH_COUNT) -benchtime=$(BENCH_TIME)

benchmark-native-baseline:
	GOMAXPROCS=1 $(GO) test ./tests/benchmark -run '^$$' -bench BenchmarkNativeBaseline -benchmem -count=$(BENCH_COUNT) -benchtime=$(BENCH_TIME)

benchmark-native: build
	GOMAXPROCS=1 python3 scripts/qualify_native.py --binary bin/lo --workdir $(NATIVE_BENCH_WORK) --samples $(BENCH_COUNT) --lane $(NATIVE_BENCH_LANE)

benchmark-training-cli: build
	GOMAXPROCS=1 python3 scripts/benchmark_training.py --binary bin/lo --samples $(BENCH_COUNT)

benchmark-training-profile: build
	GOMAXPROCS=1 $(GO) test ./tests/benchmark -run '^$$' -bench 'BenchmarkTraining/(MVP200|Dense20)/Execute$$' -benchtime=3s -cpuprofile bin/training-cpu.pprof -memprofile bin/training-alloc.pprof -o bin/training-benchmark.test
	$(GO) tool pprof -top -nodecount=30 bin/training-benchmark.test bin/training-cpu.pprof
	$(GO) tool pprof -top -cum -nodecount=30 bin/training-benchmark.test bin/training-cpu.pprof
	$(GO) tool pprof -sample_index=alloc_space -top -nodecount=30 bin/training-benchmark.test bin/training-alloc.pprof

.PHONY: prepare-native-backend-probe experiment-native-backends check-native-backend-probe test-native-backend-probe
prepare-native-backend-probe:
	CI=1 python3 experiments/native-backends/probe.py bootstrap </dev/null

experiment-native-backends:
	CI=1 python3 experiments/native-backends/probe.py run </dev/null

check-native-backend-probe:
	CI=1 python3 experiments/native-backends/probe.py check </dev/null

test-native-backend-probe:
	CI=1 uv run --with pytest==8.3.5 python -m pytest -q experiments/native-backends/test_probe.py </dev/null

ci: verify-runtime-bundle fmt lint test test-native build
