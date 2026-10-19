.PHONY: process-resources-controller-test process-resources-controller-build process-resources-controller-verify process-resources-controller-run
process-resources-controller-test: | tool-state
	uv run --offline --with pytest==8.3.5 python -B -m pytest -q experiments/process-resources-controller/test_probe.py </dev/null

process-resources-controller-build: | tool-state
	python3 -B experiments/process-resources-controller/probe.py prepare </dev/null
	python3 -B experiments/process-resources-controller/probe.py seal </dev/null

process-resources-controller-verify: | tool-state
	python3 -B experiments/process-resources-controller/probe.py verify "$(PROCESS_RESOURCES_CONTROLLER_REVIEWED_MANIFEST)" </dev/null

process-resources-controller-run: | tool-state
	python3 -B experiments/process-resources-controller/probe.py run "$(PROCESS_RESOURCES_CONTROLLER_REVIEWED_MANIFEST)" </dev/null
