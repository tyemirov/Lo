#ifndef LO_PROCESS_H
#define LO_PROCESS_H
#include "lo_runtime.h"

enum { LO_PROCESS_CAPTURE=0, LO_PROCESS_FORWARD=1 };
enum { LO_PROCESS_EXECUTION=0, LO_PROCESS_CANCELLED=1, LO_PROCESS_HOST=2,
       LO_PROCESS_LIMIT=3, LO_PROCESS_DENIED=4, LO_PROCESS_TIMEOUT=5 };
typedef struct {
    uint64_t timeout_ms, cleanup_ms;
    uintptr_t output_bytes;
    uint32_t forward_streams;
} LoProcessLimits;
typedef struct {
    int stage, class_id, started, cleanup_complete, termination_kind, termination_value;
    uintptr_t stdin_written, stdout_size, stderr_size;
    uint32_t cleanup_failures;
    uintptr_t ownership;
} LoProcessResult;
typedef int (*LoProcessStop)(const void *);
typedef int (*LoProcessOutput)(void *, uint32_t, const unsigned char *, uintptr_t, uintptr_t *);
typedef struct {
    const unsigned char *frame;
    uintptr_t frame_size;
    const unsigned char *input;
    uintptr_t input_size;
    uint64_t timeout_ms, cleanup_ms;
    uintptr_t output_bytes;
    uint32_t output_mode;
    LoProcessStop stop;
    const void *stop_token;
    LoProcessOutput output;
    void *output_token;
} LoProcessRequest;
typedef void (*LoProcessDriver)(uintptr_t, const LoProcessRequest *, LoProcessResult *);
/* Install before evaluation and before sharing the session. The synchronous
 * installation boundary stays closed after any session context evaluates.
 * Freeing an evaluated peer does not reopen driver or limits registration.
 * The synchronous
 * driver owns cleanup until return. All request pointers are borrowed. Output
 * calls contain 1 through 4096 bytes and return confirmed bytes and a class above.
 * Concurrent calls serialize through acknowledgment. Drivers preserve each
 * stream's order and join all callback threads before returning.
 * Stop returns zero or a terminal/timeout class. Callbacks must not reenter the
 * context. Driver code and userdata outlive all retained session ownership. */
int lo_context_process_driver(Context *, LoProcessDriver, uintptr_t);
int lo_context_process_driver_limits(Context *, const LoProcessLimits *);
int lo_context_process_close(Context *);
Context *lo_context_process_peer(Context *);
int lo_context_process_wait_retained(Context *);
uint32_t lo_context_process_ownership(Context *);
int lo_native_process_execute(const char *, int, const char *, const unsigned char *, uintptr_t,
                              const LoProcessRequest *, LoProcessResult *);
#endif
