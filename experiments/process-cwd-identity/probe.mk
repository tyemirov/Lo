PROCESS_CWD_IDENTITY_ROOT := $(abspath $(CURDIR)/_scratch/adapter-implementation-x921/native-cwd-identity)
.PHONY: process-cwd-identity-build process-cwd-identity-verify process-cwd-identity-run
process-cwd-identity-build: | tool-state
	python3 -B experiments/process-cwd-identity/probe.py prepare </dev/null
	clang -std=c11 -Wall -Wextra -I "$(PROCESS_CWD_IDENTITY_ROOT)" experiments/process-preexec/launcher.c -lsandbox -o "$(PROCESS_CWD_IDENTITY_ROOT)/launcher" </dev/null
	clang -std=c11 -Wall -Wextra -I "$(PROCESS_CWD_IDENTITY_ROOT)" experiments/process-cwd-identity/target.c -o "$(PROCESS_CWD_IDENTITY_ROOT)/target" </dev/null
	python3 -B experiments/process-cwd-identity/probe.py seal </dev/null

process-cwd-identity-verify: | tool-state
	python3 -B experiments/process-cwd-identity/probe.py verify "$(PROCESS_CWD_IDENTITY_REVIEWED_MANIFEST)" </dev/null

process-cwd-identity-run: | tool-state
	python3 -B experiments/process-cwd-identity/probe.py run "$(PROCESS_CWD_IDENTITY_REVIEWED_MANIFEST)" </dev/null
