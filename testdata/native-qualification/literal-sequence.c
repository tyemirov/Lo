#include "lo_context_options.h"
#define LO_OPTIONS(budget) (&(const ContextOptions){(budget),16ULL<<20,64ULL<<20,0,0,0,128})
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef struct Program Program;
typedef struct Context Context;
typedef struct Value Value;
typedef struct { U32 status, code; const Value *value; } Result;
extern const Program *lo_program_new(void);
extern void lo_program_free(const Program *);
extern Context *lo_context_from_program(const Program *, const ContextOptions *);
extern void lo_context_free(Context *), lo_context_run(Context *, Result *);
extern void lo_context_cancel_after_polls(Context *, U64);
extern U64 lo_context_remaining_steps(const Context *), lo_context_scope_count(const Context *), lo_context_depth(const Context *);
extern U64 lo_runtime_live_values(void), lo_runtime_live_programs(void), lo_runtime_live_contexts(void);
extern void lo_value_release(const Value *);
extern void lo_scope_leave(Context *, const Value *, U64);
extern long write(int, const void *, unsigned long);
#define ABI(name) void name(Context *context, const Value *const *arguments, U64 count, U64 site, Result *out)
extern ABI(lo_scope_enter);
extern ABI(lo_step);
extern ABI(lo_literal);
extern ABI(lo_charge_value);
extern ABI(lo_op_List_2Enew);
extern ABI(lo_cause);
extern const U64 lo_literal_sequence_sites[15], lo_literal_sequence_body_site;

static const unsigned kinds[15] = {1,2,2,2,3,4,2,3,4,2,3,4,5,4,6};
static const unsigned failed_step_calls[5] = {2,3,4,7,10};
static const Value *owners[15], *literals[3], *list;
static unsigned calls, owned, released, steps, charges, literal_count, closed, failed, error;
static void before(unsigned kind, U64 site) {
    if (failed || closed || calls >= 15 || kinds[calls] != kind || lo_literal_sequence_sites[calls] != site) error = 1;
    ++calls;
}
static void after(Result *result) {
    if (result->status == 0) {
        if (!result->value || owned >= 15) error = 2;
        else owners[owned++] = result->value;
    } else if (result->status == 2) failed = 1;
    else error = 3;
}
ABI(lo_literal_sequence_enter) {
    before(1,site); lo_scope_enter(context,arguments,count,site,out); after(out);
}
ABI(lo_literal_sequence_step) {
    before(2,site); lo_step(context,arguments,count,site,out);
    if (out->status == 0) ++steps;
    after(out);
}
ABI(lo_literal_sequence_literal) {
    before(3,site); lo_literal(context,arguments,count,site,out);
    if (out->status == 0) {
        if (literal_count >= 3) error = 4;
        else literals[literal_count++] = out->value;
    }
    after(out);
}
ABI(lo_literal_sequence_charge) {
    before(4,site);
    if (count != 1 || !arguments || charges >= 4 || arguments[0] != (charges < 3 ? literals[charges] : list)) error = 5;
    ++charges; lo_charge_value(context,arguments,count,site,out); after(out);
}
ABI(lo_literal_sequence_list) {
    before(5,site);
    if (count != 3 || !arguments || literal_count != 3 || charges != 3) error = 6;
    else for (unsigned index = 0; index < 3; ++index) if (arguments[index] != literals[index]) error = 7;
    lo_op_List_2Enew(context,arguments,count,site,out);
    if (out->status == 0) list = out->value;
    after(out);
}
ABI(lo_literal_sequence_cause) {
    before(6,site);
    if (count != 1 || !arguments || arguments[0] != list) error = 8;
    lo_cause(context,arguments,count,site,out); after(out);
}
void lo_literal_sequence_scope_leave(Context *context, const Value *value, U64 site) {
    if (closed || released || site != lo_literal_sequence_body_site || value != (failed ? 0 : list)) error = 9;
    lo_scope_leave(context,value,site); closed = 1;
}
void lo_literal_sequence_release(const Value *value) {
    if (value) {
        if (!closed || released >= owned || owners[released] != value) error = 10;
        ++released;
    }
    lo_value_release(value);
}
static int session(U64 budget, U64 cancel_poll) {
    calls=owned=released=steps=charges=literal_count=closed=failed=error=0;
    list=0; for (unsigned index=0;index<3;++index) literals[index]=0;
    const Program *program=lo_program_new(); if (!program) return 11;
    Context *context=lo_context_from_program(program,LO_OPTIONS(budget)); if (!context) return 12;
    if (cancel_poll) lo_context_cancel_after_polls(context,cancel_poll);
    Result result={99,99,0}; lo_context_run(context,&result);
    U64 expected_steps=LO_CASE ? 2+LO_CASE : cancel_poll && cancel_poll<=5 ? cancel_poll-1 : budget<5 ? budget : 5;
    int terminal=LO_CASE || budget<5 || (cancel_poll && cancel_poll<=5);
    if (error) return 20+error;
    if (!closed || released!=owned || steps!=expected_steps || lo_context_remaining_steps(context)!=budget-expected_steps) return 40;
    if (lo_context_scope_count(context) || lo_context_depth(context)) return 41;
    if (terminal) {
        if (result.status!=2 || result.code!=2 || result.value || calls!=(LO_CASE ? 2+3*LO_CASE : failed_step_calls[expected_steps])) return 42;
    } else if (result.status || result.code || !result.value || calls!=15 || charges!=4 || literal_count!=3 || owned!=15) return 43;
    lo_program_free(program); lo_context_free(context);
    if (lo_runtime_live_programs() || lo_runtime_live_contexts()) return 44;
    if (!terminal && !lo_runtime_live_values()) return 45;
    lo_value_release(result.value);
    if (lo_runtime_live_values()) return 46;
    return 0;
}
int main(void) {
    if (LO_CASE) { int status=session(100,0); if (status) return status; }
    else {
    /* Zero-step contexts are rejected; cancellation poll1 covers the first step. */
    for (U64 budget=1;budget<=6;++budget) { int status=session(budget,0); if (status) return status; }
    for (U64 poll=1;poll<=6;++poll) { int status=session(100,poll); if (status) return status; }
    }
    const char message[]="literal-sequence-prefixes-ok\n";
    return write(1,message,sizeof(message)-1)==sizeof(message)-1 ? 0 : 47;
}
