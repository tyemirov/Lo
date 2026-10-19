PROCESS_PUBLIC_PHASE ?= gate-red
PROCESS_PUBLIC_ROOT := $(CURDIR)/_scratch/adapter-implementation-x921/public-native-$(PROCESS_PUBLIC_PHASE)
PROCESS_PUBLIC_HARNESS := experiments/process-public/probe.py
.PHONY: process-public-test process-public-compile process-public-build process-public-verify process-public-run
.PHONY: process-public-format
process-public-format: | tool-state
	gofmt -w experiments/process-public/public_test.go </dev/null
	python3 -B -c "import ast,pathlib; ast.parse(pathlib.Path('experiments/process-public/probe.py').read_text())" </dev/null

process-public-test: | tool-state
	uv run --offline --with pytest==8.3.5 python -B -m pytest -q experiments/process-public/test_probe.py </dev/null

process-public-compile: | tool-state
	$(GO) test -c -tags lo_process_public_qualification -o _scratch/adapter-implementation-x921/process-public-tests ./experiments/process-public </dev/null

# Root regenerates the runtime bundle before this target. This verifies the
# existing product helper pin, and never updates product source or that pin.
process-public-build: verify-process-launcher | tool-state
	python3 -B $(PROCESS_PUBLIC_HARNESS) prepare $(PROCESS_PUBLIC_PHASE) </dev/null
	$(MAKE) -j1 -f Makefile -f experiments/process-public/probe.mk process-public-build-prepared PROCESS_PUBLIC_PHASE=$(PROCESS_PUBLIC_PHASE) </dev/null

.PHONY: process-public-build-prepared
process-public-build-prepared: verify-process-launcher | tool-state
	python3 -B $(PROCESS_PUBLIC_HARNESS) snapshot-helper $(PROCESS_PUBLIC_PHASE) </dev/null
	python3 -B $(PROCESS_PUBLIC_HARNESS) helper-identity $(PROCESS_PUBLIC_PHASE) </dev/null
	clang -std=c11 -Wall -Wextra -Werror -I$(PROCESS_PUBLIC_ROOT)/fixture experiments/process-public/native/target.c -o $(PROCESS_PUBLIC_ROOT)/fixture/target </dev/null
	$(GO) build -o $(PROCESS_PUBLIC_ROOT)/lo ./cmd/lo </dev/null
	python3 -B $(PROCESS_PUBLIC_HARNESS) sources $(PROCESS_PUBLIC_PHASE) </dev/null
	$(GO) test -c -tags lo_process_public_qualification -o $(PROCESS_PUBLIC_ROOT)/public-tests ./experiments/process-public </dev/null
	python3 -B $(PROCESS_PUBLIC_HARNESS) seal $(PROCESS_PUBLIC_PHASE) </dev/null

process-public-verify: | tool-state
	python3 -B $(PROCESS_PUBLIC_HARNESS) verify $(PROCESS_PUBLIC_PHASE) '$(PROCESS_PUBLIC_REVIEWED_MANIFEST)' </dev/null

process-public-run: | tool-state
	python3 -B $(PROCESS_PUBLIC_HARNESS) run $(PROCESS_PUBLIC_PHASE) '$(PROCESS_PUBLIC_REVIEWED_MANIFEST)' </dev/null
