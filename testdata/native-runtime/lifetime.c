#include "lo_context_options.h"
#define LO_OPTIONS(budget) (&(const ContextOptions){(budget),16ULL<<20,64ULL<<20,0,0,0,128})
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef __UINTPTR_TYPE__ Thread;
typedef struct Context Context;
typedef struct Value Value;
typedef struct { U32 status; U32 code; const Value *value; } ResultSlot;

extern Context *lo_context_new(const ContextOptions *);
extern void lo_context_free(Context *);
extern void lo_context_run(Context *, ResultSlot *);
extern void lo_context_cancel(Context *);
extern U64 lo_context_scope_count(const Context *);
extern U64 lo_context_depth(const Context *);
extern U64 lo_context_remaining_steps(const Context *);
extern U64 lo_runtime_live_contexts(void);
extern U64 lo_runtime_live_values(void);
extern void lo_value_release(const Value *);
extern int pthread_create(Thread *, const void *, void *(*)(void *), void *);
extern int pthread_join(Thread, void **);

static void *foreign_run(void *pointer) {
    ResultSlot result = {0, 0, 0};
    lo_context_run((Context *)pointer, &result);
    if (result.status != 2 || result.code != 3 || result.value) return (void *)1;
    return (void *)0;
}

int main(void) {
    if (LO_CASE == 5) {
        Context *context = lo_context_new(LO_OPTIONS(100000));
        if (context) {
            lo_context_free(context);
            return 40;
        }
        return lo_runtime_live_contexts() || lo_runtime_live_values() ? 41 : 0;
    }
    if (lo_context_new(LO_OPTIONS(0))) return 1;
    for (unsigned iteration = 0; iteration < 256; ++iteration) {
        U64 steps = LO_CASE == 2 ? 1 : 100000;
        Context *context = lo_context_new(LO_OPTIONS(steps));
        if (!context) return 10;
        if (lo_runtime_live_contexts() != 1) return 11;
        if (LO_CASE == 3) lo_context_cancel(context);
        if (LO_CASE == 4) {
            Thread thread;
            void *foreign_result = (void *)1;
            if (pthread_create(&thread, 0, foreign_run, context) ||
                pthread_join(thread, &foreign_result) || foreign_result) return 1;
            if (lo_context_remaining_steps(context) != steps) return 12;
        }
        U64 retained_values = 0;
        for (unsigned request = 0; request < 8; ++request) {
            U64 before = lo_context_remaining_steps(context);
            ResultSlot result = {99, 99, 0};
            lo_context_run(context, &result);
            U32 expected = LO_CASE == 1 ? 1 : (LO_CASE == 2 || LO_CASE == 3 ? 2 : 0);
            if (result.status != expected || result.code != expected) return 20;
            if ((result.value != 0) != (expected != 2)) return 21;
            if (lo_context_scope_count(context) != 0) return 22;
            if (lo_context_depth(context) != 0) return 23;
            U64 remaining = lo_context_remaining_steps(context);
            if (LO_CASE == 3 ? remaining != before : (before && remaining >= before)) return 24;
            if (LO_CASE == 2 && remaining != 0) return 25;
            if (result.value) lo_value_release(result.value);
            U64 live_values = lo_runtime_live_values();
            if (request && live_values != retained_values) return 26;
            retained_values = live_values;
        }
        lo_context_free(context);
        if (lo_runtime_live_contexts() != 0) return 30;
        if (lo_runtime_live_values() != 0) return 31;
    }
    return 0;
}
