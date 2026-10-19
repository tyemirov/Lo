PROCESS_IPC_ROOT := $(CURDIR)/_scratch/adapter-implementation-x921/ipc-native
PROCESS_IPC_HARNESS := experiments/process-ipc/probe.py
.PHONY: process-ipc-test process-ipc-contract process-ipc-build process-ipc-verify process-ipc-run
process-ipc-test: | tool-state
	uv run --offline --with pytest==8.3.5 python -B -m pytest -q experiments/process-ipc/test_probe.py </dev/null

# Compile/link only; never execute this contract image.
process-ipc-contract: | tool-state
	clang -std=c11 -Wall -Wextra -Werror -Wno-error=deprecated-declarations experiments/process-ipc/contract.c experiments/process-ipc/normalization.c -o _scratch/adapter-implementation-x921/ipc-normalization-contract </dev/null

process-ipc-build: | tool-state
	python3 -B $(PROCESS_IPC_HARNESS) prepare </dev/null
	clang -std=c11 -Wall -Wextra -Werror -Wno-error=deprecated-declarations -I$(PROCESS_IPC_ROOT) experiments/process-ipc/controller.c experiments/process-ipc/normalization.c -o $(PROCESS_IPC_ROOT)/controller </dev/null
	clang -std=c11 -Wall -Wextra -Werror -Wno-error=deprecated-declarations -I$(PROCESS_IPC_ROOT) experiments/process-ipc/launcher.c experiments/process-ipc/normalization.c -lsandbox -o $(PROCESS_IPC_ROOT)/launcher </dev/null
	clang -std=c11 -Wall -Wextra -Werror -Wno-error=deprecated-declarations -I$(PROCESS_IPC_ROOT) experiments/process-ipc/target.c experiments/process-ipc/normalization.c -o $(PROCESS_IPC_ROOT)/target </dev/null
	python3 -B $(PROCESS_IPC_HARNESS) seal </dev/null

process-ipc-verify: | tool-state
	python3 -B $(PROCESS_IPC_HARNESS) verify '$(PROCESS_IPC_REVIEWED_MANIFEST)' </dev/null

process-ipc-run: | tool-state
	python3 -B $(PROCESS_IPC_HARNESS) run '$(PROCESS_IPC_REVIEWED_MANIFEST)' </dev/null
