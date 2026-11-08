#include "lo_runtime.h"
#include "lo_file_observer.h"

extern void lo_cleanup_policy_entry(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
extern int lo_cli_main(int, const char **, const char **);
extern uint64_t lo_runtime_live_contexts(void), lo_runtime_live_values(void);
extern int open(const char *, int, ...), close(int), fcntl(int, int, ...), dup2(int, int);
#if defined(__APPLE__)
extern int *__error(void);
#define ERROR_ADDRESS __error
#else
extern int *__errno_location(void);
#define ERROR_ADDRESS __errno_location
#endif

static const char *fixture_root = LO_FIXTURE_ROOT;
static const char *stage_path = LO_FIXTURE_STAGE;
static const char *removed_path = LO_FIXTURE_REMOVED;
static Context *active;
static unsigned installed, injected, directory_observations, allocation_observations;
static int hook_error;

static int equal(const unsigned char *value, uint64_t length, const char *expected) {
    uint64_t index = 0;
    while (index < length && expected[index]) {
        if (value[index] != (unsigned char)expected[index]) return 0;
        ++index;
    }
    return index == length && !expected[index];
}

static int under(const unsigned char *value, uint64_t length, const char *parent) {
    uint64_t index = 0;
    while (index < length && parent[index]) {
        if (value[index] != (unsigned char)parent[index]) return 0;
        ++index;
    }
    return !parent[index] && (index == length || value[index] == '/');
}

static int observe(uintptr_t userdata, uint32_t phase, const unsigned char *root,
                   uint64_t root_length, const unsigned char *name,
                   uint64_t name_length, LoFileMetadata *metadata) {
    if (userdata != 29 || !active || !root || !root_length || !name || !metadata) {
        hook_error = 1;
        return -1;
    }
    if (LO_CASE == 0) {
        if (under(root, root_length, stage_path)) {
            hook_error = 2;
            return -1;
        }
        if (phase == LO_FILE_DISCOVERY && equal(root, root_length, fixture_root)
            && equal(name, name_length, "a-foreign")) {
            ++metadata->owner_uid;
            ++injected;
        }
    }
    if (LO_CASE == 1 && phase == LO_FILE_DIRECTORY
        && equal(root, root_length, stage_path)) {
        int descriptor = open(removed_path, 0);
        if (descriptor >= 0) {
            if (close(descriptor)) hook_error = 3;
            hook_error = 4;
            return -1;
        }
        if (*ERROR_ADDRESS() != 2) {
            hook_error = 5;
            return -1;
        }
        ++injected;
        lo_context_cancel(active);
    }
    if (LO_CASE == 2 && phase != LO_FILE_DIRECTORY) {
        if (equal(name, name_length, "a.cache")) metadata->allocated_blocks = 6;
        else if (equal(name, name_length, "open.cache")) metadata->allocated_blocks = 10;
        else if (equal(name, name_length, "unsafe\n.cache")) metadata->allocated_blocks = 14;
        else return 0;
        ++allocation_observations;
    }
    if (LO_CASE == 3 && phase == LO_FILE_DIRECTORY
        && equal(root, root_length, fixture_root)) {
        ++directory_observations;
        if (directory_observations == 3) {
            ++metadata->owner_uid;
            ++injected;
        }
    }
    return 0;
}

void lo_entry(Context *context, const Value **arguments, uint64_t count,
              uint64_t site, ResultSlot *result) {
    active = context;
    if (lo_context_file_metadata_observer((LoContext *)context, observe, 29) != 1) hook_error = 6;
    else ++installed;
    lo_cleanup_policy_entry(context, arguments, count, site, result);
    active = 0;
}

int main(void) {
    int saved_error = fcntl(2, 0, 3);
    if (saved_error < 0 || dup2(1, 2) < 0) return 20;
    const char *arguments[] = {LO_FIXTURE_ARGUMENTS};
    int status = lo_cli_main(sizeof(arguments) / sizeof(arguments[0]), arguments, 0);
    if (dup2(saved_error, 2) < 0 || close(saved_error)) return 21;
    if (status != (LO_CASE == 1 ? 2 : 0) || installed != 1 || hook_error) return 22;
    if ((LO_CASE == 0 || LO_CASE == 1) && !injected) return 23;
    if (LO_CASE == 2 && allocation_observations < 3) return 24;
    /* One scan can replace refresh+scan. Its returned owner must still be valid. */
    if (LO_CASE == 3 && directory_observations < 2) return 25;
    if (lo_runtime_live_contexts() || lo_runtime_live_values()) return 26;
    return 0;
}
