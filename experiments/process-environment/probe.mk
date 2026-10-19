PROCESS_ENVIRONMENT_ROOT := $(abspath $(CURDIR)/_scratch/adapter-implementation-x921/native-environment)
.PHONY: process-environment-test process-environment-build process-environment-verify process-environment-run
process-environment-test: | tool-state
	uv run --offline --with pytest==8.3.5 python -m pytest -q experiments/process-environment/test_probe.py </dev/null

process-environment-build: | tool-state
	python3 -B experiments/process-environment/probe.py prepare </dev/null
	clang -std=c11 -Wall -Wextra -Werror -Wno-deprecated-declarations -I "$(PROCESS_ENVIRONMENT_ROOT)" experiments/process-environment/launcher.c -lsandbox -o "$(PROCESS_ENVIRONMENT_ROOT)/launcher" </dev/null
	clang -std=c11 -Wall -Wextra -Werror -I "$(PROCESS_ENVIRONMENT_ROOT)" experiments/process-environment/target.c -o "$(PROCESS_ENVIRONMENT_ROOT)/target" </dev/null
	clang -std=c11 -Wall -Wextra -Werror -dynamiclib -I "$(PROCESS_ENVIRONMENT_ROOT)" experiments/process-environment/constructor.c -o "$(PROCESS_ENVIRONMENT_ROOT)/constructor.dylib" </dev/null
	python3 -B experiments/process-environment/probe.py seal </dev/null

process-environment-verify: | tool-state
	python3 -B experiments/process-environment/probe.py verify "$(PROCESS_ENVIRONMENT_REVIEWED_MANIFEST)" </dev/null

process-environment-run: | tool-state
	python3 -B experiments/process-environment/probe.py run "$(PROCESS_ENVIRONMENT_REVIEWED_MANIFEST)" </dev/null
