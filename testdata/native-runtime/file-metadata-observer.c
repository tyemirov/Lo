#include "lo_file_observer.h"

typedef __SIZE_TYPE__ Size;
typedef struct Value Value;
typedef struct { uint32_t status, code; const Value *value; } Result;
extern void lo_qualified_entry(LoContext *, const Value **, uint64_t, uint64_t, Result *);
extern void lo_context_cancel(LoContext *);
extern int lo_cli_main(int, const char **, const char **);
extern uint64_t lo_runtime_live_contexts(void), lo_runtime_live_values(void);
extern int open(const char *, int, ...), close(int), fcntl(int, int, ...), dup2(int, int);
extern long write(int, const void *, Size);

static LoContext *active;
static int installed, hook_error;
static unsigned preflight[2], removing[2], discovery[2], directories;

static int observe(uintptr_t userdata, uint32_t phase, const unsigned char *root,
                   uint64_t root_length, const unsigned char *name,
                   uint64_t name_length, LoFileMetadata *metadata) {
    if (userdata != 25 || !root || !root_length || !metadata) {
        hook_error = 1;
        return -1;
    }
    if (phase == LO_FILE_DIRECTORY) {
        ++directories;
        metadata->owner_uid = 1001;
        return 0;
    }
    if (name_length != 1 || (name[0] != 'a' && name[0] != 'b')) return 0;
    unsigned index = name[0] == 'a' ? 0 : 1;
    metadata->owner_uid = 1001;
    metadata->accessed_sec = -1;
    metadata->accessed_nsec = 123456789;
    metadata->changed_sec = -1;
    metadata->changed_nsec = 0;
    if (phase == LO_FILE_DISCOVERY) {
        ++discovery[index];
        if ((LO_CASE == 1 || LO_CASE == 11) && index == 0) return 2;
        if (LO_CASE == 2 && index == 0) return 5;
        if (LO_CASE == 7 && index == 0) return -1;
    }
    if (phase == LO_FILE_PREFLIGHT) {
        ++preflight[index];
        if (LO_CASE == 3 && index == 0) metadata->owner_uid = 1002;
    }
    if (phase == LO_FILE_REMOVE) {
        ++removing[index];
        if (LO_CASE == 4 && index == 1) metadata->owner_uid = 1002;
        if (LO_CASE == 8 && index == 1) metadata->accessed_sec = -2;
        if (LO_CASE == 9 && index == 1) metadata->changed_sec = 0;
        if (LO_CASE == 10 && index == 1) ++metadata->link_count;
        if (LO_CASE == 6 && index == 0) lo_context_cancel(active);
    }
    return 0;
}

void lo_entry(LoContext *context, const Value **arguments, uint64_t count,
              uint64_t site, Result *result) {
    active = context;
    if (lo_context_file_metadata_observer(context, observe, 25) != 1) hook_error = 1;
    else installed = 1;
    lo_qualified_entry(context, arguments, count, site, result);
    active = 0;
}

static int present(const char *name) {
    int descriptor = open(name, 0);
    if (descriptor < 0) return 0;
    if (close(descriptor)) hook_error = 1;
    return 1;
}

int main(void) {
    int saved_error = fcntl(2, 0, 3);
    if (saved_error < 0 || dup2(1, 2) < 0) return 20;
    const char *arguments[] = {"fixture", "run", "--format", "json",
        "--write-root", ".", "--list-root", ".", "--remove-root", ".",
        "--max-steps", LO_CASE == 5 ? "2000" : "1000000"};
    int status = lo_cli_main(12, arguments, 0);
    if (dup2(saved_error, 2) < 0 || close(saved_error)) return 21;
    int expected_status = LO_CASE < 2 ? 0 : (LO_CASE == 5 || LO_CASE == 6 || LO_CASE == 7 ? 2 : 1);
    if (status != expected_status || !installed || hook_error || (LO_CASE != 11 && !directories)) return 22;
    if (LO_CASE == 4 || (LO_CASE >= 8 && LO_CASE <= 10)) {
        if (present("a") || !present("b") || preflight[0] != 1 || preflight[1] != 1 || removing[0] != 1 || removing[1] != 1) return 23;
    } else if (!present("a") || !present("b")) return 24;
    if (LO_CASE == 3 && (preflight[0] != 1 || preflight[1] || removing[0] || removing[1])) return 25;
    if (LO_CASE == 5 && (!preflight[0] || removing[0] || removing[1])) return 26;
    if (LO_CASE == 6 && (!removing[0] || removing[1])) return 27;
    if (lo_runtime_live_contexts() || lo_runtime_live_values()) return 28;
    return 0;
}
