IPC_LOCAL_ROOT := $(CURDIR)/_scratch/adapter-implementation-x921/ipc-local-native
IPC_LOCAL_HARNESS := experiments/process-ipc-local/probe.py
IPC_LOCAL_COMMON := experiments/process-ipc-local/normalization.c experiments/process-ipc-local/inventory.c
.PHONY: process-ipc-local-test process-ipc-local-contract process-ipc-local-build process-ipc-local-verify process-ipc-local-run
process-ipc-local-test: | tool-state
	uv run --offline --with pytest==8.3.5 python -B -m pytest -q experiments/process-ipc-local/test_probe.py </dev/null

# Compile/link only. Never execute this contract image.
process-ipc-local-contract: | tool-state
	clang -std=c11 -Wall -Wextra -Werror -Wno-error=deprecated-declarations experiments/process-ipc-local/contract.c experiments/process-ipc-local/fixture.c $(IPC_LOCAL_COMMON) -o _scratch/adapter-implementation-x921/ipc-local-contract </dev/null

process-ipc-local-build: | tool-state
	python3 -B $(IPC_LOCAL_HARNESS) prepare </dev/null
	clang -std=c11 -Wall -Wextra -Werror -I$(IPC_LOCAL_ROOT) experiments/process-ipc-local/controller.c -o $(IPC_LOCAL_ROOT)/controller </dev/null
	clang -std=c11 -Wall -Wextra -Werror -Wno-error=deprecated-declarations -I$(IPC_LOCAL_ROOT) experiments/process-ipc-local/launcher.c experiments/process-ipc-local/fixture.c $(IPC_LOCAL_COMMON) -lsandbox -o $(IPC_LOCAL_ROOT)/launcher </dev/null
	clang -std=c11 -Wall -Wextra -Werror -Wno-error=deprecated-declarations -I$(IPC_LOCAL_ROOT) experiments/process-ipc-local/target.c $(IPC_LOCAL_COMMON) -o $(IPC_LOCAL_ROOT)/target </dev/null
	python3 -B $(IPC_LOCAL_HARNESS) seal </dev/null

process-ipc-local-verify: | tool-state
	python3 -B $(IPC_LOCAL_HARNESS) verify '$(IPC_LOCAL_REVIEWED_MANIFEST)' </dev/null

process-ipc-local-run: | tool-state
	python3 -B $(IPC_LOCAL_HARNESS) run '$(IPC_LOCAL_REVIEWED_MANIFEST)' </dev/null
