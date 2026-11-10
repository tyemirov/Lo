#include "lo_runtime.h"

extern uint64_t lo_runtime_live_contexts(void), lo_runtime_live_values(void), lo_runtime_live_programs(void);
extern void lo_context_cancel_after_polls(Context *, uint64_t);
extern int lo_boolean_value(const Value *);
extern void lo_op_JSON_2Edecode(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);

#ifdef __APPLE__
extern void *malloc_default_zone(void);
extern void *malloc_zone_malloc(void *, size_t), *malloc_zone_calloc(void *, size_t, size_t);
extern void *malloc_zone_realloc(void *, void *, size_t), *malloc_zone_memalign(void *, size_t, size_t);
extern void malloc_zone_free(void *, void *);
#define ALLOCATE(size) malloc_zone_malloc(malloc_default_zone(), size)
#define ZERO(count, size) malloc_zone_calloc(malloc_default_zone(), count, size)
#define RESIZE(pointer, size) malloc_zone_realloc(malloc_default_zone(), pointer, size)
#define ALIGNED(alignment, size) malloc_zone_memalign(malloc_default_zone(), alignment, size)
#define RELEASE(pointer) malloc_zone_free(malloc_default_zone(), pointer)
#else
extern void *__libc_malloc(size_t), *__libc_calloc(size_t, size_t), *__libc_realloc(void *, size_t), *__libc_memalign(size_t, size_t);
extern void __libc_free(void *);
#define ALLOCATE(size) __libc_malloc(size)
#define ZERO(count, size) __libc_calloc(count, size)
#define RESIZE(pointer, size) __libc_realloc(pointer, size)
#define ALIGNED(alignment, size) __libc_memalign(alignment, size)
#define RELEASE(pointer) __libc_free(pointer)
#endif

static struct Allocation { void *pointer; size_t size; } allocations[4096];
static uint64_t large_live, parser_allocations, diagnostic_bytes, decode_calls;
static unsigned parsing, diagnostic, tracking_error;

static void forget(void *pointer) {
    if (!pointer) return;
    for (size_t index = 0; index < 4096; ++index) {
        if (allocations[index].pointer == pointer) {
            large_live -= allocations[index].size;
            allocations[index].pointer = 0;
            return;
        }
    }
}

static void *record(void *pointer, size_t size) {
    if (!pointer) return pointer;
    if (diagnostic) diagnostic_bytes += size;
    if (!parsing) return pointer;
    ++parser_allocations;
    if (size < 8192) return pointer;
    for (size_t index = 0; index < 4096; ++index) {
        if (!allocations[index].pointer) {
            allocations[index].pointer = pointer;
            allocations[index].size = size;
            large_live += size;
            return pointer;
        }
    }
    tracking_error = 1;
    return pointer;
}

void *malloc(size_t size) { return record(ALLOCATE(size), size); }
void *calloc(size_t count, size_t size) { return record(ZERO(count, size), count * size); }
void *realloc(void *pointer, size_t size) {
    void *result = RESIZE(pointer, size);
    if (result || !size) forget(pointer);
    return record(result, size);
}
void free(void *pointer) { forget(pointer); RELEASE(pointer); }
int posix_memalign(void **pointer, size_t alignment, size_t size) {
    if (alignment < sizeof(void *) || (alignment & (alignment - 1))) return 22;
    void *result = record(ALIGNED(alignment, size), size);
    if (!result) return 12;
    *pointer = result;
    return 0;
}

void lo_f031_accounted_decode(Context *context, const Value **arguments,
                             uint64_t count, uint64_t site, ResultSlot *result) {
    ++decode_calls;
    if (LO_CASE == 4) lo_context_cancel_after_polls(context, 64);
    parsing = 1;
    lo_op_JSON_2Edecode(context, arguments, count, site, result);
    parsing = 0;
}

static int contains(const unsigned char *bytes, size_t size, const char *text) {
    for (size_t offset = 0; offset < size; ++offset) {
        size_t index = 0;
        while (text[index] && offset + index < size && bytes[offset + index] == (unsigned char)text[index]) ++index;
        if (!text[index]) return 1;
    }
    return 0;
}

int main(void) {
    ContextOptions options = {.steps = 20000000, .value_bytes = 0, .live_bytes = 67108864,
        .data_bytes = 0, .list_items = 0, .file_entries = 0, .file_directories = 0, .json_depth = 0};
    if (LO_CASE <= 1) options.live_bytes = 1048576;
    if (LO_CASE == 2 || LO_CASE == 3) options.live_bytes = 8388608;
    if (LO_CASE == 7) options.json_depth = 4;
    if (LO_CASE == 8) options.json_depth = 5;
    Context *context = lo_context_new(&options);
    if (!context) return 10;
    ResultSlot result = {99, 99, 0};
    lo_context_run(context, &result);
    if (tracking_error) return 11;
    if (LO_CASE <= 1 && decode_calls != 64) return 12;
    if ((LO_CASE == 2 || LO_CASE == 3) && (decode_calls != 16 || large_live)) return 13;
    if (LO_CASE == 4 || LO_CASE == 7) {
        if (result.status != LO_RESULT_HOST_FAILURE || result.value) return 14;
        unsigned char buffer[8192]; size_t needed = 0;
        if (lo_context_diagnostic(context, &result, buffer, sizeof(buffer), &needed) != LO_DIAGNOSTIC_OK) return 15;
        if (!contains(buffer, needed, LO_CASE == 4 ? "runtime.cancelled" : "runtime.limit")) return 16;
        if (LO_CASE == 4 && (decode_calls != 1 || !parser_allocations || large_live)) return 17;
    } else if (LO_CASE == 5 || LO_CASE >= 10) {
        if (result.status != LO_RESULT_ERROR || !result.value || decode_calls != 1) return 18;
        unsigned char buffer[16]; for (size_t index = 0; index < sizeof(buffer); ++index) buffer[index] = 0xa5;
        size_t needed = 99; diagnostic = 1;
        int status = lo_context_diagnostic(context, &result, buffer, sizeof(buffer), &needed);
        diagnostic = 0;
        if (status != LO_DIAGNOSTIC_TOO_LARGE || needed) return 19;
        for (size_t index = 0; index < sizeof(buffer); ++index) if (buffer[index] != 0xa5) return 20;
        /* A capped diagnostic must stop before serializing repeated full snapshots. */
        if (diagnostic_bytes > 8ULL * LO_DIAGNOSTIC_MAX_BYTES) return 21;
    } else {
        if (result.status != LO_RESULT_OK || !result.value) return 22;
        size_t needed = 99;
        if (lo_context_diagnostic(context, &result, 0, 0, &needed) != LO_DIAGNOSTIC_NONE || needed) return 23;
        if (LO_CASE <= 3 || LO_CASE == 6 || LO_CASE == 9) {
            if (!lo_boolean_value(result.value)) return 26;
        }
    }
    if (result.value) lo_value_release(result.value);
    if (!lo_context_close(context)) return 24;
    lo_context_free(context);
    if (large_live || lo_runtime_live_contexts() || lo_runtime_live_values() || lo_runtime_live_programs()) return 25;
    return 0;
}
