PROCESS_DEBUGGER_ROOT := $(abspath $(CURDIR)/_scratch/adapter-implementation-x921/debugger)
PROCESS_DEBUGGER_PREEXEC := $(abspath $(CURDIR)/_scratch/adapter-implementation-x921/native-preexec)
.PHONY: process-debugger-build process-debugger-verify process-debugger-run
process-debugger-build: | tool-state
	python3 -B experiments/process-debugger/probe.py prepare </dev/null
	clang -std=c11 -Wall -Wextra -I "$(PROCESS_DEBUGGER_PREEXEC)" experiments/process-debugger/launcher.c -lsandbox -o "$(PROCESS_DEBUGGER_ROOT)/launcher" </dev/null
	python3 -B experiments/process-debugger/probe.py seal </dev/null

process-debugger-verify: | tool-state
	python3 -B experiments/process-debugger/probe.py verify "$(PROCESS_DEBUGGER_REVIEWED_MANIFEST)" </dev/null

process-debugger-run: | tool-state
	python3 -B experiments/process-debugger/probe.py run "$(PROCESS_DEBUGGER_REVIEWED_MANIFEST)" </dev/null
