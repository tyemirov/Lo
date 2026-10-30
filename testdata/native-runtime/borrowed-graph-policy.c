#include "lo_context_options.h"
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef __SIZE_TYPE__ Size;
typedef struct Context Context;
typedef struct Program Program;
typedef struct Value Value;
typedef struct { U32 status, code; const Value *value; } Result;
extern const Program *lo_program_new(void);
extern void lo_program_free(const Program *);
extern Context *lo_context_from_program(const Program *, const ContextOptions *);
extern int lo_context_set_host_invocation(Context *, const unsigned char *, Size);
extern void lo_context_run(Context *, Result *);
extern void lo_context_free(Context *);
extern void lo_value_release(const Value *);
extern int lo_boolean_value(const Value *);
extern U64 lo_context_scope_count(const Context *), lo_context_depth(const Context *);
extern U64 lo_runtime_live_contexts(void), lo_runtime_live_values(void);

#ifdef __APPLE__
extern void *malloc_default_zone(void);
extern void *malloc_zone_malloc(void *, Size), *malloc_zone_calloc(void *, Size, Size);
extern void *malloc_zone_realloc(void *, void *, Size), *malloc_zone_memalign(void *, Size, Size);
extern void malloc_zone_free(void *, void *);
#define ALLOCATE(size) malloc_zone_malloc(malloc_default_zone(), size)
#define ZERO(count, size) malloc_zone_calloc(malloc_default_zone(), count, size)
#define RESIZE(pointer, size) malloc_zone_realloc(malloc_default_zone(), pointer, size)
#define ALIGNED(alignment, size) malloc_zone_memalign(malloc_default_zone(), alignment, size)
#define RELEASE(pointer) malloc_zone_free(malloc_default_zone(), pointer)
#else
extern void *__libc_malloc(Size), *__libc_calloc(Size, Size), *__libc_realloc(void *, Size), *__libc_memalign(Size, Size);
extern void __libc_free(void *);
#define ALLOCATE(size) __libc_malloc(size)
#define ZERO(count, size) __libc_calloc(count, size)
#define RESIZE(pointer, size) __libc_realloc(pointer, size)
#define ALIGNED(alignment, size) __libc_memalign(alignment, size)
#define RELEASE(pointer) __libc_free(pointer)
#endif
static U64 live_allocations;
static void *tracked(void *pointer) {
    if (pointer) ++live_allocations;
    return pointer;
}
void *malloc(Size size) { return tracked(ALLOCATE(size)); }
void *calloc(Size count, Size size) { return tracked(ZERO(count, size)); }
void *realloc(void *pointer, Size size) {
    void *result = RESIZE(pointer, size);
    if (!pointer && result) ++live_allocations;
    if (pointer && !size && !result) --live_allocations;
    return result;
}
void free(void *pointer) { if (pointer) --live_allocations; RELEASE(pointer); }
int posix_memalign(void **pointer, Size alignment, Size size) {
    if (alignment < sizeof(void *) || (alignment & (alignment - 1))) return 22;
    void *result = tracked(ALIGNED(alignment, size));
    if (!result) return 12;
    *pointer = result;
    return 0;
}

int main(void) {
    const unsigned char ordinary[] = { @HOST_BODY@ };
    const unsigned char oversized[] = { @LARGE_HOST_BODY@ };
    const Program *program = lo_program_new();
    if (!program) return 10;
    ContextOptions policies[] = {
        {1000000, 0, LO_CASE >= 3 ? 512000 : LO_CASE ? 48000 : 12000, 0, 64, 0, 0},
        {1000000, 0, LO_CASE >= 3 ? 512000 : LO_CASE ? 48000 : 12000, LO_CASE >= 3 ? 2000000 : LO_CASE ? 80000 : 20000, 64, 0, 0},
        {1000000, 0, LO_CASE >= 3 ? 512000 : LO_CASE ? 48000 : 12000, 1, 64, 0, 0},
        {1000000, 0, LO_CASE >= 3 ? 512000 : LO_CASE ? 48000 : 12000, 0, 64, 0, 0},
    };
    for (unsigned index = 0; index < 4; ++index) {
        Context *context = lo_context_from_program(program, &policies[index]);
        if (!context) return 11;
        const unsigned char *body = index == 3 ? oversized : ordinary;
        Size size = index == 3 ? sizeof(oversized) : sizeof(ordinary);
        if (!lo_context_set_host_invocation(context, body, size)) return 12;
        Result result = {99, 99, 0};
        lo_context_run(context, &result);
        U32 expected = index >= 2 ? 2 : 0;
        if (result.status != expected || result.code != expected) return 20 + index;
        if (!expected && (!result.value || !lo_boolean_value(result.value))) return 30 + index;
        if (result.value) lo_value_release(result.value);
        if (lo_context_scope_count(context) || lo_context_depth(context)) return 40 + index;
        lo_context_free(context);
    }
    Context *repeated = lo_context_from_program(program, &policies[0]);
    if (!repeated || !lo_context_set_host_invocation(repeated, ordinary, sizeof(ordinary))) return 50;
    U64 baseline = 0, allocation_baseline = 0;
    for (unsigned request = 0; request < 100; ++request) {
        Result result = {99, 99, 0};
        lo_context_run(repeated, &result);
        if (result.status || result.code || !result.value || !lo_boolean_value(result.value)) return 51;
        lo_value_release(result.value);
        if (lo_context_scope_count(repeated) || lo_context_depth(repeated)) return 52;
        U64 live = lo_runtime_live_values();
        if (request && live != baseline) return 53;
        baseline = live;
        if (request == 3) allocation_baseline = live_allocations;
        if (request > 3 && live_allocations > allocation_baseline + 16) return 54;
    }
    lo_context_free(repeated);
    lo_program_free(program);
    return lo_runtime_live_contexts() || lo_runtime_live_values() ? 60 : 0;
}
