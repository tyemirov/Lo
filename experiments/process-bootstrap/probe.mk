PROCESS_BOOTSTRAP_ROOT := $(abspath $(CURDIR)/_scratch/adapter-implementation-x921/native-root-bootstrap)
.PHONY: process-bootstrap-build process-bootstrap-verify process-bootstrap-run
process-bootstrap-build: | tool-state
	python3 -B experiments/process-bootstrap/probe.py prepare </dev/null
	clang -std=c11 -Wall -Wextra -I "$(PROCESS_BOOTSTRAP_ROOT)" experiments/process-preexec/launcher.c -lsandbox -o "$(PROCESS_BOOTSTRAP_ROOT)/launcher" </dev/null
	python3 -B experiments/process-bootstrap/probe.py seal </dev/null

process-bootstrap-verify: | tool-state
	python3 -B experiments/process-bootstrap/probe.py verify "$(PROCESS_BOOTSTRAP_REVIEWED_MANIFEST)" </dev/null

process-bootstrap-run: | tool-state
	python3 -B experiments/process-bootstrap/probe.py run "$(PROCESS_BOOTSTRAP_REVIEWED_MANIFEST)" </dev/null
