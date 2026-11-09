#include "lo_runtime.h"
#include "lo_file_observer.h"
#include "lo_host_observer.h"

extern void lo_cleanup_workflow_entry(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
extern int lo_cli_main(int, const char *const *, const char *const *);
extern uint64_t lo_runtime_live_contexts(void), lo_runtime_live_values(void);
extern int setenv(const char *, const char *, int);
extern int unsetenv(const char *);
extern int chdir(const char *);
extern void *dlsym(void *, const char *);
extern char *getenv(const char *);
extern int strcmp(const char *, const char *), memcmp(const void *, const void *, __SIZE_TYPE__);
extern __SIZE_TYPE__ strlen(const char *);
extern int open(const char *, int, ...), close(int), flock(int, int);

static unsigned installations, volume_calls;
static int hook_error;

unsigned geteuid(void) {
    if (LO_ROOT_UID) return 0;
    unsigned (*native_uid)(void) = (unsigned (*)(void))dlsym((void *)-1, "geteuid");
    if (!native_uid) { hook_error = 1; return 0; }
    return native_uid();
}

static int metadata_observe(uintptr_t user, uint32_t phase,
                            const unsigned char *root, uint64_t root_length,
                            const unsigned char *name, uint64_t name_length,
                            LoFileMetadata *metadata) {
    (void)phase; (void)name; (void)name_length;
    if (user != 13 || !root || !root_length || !metadata) return -1;
    metadata->accessed_sec = metadata->modified_sec = metadata->changed_sec = 1;
    metadata->accessed_nsec = metadata->modified_nsec = metadata->changed_nsec = 0;
    metadata->allocated_blocks = 8;
    const char *behavior = getenv("FIXTURE_BEHAVIOR");
    if (behavior && !strcmp(behavior, "temp-owner") && root_length == strlen(LO_TEMP_PATH)
        && !memcmp(root, LO_TEMP_PATH, root_length)) ++metadata->owner_uid;
    return 0;
}

static int clock_observe(uintptr_t user, LoClockMetadata *metadata) {
    if (user != 13 || !metadata) return -1;
    metadata->seconds = 2000000000;
    metadata->nanoseconds = 0;
    return 0;
}

static int volume_observe(uintptr_t user, const unsigned char *root,
                          uint64_t root_length, LoVolumeMetadata *metadata) {
    if (user != 13 || !root || !root_length || !metadata) return -1;
    ++volume_calls;
    metadata->fragment_bytes = 1048576;
    metadata->total_blocks = 8192;
    metadata->free_blocks = 4096;
    metadata->available_blocks = volume_calls == 1 ? 1024 : 3072;
    return 0;
}

void lo_entry(Context *context, const Value **arguments, uint64_t count,
              uint64_t site, ResultSlot *result) {
    if (!lo_context_file_metadata_observer((LoContext *)context, metadata_observe, 13)
        || !lo_context_clock_observer(context, clock_observe, 13)
        || !lo_context_volume_observer(context, volume_observe, 13)) hook_error = 2;
    ++installations;
    lo_cleanup_workflow_entry(context, arguments, count, site, result);
}

int main(int argc, const char **argv) {
    (void)argc;
    int lock_descriptor = -1;
    if (LO_LOCK_BUSY) {
        lock_descriptor = open(argv[0], 0);
        if (lock_descriptor < 0 || flock(lock_descriptor, 6)) return 83;
    }
    if (chdir(LO_FIXTURE_DIRECTORY)) return 80;
    const char *environment[][2] = {LO_FIXTURE_ENVIRONMENT};
    for (unsigned index = 0; index < sizeof(environment) / sizeof(environment[0]); ++index) {
        if (setenv(environment[index][0], environment[index][1], 1)) return 80;
    }
    const char *behavior = getenv("FIXTURE_BEHAVIOR");
    if (behavior && !strcmp(behavior, "missing-home") && unsetenv("HOME")) return 80;
    const char *arguments[] = {LO_FIXTURE_ARGUMENTS};
    int status = lo_cli_main(sizeof(arguments) / sizeof(arguments[0]), arguments, 0);
    if (lock_descriptor >= 0 && close(lock_descriptor)) return 83;
    if (hook_error || installations != 1 || volume_calls != LO_VOLUME_CALLS) return 81;
    if (lo_runtime_live_contexts() || lo_runtime_live_values()) return 82;
    return status;
}
