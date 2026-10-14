GO := go
export GOTOOLCHAIN := go1.27.1
TEST_ARGS ?=
BENCH_COUNT ?= 10
BENCH_TIME ?= 300ms

.PHONY: build fmt lint test test-integration test-conformance benchmark-training benchmark-training-cli benchmark-training-profile ci

build:
	$(GO) build -o bin/lo ./cmd/lo

fmt:
	$(GO) fmt ./...

lint:
	$(GO) vet ./...

test:
	$(GO) test ./... $(TEST_ARGS)

test-integration:
	$(GO) test ./tests/integration $(TEST_ARGS)

test-conformance:
	$(GO) test ./tests/integration -run TestConformance $(TEST_ARGS)

benchmark-training:
	GOMAXPROCS=1 $(GO) test ./tests/benchmark -run '^$$' -bench BenchmarkTraining -benchmem -count=$(BENCH_COUNT) -benchtime=$(BENCH_TIME)

benchmark-training-cli: build
	GOMAXPROCS=1 python3 scripts/benchmark_training.py --binary bin/lo --samples $(BENCH_COUNT)

benchmark-training-profile: build
	GOMAXPROCS=1 $(GO) test ./tests/benchmark -run '^$$' -bench 'BenchmarkTraining/(MVP200|Dense20)/Execute$$' -benchtime=3s -cpuprofile bin/training-cpu.pprof -memprofile bin/training-alloc.pprof -o bin/training-benchmark.test
	$(GO) tool pprof -top -nodecount=30 bin/training-benchmark.test bin/training-cpu.pprof
	$(GO) tool pprof -top -cum -nodecount=30 bin/training-benchmark.test bin/training-cpu.pprof
	$(GO) tool pprof -sample_index=alloc_space -top -nodecount=30 bin/training-benchmark.test bin/training-alloc.pprof

ci: fmt lint test build
