#include "lo_runtime.h"
#include "lo_file_observer.h"

extern int lo_cli_main(int, const char **, const char **);
extern uint64_t lo_runtime_live_contexts(void), lo_runtime_live_values(void);
extern int rename(const char *, const char *), fcntl(int, int, ...), dup2(int, int), close(int), access(const char *, int);
extern int mkstemp(char *), snprintf(char *, size_t, const char *, ...);
extern char *mkdtemp(char *);
extern long write(int, const void *, size_t);
extern void *dlsym(void *, const char *);
#if defined(__APPLE__)
extern int *__error(void);
#define ERROR_ADDRESS __error
#else
extern int *__errno_location(void);
extern long readlink(const char *, char *, size_t);
#define ERROR_ADDRESS __errno_location
#endif

#define DECLARE(name) extern void name(Context *, const Value **, uint64_t, uint64_t, ResultSlot *)
DECLARE(lo_f034_evidence_entry);
DECLARE(lo_f034_evidence_guard);
DECLARE(lo_commit);

static int commit_active, invalid, metadata_failures, enumeration_failed;
static unsigned attempts, removed, stream_close_failures, file_close_failures, guards, protection_failures;
static int last_tree_descriptor = -1, retained_descriptor = -1;
static void *failed_stream;

static int same(const char *left, const char *right) {
    while (*left && *left == *right) { ++left; ++right; }
    return *left == *right;
}

static int suffix(const unsigned char *bytes, uint64_t length, const char *ending) {
    uint64_t count = 0;
    while (ending[count]) ++count;
    if (length < count) return 0;
    for (uint64_t index = 0; index < count; ++index) if (bytes[length - count + index] != (unsigned char)ending[index]) return 0;
    return 1;
}

static unsigned descriptors(void) {
    unsigned count = 0;
    for (int descriptor = 0; descriptor < 512; ++descriptor) if (fcntl(descriptor, 1) >= 0) ++count;
    return count;
}

static int selected_root(int descriptor) {
    char path[4096];
#if defined(__APPLE__)
    if (fcntl(descriptor, 50, path)) return 0;
    size_t count = 0;
    while (path[count]) ++count;
#else
    char proc[64];
    int length = snprintf(proc, sizeof(proc), "/proc/self/fd/%d", descriptor);
    if (length < 0 || (size_t)length >= sizeof(proc)) return 0;
    long count = readlink(proc, path, sizeof(path));
    if (count < 0) return 0;
#endif
    return suffix((const unsigned char *)path, (uint64_t)count, "/parent/tree");
}

int openat(int parent, const char *name, int flags, ...) {
    int (*native_open)(int, const char *, int, ...) = (int (*)(int, const char *, int, ...))dlsym((void *)-1, "openat");
    if (!native_open) { *ERROR_ADDRESS() = 5; return -1; }
    int descriptor = native_open(parent, name, flags);
    if (descriptor >= 0 && commit_active && same(name, "tree") && selected_root(descriptor)) last_tree_descriptor = descriptor;
    return descriptor;
}

void *fdopendir(int descriptor) {
    void *(*native_stream)(int) = (void *(*)(int))dlsym((void *)-1, "fdopendir");
    if (!native_stream) { *ERROR_ADDRESS() = 5; return 0; }
    void *stream = native_stream(descriptor);
    if (stream && commit_active && (LO_CASE == 3 || LO_CASE == 4) && !failed_stream && selected_root(descriptor)) {
        failed_stream = stream;
        retained_descriptor = last_tree_descriptor;
        if (retained_descriptor < 0 || retained_descriptor == descriptor || !selected_root(retained_descriptor)) invalid = 1;
    }
    return stream;
}

int fstatat(int parent, const char *name, void *information, int flags) {
    int (*native_stat)(int, const char *, void *, int) = (int (*)(int, const char *, void *, int))dlsym((void *)-1, "fstatat");
    if (!native_stat) { *ERROR_ADDRESS() = 5; return -1; }
    if ((LO_CASE == 5 || LO_CASE == 6) && commit_active && guards == 2 && same(name, "a") && !protection_failures) {
        ++protection_failures;
        *ERROR_ADDRESS() = LO_CASE == 5 ? 5 : 2;
        return -1;
    }
    return native_stat(parent, name, information, flags);
}

void *readdir(void *stream) {
    void *(*native_next)(void *) = (void *(*)(void *))dlsym((void *)-1, "readdir");
    if (!native_next) { *ERROR_ADDRESS() = 5; return 0; }
    if (stream == failed_stream && !enumeration_failed) {
        enumeration_failed = 1;
        if (LO_CASE == 4 && (rename("parent", "held") || rename("replacement", "parent") || rename("held/tree", "parent/tree"))) invalid = 2;
        *ERROR_ADDRESS() = 5;
        return 0;
    }
    return native_next(stream);
}

