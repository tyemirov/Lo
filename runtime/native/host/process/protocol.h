#ifndef LO_PROCESS_PROTOCOL_H
#define LO_PROCESS_PROTOCOL_H
#include <stdint.h>

enum {
    FRAME = 3, STATUS = 4, PERMIT = 5, CONFIGURATION = 6,
    MAX_BODY = 1048576, MAX_ENTRY = 65536, MAX_PATH = 1024,
    MAX_ARGUMENTS = 256, MAX_ENVIRONMENT = 4096, MAX_COUNT = 16,
    MAX_CONFIGURATION = 65536, MAX_PROFILE = 32768,
    PROCESS_READY_BYTES = 38
};

int lo_process_execution_token(uint32_t words[8]);
int lo_process_execution_matches(int32_t pid, const uint32_t words[8]);

#endif
