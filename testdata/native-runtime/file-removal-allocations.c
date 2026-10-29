#include "lo_file_observer.h"

typedef __SIZE_TYPE__ Size;
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef LoContext Context;
typedef struct Value Value;
typedef struct { U32 status, code; const Value *value; } Result;
extern int lo_cli_main(int, const char **, const char **);
extern void lo_profiled_entry(Context *, const Value **, U64, U64, Result *);
extern void lo_commit(Context *, const Value **, U64, U64, Result *);
extern U64 lo_runtime_live_values(void), lo_runtime_live_contexts(void);
extern long write(int, const void *, Size);
extern int open(const char *, int, ...), close(int);

static int collecting;
static U64 bytes, allocations, commits, measured;
static U64 discovery, preflight, removing;
static int observer_error;

#ifdef __APPLE__
extern void *malloc_default_zone(void);
extern void *malloc_zone_malloc(void *, Size);
extern void *malloc_zone_calloc(void *, Size, Size);
extern void *malloc_zone_realloc(void *, void *, Size);
extern void *malloc_zone_memalign(void *, Size, Size);
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

static void *record(void *pointer, Size size) {
    if (pointer && collecting) { bytes += size; ++allocations; }
    return pointer;
}
void *malloc(Size size) { return record(ALLOCATE(size), size); }
void *calloc(Size count, Size size) { return record(ZERO(count, size), count * size); }
void *realloc(void *pointer, Size size) { return record(RESIZE(pointer, size), size); }
void free(void *pointer) { RELEASE(pointer); }
int posix_memalign(void **pointer, Size alignment, Size size) {
    if (alignment < sizeof(void *) || (alignment & (alignment - 1))) return 22;
    void *result = record(ALIGNED(alignment, size), size);
    if (!result) return 12;
    *pointer = result;
    return 0;
}

static int observe(uintptr_t userdata, uint32_t phase, const unsigned char *root,
                   uint64_t root_length, const unsigned char *name,
                   uint64_t name_length, LoFileMetadata *metadata) {
    if (userdata != LO_CASE || !root || !root_length || !metadata) {
        observer_error = 1;
        return -1;
    }
    const char prefix[] = "selected-";
    if (name_length <= sizeof(prefix) - 1) return 0;
    for (Size index = 0; index < sizeof(prefix) - 1; ++index)
        if (name[index] != (unsigned char)prefix[index]) return 0;
    if (phase == LO_FILE_DISCOVERY) ++discovery;
    if (phase == LO_FILE_PREFLIGHT) ++preflight;
    if (phase == LO_FILE_REMOVE) ++removing;
    return 0;
}

void lo_entry(Context *context, const Value **arguments, U64 count,
              U64 site, Result *result) {
    if (lo_context_file_metadata_observer(context, observe, LO_CASE) != 1)
        observer_error = 1;
    lo_profiled_entry(context, arguments, count, site, result);
}

void lo_profile_commit(Context *context, const Value **arguments, U64 count,
                       U64 site, Result *result) {
    ++commits;
    collecting = commits == LO_CASE + 1;
    if (collecting) ++measured;
    lo_commit(context, arguments, count, site, result);
    collecting = 0;
}

static Size decimal(char *text, U64 value) {
    char digits[32];
    Size count = 0;
    do { digits[count++] = (char)('0' + value % 10); value /= 10; } while (value);
    for (Size index = 0; index < count; ++index) text[index] = digits[count - index - 1];
    return count;
}
static int number(U64 value) {
    char text[32];
    Size count = decimal(text, value);
    return write(1, text, count) == (long)count;
}

int main(void) {
    const char *arguments[] = {"fixture", "run", "--format", "json",
        "--write-root", ".", "--list-root", ".", "--remove-root", ".",
        "--max-steps", "10000000"};
    int status = lo_cli_main(12, arguments, 0);
    if (status || commits != LO_CASE + 1 || measured != 1) return 20;
    if (observer_error || discovery != LO_CASE || preflight != LO_CASE || removing != LO_CASE) return 24;
    for (U64 index = 0; index < LO_CASE; ++index) {
        char path[64] = "selected-";
        Size count = decimal(path + 9, index);
        path[9 + count] = 0;
        int file = open(path, 0);
        if (file >= 0) { close(file); return 21; }
    }
    if (lo_runtime_live_values() || lo_runtime_live_contexts()) return 22;
    const char first[] = "{\"bytes\":";
    const char second[] = ",\"allocations\":";
    const char last[] = "}\n";
    if (write(1, first, sizeof(first) - 1) != sizeof(first) - 1 || !number(bytes) ||
        write(1, second, sizeof(second) - 1) != sizeof(second) - 1 || !number(allocations) ||
        write(1, last, sizeof(last) - 1) != sizeof(last) - 1) return 23;
    return 0;
}
