#ifndef LO_RUNTIME_H
#define LO_RUNTIME_H
#include <stddef.h>
#include <stdint.h>
#include "lo_context_options.h"
#ifdef __cplusplus
extern "C" {
#endif

typedef struct Program Program;
typedef struct Context Context;
typedef struct Value Value;
typedef struct {
    uint32_t status;
    uint32_t code;
    const Value *value;
} ResultSlot;
enum { LO_RESULT_OK = 0, LO_RESULT_ERROR = 1, LO_RESULT_HOST_FAILURE = 2 };
enum { LO_RESULT_WRONG_THREAD = 3 };

/* Program admission returns one owned reference or NULL. */
/* Context creation retains the program. The caller can then free its reference. */
/* A live immutable program can supply contexts on different owner threads. */
const Program *lo_program_new(void);
void lo_program_free(const Program *);
Context *lo_context_new(const ContextOptions *);
Context *lo_context_from_program(const Program *, const ContextOptions *);

/* A context belongs to its creation thread. Run, cancel, close, and free there. */
/* Hosts marshal cancellation requests to that owner, including callback entry. */
/* Cancel does not install signal handlers or observe native CLI signal state. */
/* The host owns signals for embedding. Program commands own their CLI signals. */
void lo_context_run(Context *, ResultSlot *);
void lo_context_cancel(Context *);
/* Close joins retained children. Return 1 for successful cleanup, 0 for failure. */
/* Repeated close returns the recorded result. Free also performs required cleanup. */
/* Complete validates the final owned value and publishes terminal output. */
/* Return 1 only after cleanup and output succeed. Then set the selected status. */
/* On failure, return 0 and leave the status output unchanged. */
int lo_context_close(Context *);
int lo_context_complete(Context *, const Value *, int *);
void lo_context_free(Context *);

/* Status 0 and 1 each transfer one value reference to the caller. */
/* Status 2 has no value. Code 3 identifies wrong-thread run rejection. */
/* Release each owned value once. Retain before another owner needs a reference. */
/* Values remain readable after context release while their references remain. */
/* NULL is permitted for program free, context free, and value retain/release. */
void lo_value_retain(const Value *);
void lo_value_release(const Value *);

enum {
    LO_DIAGNOSTIC_OK = 0,
    LO_DIAGNOSTIC_SHORT_BUFFER = 1,
    LO_DIAGNOSTIC_NONE = 2,
    LO_DIAGNOSTIC_INVALID = 3,
    LO_DIAGNOSTIC_WRONG_THREAD = 4,
    LO_DIAGNOSTIC_TOO_LARGE = 5
};
#define LO_DIAGNOSTIC_MAX_BYTES (1048576U)
/* Read on the context owner thread, including after checked close or completion. */
/* An optional result must come from this context and retain its value reference. */
/* NULL selects the stored host or cleanup fault. A stored fault takes precedence. */
/* Return UTF-8 diagnostic JSON without a terminal NUL. No Console output occurs. */
/* The caller owns the buffer and required-size output. They must not overlap. */
/* The required-size pointer is mandatory. NULL with zero capacity queries size. */
/* OK copies the whole document. SHORT_BUFFER sets the required size without a copy. */
/* Other statuses set required size to zero and leave the buffer unchanged. */
/* Documents above LO_DIAGNOSTIC_MAX_BYTES return TOO_LARGE without partial output. */
/* All non-NULL handles and supplied memory must remain valid for the call. */
int lo_context_diagnostic(const Context *, const ResultSlot *, uint8_t *, size_t, size_t *);

#ifdef __cplusplus
}
#endif
#endif
