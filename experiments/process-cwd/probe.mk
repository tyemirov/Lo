PROCESS_CWD_ROOT := $(abspath $(CURDIR)/_scratch/adapter-implementation-x921/native-cwd)
.PHONY: process-cwd-build process-cwd-verify process-cwd-run
process-cwd-build: | tool-state
	python3 -B experiments/process-cwd/probe.py prepare </dev/null
	clang -std=c11 -Wall -Wextra -I "$(PROCESS_CWD_ROOT)" experiments/process-cwd/fixture.c -lsandbox -o "$(PROCESS_CWD_ROOT)/fixture" </dev/null
	python3 -B experiments/process-cwd/probe.py seal </dev/null

process-cwd-verify: | tool-state
	python3 -B experiments/process-cwd/probe.py verify "$(PROCESS_CWD_REVIEWED_MANIFEST)" </dev/null

process-cwd-run: | tool-state
	python3 -B experiments/process-cwd/probe.py run "$(PROCESS_CWD_REVIEWED_MANIFEST)" </dev/null
