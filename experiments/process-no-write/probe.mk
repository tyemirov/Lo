PROCESS_NO_WRITE_ROOT := $(abspath $(CURDIR)/_scratch/adapter-implementation-x921/native-no-write)
.PHONY: process-no-write-test process-no-write-build process-no-write-verify process-no-write-run
process-no-write-test: | tool-state
	uv run --offline --with pytest==8.3.5 python -B -m pytest -q experiments/process-no-write/test_probe.py </dev/null

process-no-write-build: | tool-state
	python3 -B experiments/process-no-write/probe.py prepare </dev/null
	clang -std=c11 -Wall -Wextra -Werror -Wno-deprecated-declarations -I "$(PROCESS_NO_WRITE_ROOT)" experiments/process-environment/launcher.c -lsandbox -o "$(PROCESS_NO_WRITE_ROOT)/launcher" </dev/null
	clang -std=c11 -Wall -Wextra -Werror -I "$(PROCESS_NO_WRITE_ROOT)" experiments/process-no-write/target.c -o "$(PROCESS_NO_WRITE_ROOT)/target" </dev/null
	python3 -B experiments/process-no-write/probe.py seal </dev/null

process-no-write-verify: | tool-state
	python3 -B experiments/process-no-write/probe.py verify "$(PROCESS_NO_WRITE_REVIEWED_MANIFEST)" </dev/null

process-no-write-run: | tool-state
	python3 -B experiments/process-no-write/probe.py run "$(PROCESS_NO_WRITE_REVIEWED_MANIFEST)" </dev/null
