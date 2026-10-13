GO := go
export GOTOOLCHAIN := go1.27.1
TEST_ARGS ?=

.PHONY: build fmt lint test test-integration test-conformance ci

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

ci: fmt lint test build
