#include "lo_runtime.h"
#include "lo_file_observer.h"
#include "lo_host_observer.h"

extern void lo_qualified_entry(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
extern int lo_cli_main(int, const char **, const char **);
extern uint64_t lo_runtime_live_contexts(void), lo_runtime_live_values(void);
extern int close(int), fcntl(int, int, ...), dup2(int, int);
extern int rename(const char *, const char *);
extern char *mkdtemp(char *);
extern void *dlsym(void *, const char *);
#if defined(__APPLE__)
extern int *__error(void);
#define ERROR_ADDRESS __error
#else
extern int *__errno_location(void);
#define ERROR_ADDRESS __errno_location
#endif
typedef uintptr_t Thread;
extern int pthread_create(Thread *, const void *, void *(*)(void *), void *), pthread_join(Thread, void **);
static Context *active;
static int hook_error, clock_calls, volume_calls;
static int fail_close, close_failures;
static char replacement[] = "replacement-XXXXXX";
int close(int descriptor) {
    int (*native_close)(int) = (int (*)(int))dlsym((void *)-1, "close");
    if (!native_close) { *ERROR_ADDRESS() = 5; return -1; }
    int status = native_close(descriptor);
    if (fail_close && status == 0) {
        fail_close = 0;
        ++close_failures;
        *ERROR_ADDRESS() = 5;
        return -1;
    }
    return status;
}
static int descriptors(void) {
    int count = 0;
    for (int fd = 0; fd < 256; ++fd) if (fcntl(fd, 1) >= 0) ++count;
    return count;
}

static int clock_observe(uintptr_t user, LoClockMetadata *metadata) {
    if (user != 32 || !metadata) { hook_error = 1; return -1; }
    ++clock_calls;
    metadata->seconds = clock_calls;
    metadata->nanoseconds = 3;
    if (LO_CASE == 1) return 5;
    if (LO_CASE == 2) metadata->nanoseconds = 1000000000;
    if (LO_CASE == 3) metadata->seconds = INT64_MAX;
    if (LO_CASE == 12) lo_context_cancel(active);
    if (LO_CASE == 22) metadata->seconds = -1;
    return 0;
}
static int volume_observe(uintptr_t user, const unsigned char *root, uint64_t length, LoVolumeMetadata *metadata) {
    if (user != 32 || !root || !length || !metadata) { hook_error = 1; return -1; }
    ++volume_calls;
    metadata->fragment_bytes = 2048;
    metadata->total_blocks = 10;
    metadata->free_blocks = 4;
    metadata->available_blocks = LO_CASE == 14 && volume_calls == 2 ? 1 : 2;
    if (LO_CASE == 5) return 5;
    if (LO_CASE == 6) metadata->fragment_bytes = 0;
    if (LO_CASE == 7) metadata->free_blocks = 11;
    if (LO_CASE == 8) { metadata->fragment_bytes = 512; metadata->total_blocks = UINT64_MAX; }
    if (LO_CASE == 13) lo_context_cancel(active);
    if (LO_CASE == 16 && (rename("root", "old-root") || rename(replacement, "root"))) return -1;
    if (LO_CASE == 21) fail_close = 1;
    return 0;
}
static int metadata_observe(uintptr_t user, uint32_t phase, const unsigned char *root, uint64_t length, const unsigned char *name, uint64_t name_length, LoFileMetadata *metadata) {
    (void)phase; (void)root; (void)length; (void)name; (void)name_length;
    if (user != 32 || !metadata) { hook_error = 1; return -1; }
    metadata->allocated_blocks = LO_CASE == 10 ? -1 : LO_CASE == 11 ? INT64_MAX : 7;
    return 0;
}
static void *foreign_owner(void *opaque) {
    Context *context = opaque;
    if (lo_context_clock_observer(context, clock_observe, 32)
        || lo_context_volume_observer(context, volume_observe, 32)) hook_error = 1;
    return 0;
}
void lo_entry(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    active = context;
    int before = descriptors();
    if (LO_CASE == 18) {
        Thread thread;
        if (pthread_create(&thread, 0, foreign_owner, context) || pthread_join(thread, 0)) hook_error = 1;
    }
    if (LO_CASE != 17 && (!lo_context_clock_observer(context, clock_observe, 32)
        || !lo_context_volume_observer(context, volume_observe, 32)
        || !lo_context_file_metadata_observer((LoContext *)context, metadata_observe, 32))) hook_error = 1;
    lo_qualified_entry(context, arguments, count, site, result);
    if (lo_context_clock_observer(context, clock_observe, 32)
        || lo_context_volume_observer(context, volume_observe, 32)) hook_error = 1;
    if (((LO_CASE >= 4 && LO_CASE <= 8) || LO_CASE == 13 || LO_CASE == 14 || LO_CASE == 16 || LO_CASE == 20 || LO_CASE == 21)
        && descriptors() != before) hook_error = 1;
    active = 0;
}
int main(void) {
    if (LO_CASE == 16) {
        char root[] = "initial-XXXXXX";
        if (!mkdtemp(root) || rename(root, "root") || !mkdtemp(replacement)) return 19;
    }
    int saved = fcntl(2, 0, 3);
    if (saved < 0 || dup2(1, 2) < 0) return 20;
    const char *arguments[] = {"fixture", "run", "--format", "json", "--list-root", LO_CASE == 16 ? "root" : ".", "--write-root", ".", "--remove-root", ".", "--max-steps", LO_CASE == 19 || LO_CASE == 20 ? "1" : "1000000"};
    int status = lo_cli_main(12, arguments, 0);
    if (dup2(saved, 2) < 0 || close(saved)) return 21;
    int expected = LO_CASE == 1 || LO_CASE == 3 || LO_CASE == 5 ? 1 :
        LO_CASE == 2 || LO_CASE == 6 || LO_CASE == 7 || LO_CASE == 10 || LO_CASE == 12 || LO_CASE == 13 || LO_CASE == 16 || (LO_CASE >= 19 && LO_CASE <= 21) ? 2 : 0;
    if (status != expected || hook_error) return 22;
    if (LO_CASE == 0 && clock_calls != 2) return 23;
    if (LO_CASE == 14 && volume_calls != 2) return 24;
    if ((LO_CASE == 19 || LO_CASE == 20) && (clock_calls || volume_calls)) return 26;
    if (LO_CASE == 21 && close_failures != 1) return 27;
    if (lo_runtime_live_contexts() || lo_runtime_live_values()) return 25;
    return 0;
}
