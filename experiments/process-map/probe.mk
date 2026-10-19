PROCESS_MAP_ROOT := $(abspath $(CURDIR)/_scratch/adapter-implementation-x921/native-map)
.PHONY: process-map-build process-map-verify process-map-run
process-map-build: | tool-state
	python3 -B experiments/process-map/probe.py prepare </dev/null
	clang -std=c11 -Wall -Wextra experiments/process-map/image.c -o "$(PROCESS_MAP_ROOT)/mapping-fixture" </dev/null
	clang -std=c11 -Wall -Wextra -I "$(PROCESS_MAP_ROOT)" experiments/process-map/fixture.c -lsandbox -o "$(PROCESS_MAP_ROOT)/fixture" </dev/null
	python3 -B experiments/process-map/probe.py seal </dev/null

process-map-verify: | tool-state
	python3 -B experiments/process-map/probe.py verify "$(PROCESS_MAP_REVIEWED_MANIFEST)" </dev/null

process-map-run: | tool-state
	python3 -B experiments/process-map/probe.py run "$(PROCESS_MAP_REVIEWED_MANIFEST)" </dev/null
