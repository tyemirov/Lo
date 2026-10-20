/* P005 feasibility fixture. This is not the production Lo runtime ABI. */
#ifndef LO_RUNTIME_ABI_PROBE_H
#define LO_RUNTIME_ABI_PROBE_H

typedef unsigned int lo_u32;
typedef unsigned long long lo_u64;
typedef long long lo_i64;
typedef struct lo_context lo_context;
typedef struct lo_value lo_value;

enum { LO_OK = 0, LO_ERROR = 1, LO_HOST = 2 };
typedef struct {
    lo_u32 status;
    lo_u32 code;
    const lo_value *value;
} lo_result;
typedef void (*lo_callback)(lo_context *, const lo_value *, lo_result *);

lo_context *lo_context_new(lo_u64 budget);
void lo_context_drop(lo_context *context);
const lo_value *lo_value_new(lo_i64 number);
lo_i64 lo_value_read(const lo_value *value);
void lo_value_retain(const lo_value *value);
void lo_value_release(const lo_value *value);
lo_u64 lo_live_values(void);
void lo_apply(lo_context *context, const lo_value *input, lo_callback callback, lo_result *output);

_Static_assert(sizeof(lo_u32) == 4, "32-bit status");
_Static_assert(sizeof(lo_u64) == 8, "64-bit counter");
_Static_assert(sizeof(lo_result) == 16, "arm64 result layout");
_Static_assert(__builtin_offsetof(lo_result, value) == 8, "arm64 payload offset");

#endif
