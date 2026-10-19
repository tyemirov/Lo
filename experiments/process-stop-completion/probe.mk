.PHONY: process-stop-completion-test process-stop-completion-build process-stop-completion-verify process-stop-completion-run
process-stop-completion-test: | tool-state
	uv run --offline --with pytest==8.3.5 python -B -m pytest -q experiments/process-stop-completion/test_probe.py </dev/null

process-stop-completion-build: | tool-state
	python3 -B experiments/process-stop-completion/probe.py prepare </dev/null
	python3 -B experiments/process-stop-completion/probe.py seal </dev/null

process-stop-completion-verify: | tool-state
	python3 -B experiments/process-stop-completion/probe.py verify "$(PROCESS_STOP_COMPLETION_REVIEWED_MANIFEST)" </dev/null

process-stop-completion-run: | tool-state
	python3 -B experiments/process-stop-completion/probe.py run "$(PROCESS_STOP_COMPLETION_REVIEWED_MANIFEST)" </dev/null
