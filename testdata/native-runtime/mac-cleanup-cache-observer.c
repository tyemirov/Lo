#include "lo_runtime.h"
#include "lo_file_observer.h"

extern void lo_cleanup_entry(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
extern int lo_cli_main(int, const char **, const char **);
extern uint64_t lo_runtime_live_contexts(void), lo_runtime_live_values(void);
extern int open(const char *, int, ...), close(int), fcntl(int, int, ...), dup2(int, int);
extern long read(int, void *, __SIZE_TYPE__);

static const char *calls = LO_FIXTURE_CALLS;
static int installed, changed, hook_error;

static int observe(uintptr_t userdata, uint32_t phase, const unsigned char *root,
                   uint64_t root_length, const unsigned char *name,
                   uint64_t name_length, LoFileMetadata *metadata) {
    (void)name;
    (void)name_length;
    if (userdata != 13 || !root || !root_length || !metadata) {
        hook_error = 1;
        return -1;
    }
    if (phase == LO_FILE_DIRECTORY) {
        int descriptor = open(calls, 0);
        if (descriptor < 0) return 5;
        char byte;
        long count = read(descriptor, &byte, 1);
        if (close(descriptor) || count < 0) return 5;
        if (count == 1) {
            ++metadata->owner_uid;
            changed = 1;
        }
    }
    return 0;
}

void lo_entry(Context *context, const Value **arguments, uint64_t count,
              uint64_t site, ResultSlot *result) {
    if (lo_context_file_metadata_observer((LoContext *)context, observe, 13) != 1) hook_error = 1;
    else installed = 1;
    lo_cleanup_entry(context, arguments, count, site, result);
}

int main(void) {
    int saved_error = fcntl(2, 0, 3);
    if (saved_error < 0 || dup2(1, 2) < 0) return 20;
    const char *arguments[] = {LO_FIXTURE_ARGUMENTS};
    int status = lo_cli_main(sizeof(arguments) / sizeof(arguments[0]), arguments, 0);
    if (dup2(saved_error, 2) < 0 || close(saved_error)) return 21;
    if (status != 1 || !installed || !changed || hook_error) return 22;
    if (lo_runtime_live_contexts() || lo_runtime_live_values()) return 23;
    return 0;
}
