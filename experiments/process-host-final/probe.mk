HOST_NATIVE := $(CURDIR)/_scratch/adapter-implementation-x921/host-final
.PHONY: process-host-final-format process-host-final-fixtures process-host-final-compile process-host-final-build process-host-final-verify process-host-final-run
process-host-final-format: | tool-state
	gofmt -w experiments/process-host-final/native_test.go </dev/null
	python3 -B -c "import ast,pathlib; ast.parse(pathlib.Path('experiments/process-host-final/probe.py').read_text())" </dev/null

process-host-final-fixtures: | tool-state
	$(GO) test -tags lo_process_native_host_qualification ./experiments/process-host-final -run '^TestNativeProcessHostFixturePrograms$$' -count=1 </dev/null

process-host-final-compile: | tool-state
	$(GO) test -c -tags lo_process_native_host_qualification -o _scratch/adapter-implementation-x921/process-host-final-tests ./experiments/process-host-final </dev/null

process-host-final-build: process-launcher | tool-state
	python3 -B experiments/process-host-final/probe.py prepare </dev/null
	clang -std=c11 -Wall -Wextra -Werror -I$(HOST_NATIVE) experiments/process-host-final/native/target.c -o $(HOST_NATIVE)/target </dev/null
	$(GO) test -c -tags lo_process_native_host_qualification -o $(HOST_NATIVE)/native-tests ./experiments/process-host-final </dev/null
	python3 -B experiments/process-host-final/probe.py seal </dev/null

process-host-final-verify: | tool-state
	python3 -B experiments/process-host-final/probe.py verify --reviewed-sha256 '$(REVIEWED_SHA256)' </dev/null

process-host-final-run: | tool-state
	python3 -B experiments/process-host-final/probe.py run --reviewed-sha256 '$(REVIEWED_SHA256)' </dev/null
