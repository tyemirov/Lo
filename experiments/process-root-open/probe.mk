PROCESS_ROOT_OPEN_ROOT := $(abspath $(CURDIR)/_scratch/adapter-implementation-x921/native-root-open)
.PHONY: process-root-open-build process-root-open-verify process-root-open-run
process-root-open-build: | tool-state
	python3 -B experiments/process-root-open/probe.py prepare </dev/null
	clang -std=c11 -Wall -Wextra -I "$(PROCESS_ROOT_OPEN_ROOT)" experiments/process-root-open/fixture.c -lsandbox -o "$(PROCESS_ROOT_OPEN_ROOT)/fixture" </dev/null
	python3 -B experiments/process-root-open/probe.py seal </dev/null

process-root-open-verify: | tool-state
	python3 -B experiments/process-root-open/probe.py verify "$(PROCESS_ROOT_OPEN_REVIEWED_MANIFEST)" </dev/null

process-root-open-run: | tool-state
	python3 -B experiments/process-root-open/probe.py run "$(PROCESS_ROOT_OPEN_REVIEWED_MANIFEST)" </dev/null
