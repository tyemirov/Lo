#include "lo_runtime.h"
#include "lo_file_observer.h"

extern void lo_cleanup_tree_entry(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
extern int lo_cli_main(int, const char **, const char **);
extern uint64_t lo_runtime_live_contexts(void), lo_runtime_live_values(void);
extern int close(int), fcntl(int, int, ...), dup2(int, int);
extern void *dlsym(void *, const char *);
#if defined(__APPLE__)
extern int *__error(void);
#define ERROR_ADDRESS __error
#else
extern int *__errno_location(void);
extern int snprintf(char *, size_t, const char *, ...);
extern long readlink(const char *, char *, size_t);
#define ERROR_ADDRESS __errno_location
#endif

static unsigned installed, observations, foreign;
static int hook_error;
static const char *foreign_path = LO_FOREIGN_PATH;
static const char *shared_path = LO_SHARED_PATH;
static unsigned removed_prefix, unlink_failures;

static int equal(const unsigned char *value, uint64_t length, const char *expected) {
    uint64_t index = 0;
    while (index < length && expected[index]) {
        if (value[index] != (unsigned char)expected[index]) return 0;
        ++index;
    }
    return index == length && !expected[index];
}

static int same_name(const char *name, const char *expected) {
    uint64_t length = 0;
    while (name[length]) ++length;
    return equal((const unsigned char *)name, length, expected);
}

static int shared_parent(int descriptor) {
    char path[4096];
#if defined(__APPLE__)
    if (fcntl(descriptor, 50, path)) { hook_error = 4; return 0; }
    return same_name(path, shared_path);
#else
    char descriptor_path[64];
    int length = snprintf(descriptor_path, sizeof(descriptor_path), "/proc/self/fd/%d", descriptor);
    if (length < 0 || (size_t)length >= sizeof(descriptor_path)) { hook_error = 4; return 0; }
    long count = readlink(descriptor_path, path, sizeof(path));
    if (count < 0) { hook_error = 4; return 0; }
    return equal((const unsigned char *)path, (uint64_t)count, shared_path);
#endif
}

int unlinkat(int parent, const char *name, int flags) {
    int (*native_unlink)(int, const char *, int) = (int (*)(int, const char *, int))dlsym((void *)-1, "unlinkat");
    if (!native_unlink) { hook_error = 5; *ERROR_ADDRESS() = 5; return -1; }
    int selected = LO_CASE == 3 && shared_parent(parent);
    if (selected && same_name(name, "module.pcm")) {
        if (removed_prefix != 1) hook_error = 6;
        ++unlink_failures;
        *ERROR_ADDRESS() = 5;
        return -1;
    }
    int status = native_unlink(parent, name, flags);
    if (!status && selected && same_name(name, "a.pcm")) ++removed_prefix;
    return status;
}

static int observe(uintptr_t userdata, uint32_t phase, const unsigned char *root,
                   uint64_t root_length, const unsigned char *name,
                   uint64_t name_length, LoFileMetadata *metadata) {
    if (userdata != 34 || !root || !root_length || !name || !metadata) {
        hook_error = 1;
        return -1;
    }
    ++observations;
    if (equal(root, root_length, foreign_path)) {
        hook_error = 2;
        return -1;
    }
    if (phase == LO_FILE_DISCOVERY && equal(name, name_length, "ForeignProject")) {
        ++metadata->owner_uid;
        ++foreign;
    }
    metadata->accessed_sec = metadata->modified_sec = metadata->changed_sec = 0;
    metadata->accessed_nsec = metadata->modified_nsec = metadata->changed_nsec = 0;
    return 0;
}

void lo_entry(Context *context, const Value **arguments, uint64_t count,
              uint64_t site, ResultSlot *result) {
    if (lo_context_file_metadata_observer((LoContext *)context, observe, 34) != 1) hook_error = 3;
    else ++installed;
    lo_cleanup_tree_entry(context, arguments, count, site, result);
}

int main(void) {
    int saved_error = fcntl(2, 0, 3);
    if (saved_error < 0 || dup2(1, 2) < 0) return 20;
    const char *arguments[] = {LO_FIXTURE_ARGUMENTS};
    int status = lo_cli_main(sizeof(arguments) / sizeof(arguments[0]), arguments, 0);
    if (dup2(saved_error, 2) < 0 || close(saved_error)) return 21;
    if (status != (LO_CASE >= 2 ? 1 : 0) || installed != 1 || !observations || !foreign || hook_error) return 22;
    if (lo_runtime_live_contexts() || lo_runtime_live_values()) return 23;
    if (LO_CASE == 3 && (removed_prefix != 1 || unlink_failures != 1)) return 24;
    return 0;
}
