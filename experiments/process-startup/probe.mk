.PHONY: process-startup-test process-startup-format
process-startup-format: | tool-state
	gofmt -w experiments/process-startup/startup.go experiments/process-startup/pending.go experiments/process-startup/startup_test.go </dev/null

process-startup-test: | tool-state
	$(GO) test -tags lo_process_qualification ./experiments/process-startup $(TEST_ARGS) </dev/null
