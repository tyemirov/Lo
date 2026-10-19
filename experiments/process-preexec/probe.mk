PROCESS_PREEXEC_ROOT := $(abspath $(CURDIR)/_scratch/adapter-implementation-x921/native-preexec)
.PHONY: process-preexec-build process-preexec-verify process-preexec-run
process-preexec-build: | tool-state
	python3 -B experiments/process-preexec/probe.py prepare </dev/null
	clang -std=c11 -Wall -Wextra -I "$(PROCESS_PREEXEC_ROOT)" experiments/process-preexec/target.c -o "$(PROCESS_PREEXEC_ROOT)/target" </dev/null
	clang -std=c11 -Wall -Wextra -I "$(PROCESS_PREEXEC_ROOT)" experiments/process-preexec/launcher.c -lsandbox -o "$(PROCESS_PREEXEC_ROOT)/launcher" </dev/null
	python3 -B experiments/process-preexec/probe.py seal </dev/null

process-preexec-verify: | tool-state
	python3 -B experiments/process-preexec/probe.py verify "$(PROCESS_PREEXEC_REVIEWED_MANIFEST)" </dev/null

process-preexec-run: | tool-state
	python3 -B experiments/process-preexec/probe.py run "$(PROCESS_PREEXEC_REVIEWED_MANIFEST)" </dev/null
