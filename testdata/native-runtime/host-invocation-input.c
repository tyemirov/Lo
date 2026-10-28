typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef __SIZE_TYPE__ Size;
typedef struct Context Context;
typedef struct Value Value;
typedef struct { U32 status, code; const Value *value; } ResultSlot;
extern Context *lo_context_new(U64);
extern int lo_context_set_host_invocation(Context *, const unsigned char *, Size);
extern void lo_context_run(Context *, ResultSlot *);
extern void lo_context_free(Context *);
extern void lo_value_release(const Value *);
extern int lo_boolean_value(const Value *);
extern U64 lo_runtime_live_values(void);
extern U64 lo_runtime_live_contexts(void);

int main(void) {
    Context *context = lo_context_new(100000);
    if (!context) return 10;
    unsigned char initial[] = { @HOST_BODY@ };
    if (!lo_context_set_host_invocation(context, initial, sizeof(initial))) return 11;
    for (Size index=0; index<sizeof(initial); index++) initial[index] = 'x';
    ResultSlot result = {0,0,0};
    lo_context_run(context, &result);
    if (result.status || !result.value) return 12;
    if (lo_context_set_host_invocation(context, initial, sizeof(initial))) return 13;
    if (!lo_boolean_value(result.value)) return 14;
    lo_value_release(result.value);
    lo_context_free(context);
    if (lo_runtime_live_values() || lo_runtime_live_contexts()) return 15;
    return 0;
}
