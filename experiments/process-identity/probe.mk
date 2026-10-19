PROCESS_IDENTITY_ROOT := $(abspath $(CURDIR)/_scratch/adapter-implementation-x921/native-identity)
.PHONY: process-identity-build process-identity-darwin-build
process-identity-build: | tool-state
	mkdir -p "$(PROCESS_IDENTITY_ROOT)" </dev/null
	clang -std=c11 -Wall -Wextra -Werror=implicit-function-declaration experiments/process-identity/descriptor.c -o "$(PROCESS_IDENTITY_ROOT)/descriptor" </dev/null

process-identity-darwin-build: | tool-state
	mkdir -p "$(PROCESS_IDENTITY_ROOT)" </dev/null
	clang -D_DARWIN_C_SOURCE -Wall -Wextra -Werror=implicit-function-declaration experiments/process-identity/descriptor.c -o "$(PROCESS_IDENTITY_ROOT)/descriptor-darwin" </dev/null
