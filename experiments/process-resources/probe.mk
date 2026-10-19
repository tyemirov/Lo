PROCESS_RESOURCES_ROOT := $(abspath $(CURDIR)/_scratch/adapter-implementation-x921/native-resources)
.PHONY: process-resources-test process-resources-build process-resources-verify process-resources-run
process-resources-test: | tool-state
	uv run --offline --with pytest==8.3.5 python -m pytest -q experiments/process-resources/test_probe.py </dev/null

process-resources-build: | tool-state
	python3 -B experiments/process-resources/probe.py prepare </dev/null
	clang -std=c11 -Wall -Wextra -Werror -Wno-deprecated-declarations -I "$(PROCESS_RESOURCES_ROOT)" experiments/process-environment/launcher.c -lsandbox -o "$(PROCESS_RESOURCES_ROOT)/launcher" </dev/null
	clang -std=c11 -Wall -Wextra -Werror -Wno-deprecated-declarations -I "$(PROCESS_RESOURCES_ROOT)" experiments/process-resources/target.c -o "$(PROCESS_RESOURCES_ROOT)/target" </dev/null
	python3 -B experiments/process-resources/probe.py seal </dev/null

process-resources-verify: | tool-state
	python3 -B experiments/process-resources/probe.py verify "$(PROCESS_RESOURCES_REVIEWED_MANIFEST)" </dev/null

process-resources-run: | tool-state
	python3 -B experiments/process-resources/probe.py run "$(PROCESS_RESOURCES_REVIEWED_MANIFEST)" </dev/null
