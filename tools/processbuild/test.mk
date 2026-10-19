.PHONY: test-process-build
test-process-build: | tool-state
	uv run --offline --with pytest==8.3.5 python -B -m pytest -q tools/processbuild/test_identity.py </dev/null