int closedir(void *stream) {
    int (*native_close)(void *) = (int (*)(void *))dlsym((void *)-1, "closedir");
    if (!native_close) { *ERROR_ADDRESS() = 5; return -1; }
    int result = native_close(stream);
    if (stream == failed_stream && result == 0) {
        ++stream_close_failures;
        *ERROR_ADDRESS() = 9;
        return -1;
    }
    return result;
}

int close(int descriptor) {
    int (*native_close)(int) = (int (*)(int))dlsym((void *)-1, "close");
    if (!native_close) { *ERROR_ADDRESS() = 5; return -1; }
    int selected = enumeration_failed && descriptor == retained_descriptor && !file_close_failures;
    int result = native_close(descriptor);
    if (selected && result == 0) {
        ++file_close_failures;
        *ERROR_ADDRESS() = 28;
        return -1;
    }
    return result;
}

int unlinkat(int parent, const char *name, int flags) {
    int (*native_unlink)(int, const char *, int) = (int (*)(int, const char *, int))dlsym((void *)-1, "unlinkat");
    if (!native_unlink) { *ERROR_ADDRESS() = 5; return -1; }
    if (!commit_active || LO_CASE != 0) invalid = 3;
    ++attempts;
    if (attempts == 2) { *ERROR_ADDRESS() = 5; return -1; }
    int result = native_unlink(parent, name, flags);
    if (!result) ++removed;
    return result;
}

static int observer(uintptr_t user, uint32_t phase, const unsigned char *root, uint64_t root_length,
    const unsigned char *name, uint64_t name_length, LoFileMetadata *metadata) {
    (void)phase;
    if (user != 34 || !metadata) { invalid = 4; return -1; }
    if (commit_active && (LO_CASE == 1 || LO_CASE == 2) && name_length == 1 && name[0] == 'a' && suffix(root, root_length, "/a-dir")) {
        ++metadata_failures;
        return LO_CASE == 1 ? 5 : 2;
    }
    return 0;
}

void lo_f034_evidence_commit(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    commit_active = 1;
    lo_commit(context, arguments, count, site, result);
    commit_active = 0;
}

void lo_flow_1(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    lo_f034_evidence_guard(context, arguments, count, site, result);
    if (!result->status) ++guards;
}

void lo_entry(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    if (!lo_context_file_metadata_observer((LoContext *)context, observer, 34)) invalid = 5;
    lo_f034_evidence_entry(context, arguments, count, site, result);
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

int main(void) {
    if (directory("parent") || directory("parent/tree") || directory("parent/tree/a-dir") || directory("parent/tree/c-dir")
        || directory("replacement") || file("parent/tree/a-dir/a") || file("parent/tree/a-dir/b")
        || file("parent/tree/b-file") || file("parent/tree/c-dir/a")) return 10;
    int saved = fcntl(2, 0, 3);
    if (saved < 0 || dup2(1, 2) < 0) return 11;
    unsigned baseline = descriptors();
    const char *arguments[] = {"tree-evidence", "run", "--format", "json", "--observe-tree-root", "parent", "--remove-tree-root", "parent/tree", "--max-steps", "10000000", "--max-live-bytes", "16777216"};
    int status = lo_cli_main(12, arguments, 0);
    unsigned after = descriptors();
    if (dup2(saved, 2) < 0 || close(saved)) return 12;
    if (invalid || status != (LO_CASE == 0 ? 0 : LO_CASE == 4 ? 2 : 1) || removed != (LO_CASE == 0 ? 1U : 0U)) {
        char evidence[160];
        int length = snprintf(evidence, sizeof(evidence), "F034 evidence: invalid=%d status=%d removed=%u attempts=%u metadata=%d closedir=%u close=%u\n", invalid, status, removed, attempts, metadata_failures, stream_close_failures, file_close_failures);
        if (length > 0) write(1, evidence, (size_t)length);
        return 20;
    }
    if (after != baseline || lo_runtime_live_contexts() || lo_runtime_live_values()) return 21;
    if ((LO_CASE == 1 || LO_CASE == 2) && metadata_failures != 1) return 22;
    if ((LO_CASE == 3 || LO_CASE == 4) && (!enumeration_failed || stream_close_failures != 1 || file_close_failures != 1)) return 23;
    if (access("parent/tree/a-dir/a", 0) == 0 ? LO_CASE == 0 : LO_CASE != 0) return 24;
    if (access("parent/tree/a-dir/b", 0) != 0 || access("parent/tree/b-file", 0) != 0 || access("parent/tree/c-dir/a", 0) != 0) return 25;
    if ((LO_CASE == 5 || LO_CASE == 6) && (guards != 2 || protection_failures != 1)) return 26;
    return 0;
}
