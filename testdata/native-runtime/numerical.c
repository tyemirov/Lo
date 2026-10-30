#include "lo_context_options.h"
#define LO_OPTIONS(budget) (&(const ContextOptions){(budget),16ULL<<20,64ULL<<20,0,0,0,128})
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef struct Context Context;
typedef struct Value Value;
typedef struct { U32 status; U32 code; const Value *value; } ResultSlot;
extern Context *lo_context_new(const ContextOptions *);
extern void lo_context_free(Context *);
extern void lo_context_run(Context *, ResultSlot *);
extern void lo_context_cancel_after_polls(Context *, U64);
extern U64 lo_context_resource_bytes(const Context *, U32);
extern U64 lo_context_peak_resource_bytes(const Context *, U32);
extern U64 lo_context_tape_count(const Context *);
extern U64 lo_context_scope_count(const Context *);
extern U64 lo_context_depth(const Context *);
extern U64 lo_runtime_live_contexts(void);
extern U64 lo_runtime_live_values(void);
extern U64 lo_runtime_live_storages(void);
extern U64 lo_runtime_live_storage_bytes(void);
extern void lo_value_release(const Value *);
extern void lo_value_retain(const Value *);
static int clean(Context *c) {
    return !lo_context_scope_count(c) && !lo_context_depth(c) && !lo_context_tape_count(c) &&
        !lo_context_resource_bytes(c, 1) && !lo_context_resource_bytes(c, 3);
}
static U64 minimum_steps(void) {
    U64 low = 1, high = 100000;
    while (low < high) {
        U64 middle = low + (high-low)/2;
        Context *c = lo_context_new(LO_OPTIONS(middle));
        ResultSlot r = {99,99,0};
        lo_context_run(c, &r);
        if (r.status == 0) high = middle; else low = middle+1;
        if (r.value) lo_value_release(r.value);
        lo_context_free(c);
    }
    return low;
}
#if LO_CASE < 6 || LO_CASE == 8
int main(void) {
    U64 steps = LO_CASE == 2 ? minimum_steps()-1 : 100000000;
    for (unsigned iteration=0; iteration<(LO_CASE == 8 ? 1 : 32); ++iteration) {
        Context *c = lo_context_new(LO_OPTIONS(steps));
        if (!c) return 10;
        if (LO_CASE == 3) lo_context_cancel_after_polls(c,128);
        const Value *retained = 0;
        const Value *alias = 0;
        for (unsigned request=0; request<(LO_CASE == 8 ? 1 : 8); ++request) {
            ResultSlot r = {99,99,0};
            lo_context_run(c,&r);
            U32 expected = LO_CASE == 1 ? 1 : (LO_CASE == 2 || LO_CASE == 3 || LO_CASE == 8 ? 2 : 0);
            if (r.status != expected || r.code != expected) return 20;
            if (!clean(c)) return 21;
            /* Eight million-element FP32 buffers and one scalar reach the boundary. */
            if (LO_CASE == 8 && lo_context_peak_resource_bytes(c,1) != 32000004) return 34;
            if (!lo_context_peak_resource_bytes(c,1) && LO_CASE != 4 && LO_CASE != 5) return 22;
            U64 bytes = LO_CASE == 4 ? 32 : 4;
            U64 optimizer = LO_CASE == 4 ? 24 : 0;
            if (expected) {
                if (lo_context_resource_bytes(c,0) || lo_context_resource_bytes(c,2)) return 23;
                if (lo_runtime_live_storage_bytes()) return 24;
            } else {
                U64 multiplier = request == 1 || request == 2 ? 2 : 1;
                if (lo_context_resource_bytes(c,0) != bytes*multiplier) return 25;
                if (lo_context_resource_bytes(c,2) != optimizer*multiplier) return 26;
                if (request == 0) { retained = r.value; lo_value_retain(retained); alias = retained; }
                else lo_value_release(r.value);
                if (request == 1) { lo_value_release(retained); retained=0; }
                if (request == 2) { lo_value_release(alias); alias=0; }
            }
            if (expected && r.value) lo_value_release(r.value);
        }
        lo_context_free(c);
        if (lo_runtime_live_contexts() || lo_runtime_live_values() ||
            lo_runtime_live_storages() || lo_runtime_live_storage_bytes()) return 30;
        if (LO_CASE == 0 || LO_CASE == 4 || LO_CASE == 5) {
            c = lo_context_new(LO_OPTIONS(steps));
            ResultSlot r = {99,99,0};
            lo_context_run(c,&r);
            if (r.status) return 31;
            lo_context_free(c);
            if (lo_runtime_live_contexts() || !lo_runtime_live_storages()) return 32;
            lo_value_release(r.value);
            if (lo_runtime_live_values() || lo_runtime_live_storages() || lo_runtime_live_storage_bytes()) return 33;
        }
    }
    return 0;
}

#else
extern void lo_numerical_foreign(Context *, const Value **, ResultSlot *);
int main(void) {
    for (unsigned iteration=0; iteration<32; ++iteration) {
        Context *first = lo_context_new(LO_OPTIONS(100000)), *second = lo_context_new(LO_OPTIONS(100000));
        ResultSlot a={99,99,0}, b={99,99,0}, rejected={99,99,0};
        lo_context_run(first,&a); lo_context_run(second,&b);
        if (a.status || b.status) return 40;
        const Value *input[1]={a.value};
        lo_numerical_foreign(second,input,&rejected);
        if (rejected.status != 2 || rejected.code != 2 || rejected.value) return 41;
        lo_value_release(b.value); lo_context_free(second);
        lo_context_free(first);
        second=lo_context_new(LO_OPTIONS(100000));
        lo_context_run(second,&b);
        if (b.status) return 42;
        lo_numerical_foreign(second,input,&rejected);
        if (rejected.status != 2 || rejected.code != 2 || rejected.value) return 43;
        lo_value_release(a.value); lo_value_release(b.value); lo_context_free(second);
        if (lo_runtime_live_values() || lo_runtime_live_contexts()) return 44;
    }
    return 0;
}
#endif
