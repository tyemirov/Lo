.PHONY: process-loader-seal process-loader-verify process-loader-run
process-loader-seal: | tool-state
	python3 -B experiments/process-loader/probe.py prepare </dev/null
	python3 -B experiments/process-loader/probe.py seal </dev/null

process-loader-verify: | tool-state
	python3 -B experiments/process-loader/probe.py verify "$(PROCESS_LOADER_REVIEWED_MANIFEST)" </dev/null

process-loader-run: | tool-state
	python3 -B experiments/process-loader/probe.py run "$(PROCESS_LOADER_REVIEWED_MANIFEST)" </dev/null
