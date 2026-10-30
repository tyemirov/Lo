#include "lo_context_options.h"
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef struct Context Context;
typedef struct Program Program;
typedef struct Value Value;
typedef struct { U32 status; U32 code; const Value *value; } ResultSlot;
extern const Program *lo_program_new(void);
extern void lo_program_free(const Program *);
extern Context *lo_context_new(const ContextOptions *);
extern Context *lo_context_from_program(const Program *, const ContextOptions *);
extern void lo_context_run(Context *, ResultSlot *);
extern void lo_context_free(Context *);
extern void lo_value_release(const Value *);
extern U64 lo_context_scope_count(const Context *);
extern U64 lo_context_depth(const Context *);
extern U64 lo_runtime_live_contexts(void);
extern U64 lo_runtime_live_values(void);
extern U64 lo_runtime_live_programs(void);

int main(void) {
    const Program *program = lo_program_new();
    if (!program || lo_context_new(0) || lo_context_from_program(program, 0)) return 1;
    ContextOptions policies[] = {
        {100000, 0, 0, 0, 0, 0, 0},
        {100000, 49, 0, 0, 0, 0, 0},
        {100000, 50, 0, 0, 0, 0, 0},
        {100000, 0, 1, 0, 0, 0, 0},
        {100000, 0, 0, 1, 0, 0, 0},
        {100000, 0, 0, 0, 1, 0, 0},
        {100000, 0, 0, 0, 2, 0, 0},
        {0, 0, 0, 0, 0, 0, 0},
    };
    U32 expected[] = {0, 2, 0, 2, 2, 2, 0};
    for (unsigned index = 0; index < sizeof(policies) / sizeof(*policies); ++index) {
        for (unsigned constructor = 0; constructor < 2; ++constructor) {
            Context *context = constructor ? lo_context_new(&policies[index]) : lo_context_from_program(program, &policies[index]);
            if (index == 7) { if (context) return 2; continue; }
            if (!context) return 3;
            ResultSlot result = {99, 99, 0};
            lo_context_run(context, &result);
            if (result.status != expected[index] || result.code != expected[index] || (result.value != 0) != (expected[index] == 0)) return 10 + index;
            if (lo_context_scope_count(context) || lo_context_depth(context)) return 20 + index;
            lo_context_free(context);
            if (result.value) lo_value_release(result.value);
            if (lo_runtime_live_contexts()) return 30 + index;
        }
    }
    ContextOptions repeated_policy = {100000, 0, 128, 0, 0, 0, 0};
    Context *repeated = lo_context_from_program(program, &repeated_policy);
    if (!repeated) return 41;
    for (unsigned index = 0; index < 100; ++index) {
        ResultSlot result = {99, 99, 0};
        lo_context_run(repeated, &result);
        if (result.status || !result.value) return 42;
        lo_value_release(result.value);
    }
    lo_context_free(repeated);
    Context *retained = lo_context_from_program(program, &repeated_policy);
    if (!retained) return 43;
    ResultSlot first = {99, 99, 0}, second = {99, 99, 0};
    lo_context_run(retained, &first);
    lo_context_run(retained, &second);
    if (first.status || !first.value || second.status != 2 || second.value) return 44;
    lo_context_free(retained);
    lo_value_release(first.value);
    repeated_policy.data_bytes = 200;
    Context *cumulative = lo_context_from_program(program, &repeated_policy);
    if (!cumulative) return 45;
    for (unsigned index = 0; index < 4; ++index) {
        ResultSlot result = {99, 99, 0};
        lo_context_run(cumulative, &result);
        if (result.status != (index == 3 ? 2 : 0)) return 46;
        if (result.value) lo_value_release(result.value);
    }
    lo_context_free(cumulative);
    lo_program_free(program);
    return lo_runtime_live_values() || lo_runtime_live_programs() ? 40 : 0;
}
