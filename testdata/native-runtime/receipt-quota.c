#include "lo_context_options.h"
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef struct Context Context;
typedef struct Program Program;
typedef struct Value Value;
typedef struct { U32 status, code; const Value *value; } ResultSlot;
extern const Program *lo_program_new(void);
extern void lo_program_free(const Program *);
extern Context *lo_context_from_program(const Program *, const ContextOptions *);
extern void lo_context_run(Context *, ResultSlot *);
extern void lo_context_free(Context *);
extern void lo_value_release(const Value *);
extern U64 lo_context_scope_count(const Context *), lo_context_depth(const Context *);
extern U64 lo_runtime_live_values(void), lo_runtime_live_contexts(void), lo_runtime_live_programs(void);
extern int lo_cli_main(int, const char **, const char **);
extern long write(int, const void *, __SIZE_TYPE__);
static int number(U64 value) {
    char digits[32], text[32];
    unsigned count = 0;
    do { digits[count++] = (char)('0' + value % 10); value /= 10; } while (value);
    for (unsigned i = 0; i < count; ++i) text[i] = digits[count - i - 1];
    return write(1, text, count) == (long)count;
}
static int invalid;
static int succeeds(const Program *program, U64 limit, unsigned cumulative) {
    ContextOptions policy = {1000000, 0, cumulative ? 0 : limit, cumulative ? limit : 0, 0, 0, 0};
    Context *context = lo_context_from_program(program, &policy);
    if (!context) { invalid = 1; return 0; }
    ResultSlot result = {99, 99, 0};
    lo_context_run(context, &result);
    int ok = result.status == 0;
    if ((result.status != 0 && result.status != 2) || result.code != result.status || !!result.value != ok) invalid = 1;
    if (lo_context_scope_count(context) || lo_context_depth(context)) invalid = 1;
    lo_context_free(context);
    if (result.value) lo_value_release(result.value);
    if (lo_runtime_live_contexts()) invalid = 1;
    return ok;
}
static U64 boundary(const Program *program, unsigned cumulative) {
    U64 low = 1, high = 1 << 24;
    if (!succeeds(program, high, cumulative)) { invalid = 1; return 0; }
    while (low < high) {
        U64 middle = low + (high - low) / 2;
        if (succeeds(program, middle, cumulative)) high = middle;
        else low = middle + 1;
    }
    if (!succeeds(program, low, cumulative) || succeeds(program, low - 1, cumulative)) invalid = 1;
    return low;
}
int main(void) {
    const char *args[] = {"fixture", "run", "--format", "json", "--max-steps", "1000000"};
    if (lo_cli_main(6, args, 0)) return 1;
    const Program *program = lo_program_new();
    if (!program) return 2;
    U64 live = boundary(program, 0), data = boundary(program, 1);
    lo_program_free(program);
    if (invalid || lo_runtime_live_values() || lo_runtime_live_programs()) return 3;
    const char first[] = "{\"live\":";
    const char second[] = ",\"data\":";
    if (write(1, first, sizeof(first)-1) != sizeof(first)-1 || !number(live) ||
        write(1, second, sizeof(second)-1) != sizeof(second)-1 || !number(data) ||
        write(1, "}\n", 2) != 2) return 4;
    return 0;
}
