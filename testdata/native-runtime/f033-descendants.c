#include "lo_runtime.h"
#include "lo_file_observer.h"

extern int lo_cli_main(int, const char **, const char **);
extern uint64_t lo_runtime_live_contexts(void), lo_runtime_live_values(void);
extern int rename(const char *, const char *), symlink(const char *, const char *);
extern int fcntl(int, int, ...), dup2(int, int);
extern int open(const char *, int, ...);
extern long read(int, void *, size_t);
extern int mkstemp(char *), snprintf(char *, size_t, const char *, ...);
extern char *mkdtemp(char *);
extern long write(int, const void *, size_t);
extern void *dlsym(void *, const char *);
#if defined(__APPLE__)
extern int *__error(void);
#define ERROR_ADDRESS __error
#else
extern int *__errno_location(void);
#define ERROR_ADDRESS __errno_location
#endif

typedef void (*Operation)(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
#define DECLARE(name) extern void name(Context *, const Value **, uint64_t, uint64_t, ResultSlot *)
DECLARE(lo_f033_entry);
DECLARE(lo_op_File_2Edirectory);
DECLARE(lo_op_File_2EDirectory_2Escan);
DECLARE(lo_op_File_2EDirectory_2Erefresh);
DECLARE(lo_op_File_2EDirectory_2Echild);
DECLARE(lo_op_File_2EDirectory_2Eentry);
DECLARE(lo_op_File_2EEntry_2Eobserve);
DECLARE(lo_project);

static Context *active;
static const Value *retained, *snapshot;
static int invalid, invocation, directory_calls, projection_calls;
static int operation, injected, fail_close, close_failures;
static unsigned baseline, operation_baseline, peak, metadata_calls;
static int failed_descriptor = -1;

void *fdopendir(int descriptor) {
    void *(*native_stream)(int) = (void *(*)(int))dlsym((void *)-1, "fdopendir");
    if (!native_stream) { *ERROR_ADDRESS() = 5; return 0; }
    if (LO_CASE == 34 && operation == 1 && !injected) {
        injected = 1; failed_descriptor = descriptor; *ERROR_ADDRESS() = 5; return 0;
    }
    return native_stream(descriptor);
}

int openat(int parent, const char *name, int flags, ...) {
    int (*native_open)(int, const char *, int, ...) = (int (*)(int, const char *, int, ...))dlsym((void *)-1, "openat");
    if (!native_open) { *ERROR_ADDRESS() = 5; return -1; }
    if (LO_CASE == 24 && operation == 5 && !injected) {
        injected = 1; *ERROR_ADDRESS() = 5; return -1;
    }
    if (LO_CASE == 26 && injected == 1 && name[0] == 'c' && name[1] == 'h'
        && name[2] == 'i' && name[3] == 'l' && name[4] == 'd' && name[5] == 0) {
        injected = 2; *ERROR_ADDRESS() = 5; return -1;
    }
    if ((LO_CASE == 22 || LO_CASE == 23) && operation == 3 && !injected
        && name[0] == 'c' && name[1] == 'h' && name[2] == 'i' && name[3] == 'l' && name[4] == 'd' && name[5] == 0) {
        injected = 1;
        if (LO_CASE == 22) { *ERROR_ADDRESS() = 5; return -1; }
        if (rename("ancestor/root/child", "ancestor/root/old-child")
            || rename("replacement", "ancestor/root/child")) invalid = 9;
    }
    return native_open(parent, name, flags);
}

int close(int descriptor) {
    int (*native_close)(int) = (int (*)(int))dlsym((void *)-1, "close");
    if (!native_close) { *ERROR_ADDRESS() = 5; return -1; }
    int result = native_close(descriptor);
    if ((fail_close || descriptor == failed_descriptor) && result == 0) {
        fail_close = 0;
        failed_descriptor = -1;
        ++close_failures;
        *ERROR_ADDRESS() = 5;
        return -1;
    }
    return result;
}

static unsigned descriptors(void) {
    unsigned count = 0;
    for (int descriptor = 0; descriptor < 256; ++descriptor) if (fcntl(descriptor, 1) >= 0) ++count;
    return count;
}

static int directory(const char *path) {
    char temporary[1024];
    int count = snprintf(temporary, sizeof(temporary), "%s-XXXXXX", path);
    return count < 0 || (size_t)count >= sizeof(temporary) || !mkdtemp(temporary) || rename(temporary, path);
}

static int file(const char *path) {
    char temporary[1024];
    int count = snprintf(temporary, sizeof(temporary), "%s-XXXXXX", path);
    if (count < 0 || (size_t)count >= sizeof(temporary)) return 1;
    int descriptor = mkstemp(temporary);
    if (descriptor < 0) return 1;
    long written = write(descriptor, "old", 3);
    return close(descriptor) || written != 3 || rename(temporary, path);
}

static void replace_root_ancestor(void) {
    injected = 1;
    if (rename("ancestor", "held") || rename("replacement", "ancestor")
        || rename("held/root", "ancestor/root")) invalid = 3;
}

static int observe(uintptr_t user, uint32_t phase, const unsigned char *root, uint64_t root_length,
    const unsigned char *name, uint64_t name_length, LoFileMetadata *metadata) {
    (void)name; (void)name_length;
    if (user != 33 || !root || !root_length || !metadata) { invalid = 1; return -1; }
    if (operation == 1 && ((LO_CASE == 32 && phase == LO_FILE_DISCOVERY) || LO_CASE == 33)) {
        if (!injected) replace_root_ancestor();
        else if (injected == 1) {
            injected = 2;
            if (rename("ancestor/root", "held/root") || rename("ancestor", "replacement")
                || rename("held", "ancestor")) invalid = 11;
        }
    }
    unsigned current = descriptors();
    if (current > peak) peak = current;
    if (LO_CASE == 7 && operation == 1 && phase == LO_FILE_DISCOVERY) return 2;
    if (LO_CASE == 8 && operation == 1 && phase == LO_FILE_DISCOVERY) return 5;
    if (LO_CASE == 9 && phase == LO_FILE_DIRECTORY) metadata->owner_uid = 1000 + ++metadata_calls;
    if (LO_CASE == 10 && operation == 5) metadata->size_bytes = 99;
    if (LO_CASE == 11 && operation == 2) lo_context_cancel(active);
    if (LO_CASE == 12 && operation == 2 && !injected) { fail_close = 1; injected = 1; }
    if (LO_CASE == 15 && operation == 4) return 5;
    if (LO_CASE == 16 && operation == 2) return 5;
    if (LO_CASE == 18 && operation == 2 && !injected) replace_root_ancestor();
    if (LO_CASE == 20 && operation == 5) return 5;
    if (LO_CASE == 25 && phase == LO_FILE_PREFLIGHT && !injected) {
        injected = 1;
        if (rename("ancestor/root/child", "ancestor/root/old-child")
            || rename("replacement", "ancestor/root/child")
            || rename("ancestor/root/old-child/inner", "ancestor/root/child/inner")) invalid = 10;
    }
    if (LO_CASE == 26 && phase == LO_FILE_PREFLIGHT) injected = 1;
    if (!injected && ((LO_CASE == 27 && operation == 2)
        || (LO_CASE == 28 && operation == 1 && phase == LO_FILE_DISCOVERY)
        || (LO_CASE == 29 && operation == 3)
        || ((LO_CASE == 30 || LO_CASE == 31) && operation == 2))) {
        replace_root_ancestor();
        if (LO_CASE == 30) return -1;
        if (LO_CASE == 31) { lo_context_cancel(active); return 0; }
        return 5;
    }
    return 0;
}

static void call(Operation target, int kind, Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    operation = kind;
    target(context, arguments, count, site, result);
    operation = 0;
    unsigned current = descriptors();
    if (current > peak) peak = current;
}

void lo_f033_directory(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    call(lo_op_File_2Edirectory, 0, context, arguments, count, site, result);
    if (!result->status && ++directory_calls == 1 && (LO_CASE == 4 || LO_CASE == 5)) {
        retained = result->value;
        lo_value_retain(retained);
    }
}

void lo_f033_scan(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    if (LO_CASE == 1 && !injected) {
        injected = 1;
        if (rename("ancestor/root/child", "ancestor/root/old-child")
            || rename("replacement", "ancestor/root/child")
            || rename("ancestor/root/old-child/inner", "ancestor/root/child/inner")) invalid = 2;
    }
    call(lo_op_File_2EDirectory_2Escan, 1, context, arguments, count, site, result);
}

void lo_f033_refresh(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    if ((LO_CASE == 0 || LO_CASE == 19) && !injected) {
        replace_root_ancestor();
    }
    const Value *replacement[] = {retained};
    if ((LO_CASE == 4 && invocation) || LO_CASE == 5) arguments = replacement;
    call(lo_op_File_2EDirectory_2Erefresh, 2, context, arguments, count, site, result);
}

void lo_f033_child(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    if ((LO_CASE == 2 || LO_CASE == 3 || LO_CASE == 17) && !injected) {
        injected = 1;
        if (rename("ancestor/root/child", "ancestor/root/old-child")) invalid = 4;
        if (LO_CASE == 2 && rename("replacement", "ancestor/root/child")) invalid = 5;
        if (LO_CASE == 3 && symlink("../../outside", "ancestor/root/child")) invalid = 6;
    }
    call(lo_op_File_2EDirectory_2Echild, 3, context, arguments, count, site, result);
}

void lo_f033_named_entry(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    call(lo_op_File_2EDirectory_2Eentry, 4, context, arguments, count, site, result);
}

void lo_f033_observe(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    if (LO_CASE == 21 && !injected) replace_root_ancestor();
    call(lo_op_File_2EEntry_2Eobserve, 5, context, arguments, count, site, result);
}

void lo_f033_project(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    const Value *replacement[] = {snapshot};
    if (LO_CASE == 6 && projection_calls == 1) arguments = replacement;
    lo_project(context, arguments, count, site, result);
    if (LO_CASE == 6 && !projection_calls++ && !result->status) {
        snapshot = result->value;
        lo_value_retain(snapshot);
    }
}

void lo_entry(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    active = context;
    /* CLI admission retains its own grants and program witnesses. */
    unsigned admitted = descriptors();
    if (admitted > operation_baseline) operation_baseline = admitted;
    if (!lo_context_file_metadata_observer((LoContext *)context, observe, 33)) invalid = 7;
    lo_f033_entry(context, arguments, count, site, result);
    active = 0;
}

int main(void) {
    if (directory("ancestor") || directory("ancestor/root") || directory("replacement") || directory("outside")) return 10;
    if (LO_CASE == 13 || LO_CASE == 14) {
        char parent[1024] = "ancestor/root";
        for (unsigned index = 0; index < 160; ++index) {
            char path[1024];
            if (LO_CASE == 13) snprintf(path, sizeof(path), "ancestor/root/child-%03u", index);
            else {
                snprintf(path, sizeof(path), "%s/z", parent);
                if (directory(path)) return 11;
                snprintf(path, sizeof(path), "%s/a", parent);
            }
            if (directory(path)) return 12;
            if (LO_CASE == 14) snprintf(parent, sizeof(parent), "%s", path);
        }
    } else if (LO_CASE == 10) {
        if (file("ancestor/root/entry")) return 13;
    } else if (directory("ancestor/root/child") || directory("ancestor/root/child/inner")) return 14;
    if ((LO_CASE == 25 || LO_CASE == 26) && file("ancestor/root/child/inner/entry")) return 17;
    if ((LO_CASE == 32 || LO_CASE == 33) && directory("ancestor/root/another")) return 18;
    int saved = fcntl(2, 0, 3);
    if (saved < 0 || dup2(1, 2) < 0) return 15;
    baseline = peak = descriptors();
    const char *arguments[] = {"descendants", "run", "--format", "json", LO_CASE == 19 ? "--list-root" : "--observe-tree-root", "ancestor/root", "--max-steps", "10000000", "--max-live-bytes", "8388608", "--remove-tree-root", "ancestor/root"};
    int status = lo_cli_main(LO_CASE == 25 || LO_CASE == 26 ? 12 : 10, arguments, 0);
    if (LO_CASE == 4) {
        if (status) invalid = 8;
        invocation = 1;
        status = lo_cli_main(10, arguments, 0);
    }
    lo_value_release(retained);
    lo_value_release(snapshot);
    unsigned after = descriptors();
    if (dup2(saved, 2) < 0 || close(saved)) return 16;
    int expected = LO_CASE == 30 || LO_CASE == 31 ? 2 : LO_CASE <= 3 || LO_CASE == 8 || LO_CASE >= 15 ? 1
        : (LO_CASE >= 4 && LO_CASE <= 6) || LO_CASE == 11 || LO_CASE == 12 ? 2 : 0;
    if (invalid || status != expected) return 30 + invalid;
    if (after != baseline || peak > operation_baseline + 12) {
        char evidence[128];
        int length = snprintf(evidence, sizeof(evidence), "descriptor counts: baseline=%u admitted=%u after=%u peak=%u\n", baseline, operation_baseline, after, peak);
        if (length > 0) write(1, evidence, (size_t)length);
        return 40;
    }
    if ((LO_CASE == 12 || LO_CASE == 34) && close_failures != 1) return 41;
    if (lo_runtime_live_contexts() || lo_runtime_live_values()) return 42;
    if (LO_CASE == 25 || LO_CASE == 26) {
        int descriptor = open("ancestor/root/child/inner/entry", 0);
        char contents[3];
        if (descriptor < 0 || read(descriptor, contents, 3) != 3 || close(descriptor)) return 43;
        if (contents[0] != 'o' || contents[1] != 'l' || contents[2] != 'd') return 44;
    }
    return 0;
}
