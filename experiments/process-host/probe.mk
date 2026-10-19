HOST_NATIVE := $(CURDIR)/_scratch/adapter-implementation-x921/host-native
.PHONY: process-host-native-format process-host-native-fixtures process-host-native-compile process-host-native-build process-host-native-verify process-host-native-run
process-host-native-format: | tool-state
	gofmt -w experiments/process-host/native_test.go </dev/null
	python3 -B -c "import ast,pathlib; ast.parse(pathlib.Path('experiments/process-host/probe.py').read_text())" </dev/null

process-host-native-fixtures: | tool-state
	$(GO) test -tags lo_process_native_host_qualification ./experiments/process-host -run '^TestNativeProcessHostFixturePrograms$$' -count=1 </dev/null

process-host-native-compile: | tool-state
	$(GO) test -c -tags lo_process_native_host_qualification -o _scratch/adapter-implementation-x921/process-host-native-tests ./experiments/process-host </dev/null

process-host-native-build: process-launcher | tool-state
	python3 -B experiments/process-host/probe.py prepare </dev/null
	clang -std=c11 -Wall -Wextra -Werror -I$(HOST_NATIVE) experiments/process-host/native/target.c -o $(HOST_NATIVE)/target </dev/null
	$(GO) test -c -tags lo_process_native_host_qualification -o $(HOST_NATIVE)/native-tests ./experiments/process-host </dev/null
	python3 -B experiments/process-host/probe.py seal </dev/null

process-host-native-verify: | tool-state
	python3 -B experiments/process-host/probe.py verify --reviewed-sha256 '$(REVIEWED_SHA256)' </dev/null

process-host-native-run: | tool-state
	python3 -B experiments/process-host/probe.py run --reviewed-sha256 '$(REVIEWED_SHA256)' </dev/null
