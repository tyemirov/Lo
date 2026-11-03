#ifndef LO_CONSOLE_H
#define LO_CONSOLE_H
#include "lo_runtime.h"
enum { LO_CONSOLE_STDOUT = 1, LO_CONSOLE_STDERR = 2 };
enum { LO_CONSOLE_OK = 0, LO_CONSOLE_WOULD_BLOCK = 1, LO_CONSOLE_CLOSED = 2, LO_CONSOLE_FAILED = 3 };
/* Callbacks must return without blocking. The write result confirms a prefix. */
typedef int (*LoConsoleWrite)(void *, uint32_t, const uint8_t *, size_t, size_t *);
typedef int (*LoConsoleFlush)(void *, uint32_t);
int lo_context_set_console(Context *, uint32_t, void *, LoConsoleWrite, LoConsoleFlush, uint64_t);
#endif
