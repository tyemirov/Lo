RUNTIME_ABI_INPUTS ?= _scratch/p005-20261019/abi-inputs.json
RUNTIME_ABI_LIBRARIES ?= _scratch/p005-20261019/prepared/runtime-libraries.json

.PHONY: experiment-runtime-abi
experiment-runtime-abi: | tool-state
	python3 -B experiments/native-runtime-abi/probe.py --inputs "$(RUNTIME_ABI_INPUTS)" --runtime-libraries "$(RUNTIME_ABI_LIBRARIES)" </dev/null

.PHONY: test-runtime-abi-probe
test-runtime-abi-probe: | tool-state
	UV_NO_CONFIG=1 uv run --offline --with pytest==8.3.5 python -B -m pytest -p no:cacheprovider --basetemp "$(LO_BUILD_ROOT)/tmp/runtime-abi-pytest" -q experiments/native-runtime-abi/test_probe.py </dev/null
