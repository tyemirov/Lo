PROCESS_PROFILE_ROOT := $(abspath $(CURDIR)/_scratch/adapter-implementation-x921/native-profile)
.PHONY: process-profile-build process-profile-run
process-profile-build: | tool-state
	python3 -B experiments/process-profile/probe.py prepare </dev/null
	clang -std=c11 -Wall -Wextra -I "$(PROCESS_PROFILE_ROOT)" experiments/process-profile/fixture.c -lsandbox -o "$(PROCESS_PROFILE_ROOT)/fixture" </dev/null
	python3 -B experiments/process-profile/probe.py seal </dev/null

process-profile-run: | tool-state
	python3 -B experiments/process-profile/probe.py run "$(PROCESS_PROFILE_REVIEWED_MANIFEST)" </dev/null
