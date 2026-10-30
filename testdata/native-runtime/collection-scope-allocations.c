typedef __SIZE_TYPE__ Size;
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef struct Context Context;
typedef struct Value Value;
typedef struct { U32 status, code; const Value *value; } Result;
extern int lo_cli_main(int, const char **, const char **);
extern void lo_op_List_2Efold(Context *, const Value **, U64, U64, Result *);
extern U64 lo_runtime_live_values(void), lo_runtime_live_contexts(void);
extern long write(int, const void *, Size);
static int collecting;
static U64 bytes, allocations, measured;

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

void lo_profile_fold(Context *context, const Value **arguments, U64 count,
                     U64 site, Result *result) {
    collecting = 1;
    ++measured;
    lo_op_List_2Efold(context, arguments, count, site, result);
    collecting = 0;
}

static int number(U64 value) {
    char text[32], digits[32];
    Size count = 0;
    do { digits[count++] = (char)('0' + value % 10); value /= 10; } while (value);
    for (Size index = 0; index < count; ++index) text[index] = digits[count - index - 1];
    return write(1, text, count) == (long)count;
}

int main(void) {
    const char *arguments[] = {"fixture", "run", "--format", "json",
                               "--max-steps", "10000000"};
    int status = lo_cli_main(6, arguments, 0);
    if (status || measured != 1) return 20;
    if (lo_runtime_live_values() || lo_runtime_live_contexts()) return 21;
    const char first[] = "{\"bytes\":";
    const char second[] = ",\"allocations\":";
    const char last[] = "}\n";
    if (write(1, first, sizeof(first) - 1) != sizeof(first) - 1 || !number(bytes) ||
        write(1, second, sizeof(second) - 1) != sizeof(second) - 1 || !number(allocations) ||
        write(1, last, sizeof(last) - 1) != sizeof(last) - 1) return 22;
    return 0;
}
