PROCESS_RESOURCES_TAIL_ROOT := $(abspath $(CURDIR)/_scratch/adapter-implementation-x921/native-resources-tail)
.PHONY: process-resources-tail-test process-resources-tail-build process-resources-tail-verify process-resources-tail-run
process-resources-tail-test: | tool-state
	uv run --offline --with pytest==8.3.5 python -m pytest -q experiments/process-resources-tail/test_probe.py </dev/null

process-resources-tail-build: | tool-state
	python3 -B experiments/process-resources-tail/probe.py prepare </dev/null
	python3 -B experiments/process-resources-tail/probe.py seal </dev/null

process-resources-tail-verify: | tool-state
	python3 -B experiments/process-resources-tail/probe.py verify "$(PROCESS_RESOURCES_TAIL_REVIEWED_MANIFEST)" </dev/null

process-resources-tail-run: | tool-state
	python3 -B experiments/process-resources-tail/probe.py run "$(PROCESS_RESOURCES_TAIL_REVIEWED_MANIFEST)" </dev/null
