STARTUP_NATIVE := $(CURDIR)/_scratch/adapter-implementation-x921/startup-native
STARTUP_NATIVE_HARNESS := experiments/process-startup/native_probe.py

.PHONY: process-startup-native-format process-startup-native-build process-startup-native-verify process-startup-native-run
process-startup-native-format: | tool-state
	gofmt -w experiments/process-startup/native_darwin.go experiments/process-startup/native_test.go </dev/null
	python3 -B -c "import ast,pathlib; ast.parse(pathlib.Path('$(STARTUP_NATIVE_HARNESS)').read_text())" </dev/null

process-startup-native-build: | tool-state
	python3 -B $(STARTUP_NATIVE_HARNESS) prepare </dev/null
	clang -std=c11 -Wall -Wextra -Werror -Wno-error=deprecated-declarations -I$(STARTUP_NATIVE) experiments/process-startup/native/launcher.c -lsandbox -o $(STARTUP_NATIVE)/launcher </dev/null
	clang -std=c11 -Wall -Wextra -Werror -I$(STARTUP_NATIVE) experiments/process-startup/native/target.c -o $(STARTUP_NATIVE)/target </dev/null
	$(GO) test -c -tags 'lo_process_qualification lo_process_native_qualification' -o $(STARTUP_NATIVE)/native-tests ./experiments/process-startup </dev/null
	python3 -B $(STARTUP_NATIVE_HARNESS) seal </dev/null

process-startup-native-verify: | tool-state
	python3 -B $(STARTUP_NATIVE_HARNESS) verify --reviewed-sha256 '$(REVIEWED_SHA256)' </dev/null

process-startup-native-run: | tool-state
	python3 -B $(STARTUP_NATIVE_HARNESS) run --reviewed-sha256 '$(REVIEWED_SHA256)' </dev/null
