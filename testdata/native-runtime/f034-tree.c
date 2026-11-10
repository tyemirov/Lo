#include "lo_runtime.h"
#include "lo_file_observer.h"

extern int lo_cli_main(int, const char **, const char **);
extern uint64_t lo_runtime_live_contexts(void), lo_runtime_live_values(void);
extern int rename(const char *, const char *), link(const char *, const char *);
extern int fcntl(int, int, ...), dup2(int, int), close(int), access(const char *, int);
extern int mkstemp(char *), snprintf(char *, size_t, const char *, ...);
extern char *mkdtemp(char *);
extern char *getcwd(char *, size_t);
extern long write(int, const void *, size_t);
extern void *dlsym(void *, const char *);
#if defined(__APPLE__)
extern int *__error(void);
#define ERROR_ADDRESS __error
#else
extern int *__errno_location(void);
extern long readlink(const char *, char *, size_t);
/* The prepared Linux AArch64 sysroot defines this readdir ABI. */
typedef struct FixtureDirectory FixtureDirectory;
struct FixtureDirectoryEntry {
    uint64_t inode;
    int64_t offset;
    unsigned short record_length;
    unsigned char type;
    char name[256];
};
_Static_assert(offsetof(struct FixtureDirectoryEntry, name) == 19, "Linux dirent name offset");
extern FixtureDirectory *opendir(const char *);
extern struct FixtureDirectoryEntry *readdir(FixtureDirectory *);
extern int dirfd(FixtureDirectory *), closedir(FixtureDirectory *);
static unsigned descriptor_measurements, descriptor_entries, reference_probes;
static unsigned metadata_observations, reference_matches, sparse_checks;
#define ERROR_ADDRESS __errno_location
#endif

#define DECLARE(name) extern void name(Context *, const Value **, uint64_t, uint64_t, ResultSlot *)
DECLARE(lo_f034_entry);
DECLARE(lo_f034_guard);
DECLARE(lo_op_File_2ETree_2Esnapshot);
DECLARE(lo_op_File_2ETree_2ERemove_2Echecked);
DECLARE(lo_commit);
DECLARE(lo_project);

static Context *active;
static const Value *retained, *diagnostic;
static unsigned admitted, peak, guards, snapshots, constructions, projections;
static int snapshot_active, commit_active, invocation, injected, invalid;
static unsigned removed, attempts, close_failures;
static int fail_close, cleanup_descriptor = -1;

static int same(const char *left, const char *right) {
    while (*left && *left == *right) { ++left; ++right; }
    return *left == *right;
}

static int bytes_equal(const unsigned char *bytes, uint64_t length, const char *text) {
    uint64_t index = 0;
    while (text[index]) {
        if (index == length || bytes[index] != (unsigned char)text[index]) return 0;
        ++index;
    }
    return index == length;
}

static unsigned descriptor_reference(void) {
    int saved_error = *ERROR_ADDRESS();
    unsigned count = 0;
    for (int descriptor = 0; descriptor < 512; ++descriptor) {
#if !defined(__APPLE__)
        ++reference_probes;
#endif
        if (fcntl(descriptor, 1) >= 0) ++count;
    }
    *ERROR_ADDRESS() = saved_error;
    return count;
}

static unsigned descriptors(void) {
#if defined(__APPLE__)
    return descriptor_reference();
#else
    int saved_error = *ERROR_ADDRESS();
    unsigned count = 0, own_seen = 0;
    ++descriptor_measurements;
    FixtureDirectory *directory = opendir("/proc/self/fd");
    if (!directory) { invalid = 10; *ERROR_ADDRESS() = saved_error; return 0; }
    int own = dirfd(directory);
    if (own < 0) invalid = 10;
    while (own >= 0) {
        *ERROR_ADDRESS() = 0;
        struct FixtureDirectoryEntry *entry = readdir(directory);
        if (!entry) {
            if (*ERROR_ADDRESS()) invalid = 10;
            break;
        }
        ++descriptor_entries;
        size_t header = offsetof(struct FixtureDirectoryEntry, name);
        if (entry->record_length < header + 2 || entry->record_length > sizeof(*entry)
            || entry->record_length % 8) { invalid = 10; break; }
        size_t capacity = entry->record_length - header, length = 0;
        if (capacity > sizeof(entry->name)) { invalid = 10; break; }
        while (length < capacity && entry->name[length]) ++length;
        if (!length || length == capacity) { invalid = 10; break; }
        if ((length == 1 && entry->name[0] == '.')
            || (length == 2 && entry->name[0] == '.' && entry->name[1] == '.')) continue;
        if (length > 1 && entry->name[0] == '0') { invalid = 10; break; }
        unsigned descriptor = 0;
        size_t index = 0;
        for (; index < length; ++index) {
            unsigned digit = (unsigned char)entry->name[index] - '0';
            if (digit > 9 || descriptor > (2147483647U - digit) / 10) break;
            descriptor = 10 * descriptor + digit;
        }
        if (index != length) { invalid = 10; break; }
        if (descriptor == (unsigned)own) ++own_seen;
        else if (++count > 512) { invalid = 10; break; }
    }
    if (closedir(directory)) invalid = 10;
    if (own_seen != 1) invalid = 10;
    *ERROR_ADDRESS() = saved_error;
    return count;
#endif
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

static int descriptor_parent(int descriptor, const char *suffix) {
    char path[4096];
#if defined(__APPLE__)
    if (fcntl(descriptor, 50, path)) return 0;
#else
    char proc[64];
    int length = snprintf(proc, sizeof(proc), "/proc/self/fd/%d", descriptor);
    if (length < 0 || (size_t)length >= sizeof(proc)) return 0;
    long count = readlink(proc, path, sizeof(path) - 1);
    if (count < 0) return 0;
    path[count] = 0;
#endif
    size_t path_length = 0, suffix_length = 0;
    while (path[path_length]) ++path_length;
    while (suffix[suffix_length]) ++suffix_length;
    return path_length >= suffix_length && same(path + path_length - suffix_length, suffix);
}

int close(int descriptor) {
    int (*native_close)(int) = (int (*)(int))dlsym((void *)-1, "close");
    if (!native_close) { *ERROR_ADDRESS() = 5; return -1; }
    int selected = fail_close && (cleanup_descriptor < 0 || descriptor == cleanup_descriptor);
    int result = native_close(descriptor);
    if (selected && result == 0) {
        fail_close = 0;
        cleanup_descriptor = -1;
        ++close_failures;
        *ERROR_ADDRESS() = 5;
        return -1;
    }
    return result;
}

int unlinkat(int parent, const char *name, int flags) {
    int (*native_unlink)(int, const char *, int) = (int (*)(int, const char *, int))dlsym((void *)-1, "unlinkat");
    if (!native_unlink) { *ERROR_ADDRESS() = 5; return -1; }
    static const char *names[] = {"a", "b", "a-dir", "b-file", "a", "c-dir", "tree"};
    static const char *parents[] = {"/parent/tree/a-dir", "/parent/tree/a-dir", "/parent/tree", "/parent/tree", "/parent/tree/c-dir", "/parent/tree", "/parent"};
    unsigned total = LO_CASE == 33 ? 161 : LO_CASE == 34 ? 321 : 7;
    if (!commit_active || guards != total || attempts >= total) invalid = 1;
    if (LO_CASE == 33 || LO_CASE == 34) {
        if ((attempts == total - 1) != same(name, "tree")) invalid = 1;
        if (attempts == total - 1 && !descriptor_parent(parent, "/parent")) invalid = 1;
    } else if (attempts < 7 && (!same(name, names[attempts]) || !descriptor_parent(parent, parents[attempts]))) invalid = 1;
    ++attempts;
    if ((LO_CASE == 1 || LO_CASE == 4) && attempts == 2) {
        if (LO_CASE == 4) { fail_close = 1; cleanup_descriptor = -1; }
        *ERROR_ADDRESS() = 5;
        return -1;
    }
    int result = native_unlink(parent, name, flags);
    if (result == 0) {
        ++removed;
        if ((LO_CASE == 2 && removed == 1) || (LO_CASE == 25 && removed == 7)) lo_context_cancel(active);
        if (LO_CASE == 3 && removed == 7) { fail_close = 1; cleanup_descriptor = parent; }
        if (LO_CASE == 6 && removed == 1 && file("parent/tree/a-dir/unexpected")) invalid = 2;
    }
    return result;
}

static int observer(uintptr_t user, uint32_t phase, const unsigned char *root, uint64_t root_length,
    const unsigned char *name, uint64_t name_length, LoFileMetadata *metadata) {
    (void)phase;
    if (user != 34 || !root || !root_length || !metadata) { invalid = 3; return -1; }
#if !defined(__APPLE__)
    ++metadata_observations;
#endif
    unsigned current = descriptors();
    if (current > peak) peak = current;
    int alias = bytes_equal(name, name_length, "b");
    int leaf = alias || bytes_equal(name, name_length, "b-file");
    int parent = bytes_equal(name, name_length, "a-dir");
    /* Native root observations can use an empty name with their retained path. */
    if (!name_length) {
        static const char suffix[] = "/a-dir";
        if (root_length >= sizeof(suffix) - 1) parent = bytes_equal(root + root_length - (sizeof(suffix) - 1), sizeof(suffix) - 1, suffix);
    }
    if (snapshot_active && (LO_CASE == 16 || LO_CASE == 17 || LO_CASE == 18 || LO_CASE == 26) && bytes_equal(name, name_length, "a")) {
        injected = 1;
        if (LO_CASE == 16) lo_context_cancel(active);
        if (LO_CASE == 17) return 5;
        if (LO_CASE == 18) return 2;
        if (LO_CASE == 26) metadata->allocated_blocks = -1;
    }
    if (commit_active && LO_CASE == 8 && leaf) { ++metadata->allocated_blocks; injected = 1; }
    if (commit_active && LO_CASE == 9 && leaf) { ++metadata->accessed_sec; injected = 1; }
    if (commit_active && LO_CASE == 10 && parent) { ++metadata->accessed_sec; injected = 1; }
    if (commit_active && removed == 1) {
        if (LO_CASE == 5 && parent) { ++metadata->owner_uid; injected = 1; }
        if (alias) {
            if (LO_CASE == 7) { ++metadata->link_count; injected = 1; }
            if (LO_CASE == 11) { ++metadata->size_bytes; injected = 1; }
            if (LO_CASE == 12) { ++metadata->allocated_blocks; injected = 1; }
            if (LO_CASE == 13) { ++metadata->modified_sec; injected = 1; }
            if (LO_CASE == 14) { ++metadata->accessed_sec; injected = 1; }
            if (LO_CASE == 15) { ++metadata->owner_uid; injected = 1; }
        }
    }
    return 0;
}

void lo_flow_1(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    if (removed) invalid = 4;
    ++guards;
    lo_f034_guard(context, arguments, count, site, result);
}

void lo_f034_snapshot(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    if ((LO_CASE == 19 || LO_CASE == 20) && !injected) {
        injected = 1;
        if (rename("parent/tree", "parent/held")) invalid = 5;
        if (LO_CASE == 20 && rename("replacement", "parent/tree")) invalid = 6;
    }
    snapshot_active = 1;
    lo_op_File_2ETree_2Esnapshot(context, arguments, count, site, result);
    snapshot_active = 0;
    ++snapshots;
}

void lo_f034_checked(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    unsigned before = guards;
    lo_op_File_2ETree_2ERemove_2Echecked(context, arguments, count, site, result);
    if (guards != before || removed) invalid = 7;
    if ((LO_CASE == 21 || LO_CASE == 22) && constructions == 0 && !result->status) {
        retained = result->value;
        lo_value_retain(retained);
    }
    ++constructions;
}

void lo_f034_commit(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    const Value *replacement[] = {retained};
    if ((LO_CASE == 21 && invocation) || LO_CASE == 22) arguments = replacement;
    if (LO_CASE == 21 && !invocation) lo_context_cancel(context);
    commit_active = 1;
    lo_commit(context, arguments, count, site, result);
    commit_active = 0;
}

void lo_f034_project(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    const Value *replacement[] = {diagnostic, count > 1 ? arguments[1] : 0};
    if (LO_CASE == 23 && projections == 1) arguments = replacement;
    lo_project(context, arguments, count, site, result);
    if (LO_CASE == 23 && projections == 0 && !result->status) {
        diagnostic = result->value;
        lo_value_retain(diagnostic);
    }
    ++projections;
}

void lo_entry(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    active = context;
    unsigned current = descriptors();
    if (current > admitted) admitted = current;
    if (!lo_context_file_metadata_observer((LoContext *)context, observer, 34)) invalid = 8;
    lo_f034_entry(context, arguments, count, site, result);
    active = 0;
}

static int setup(void) {
    if (directory("parent") || directory("parent/tree") || directory("replacement")) return 1;
    if (LO_CASE == 27 || LO_CASE == 28 || LO_CASE == 33 || LO_CASE == 34) {
        char parent[1024] = "parent/tree";
        for (unsigned index = 0; index < 160; ++index) {
            char path[1024];
            if (LO_CASE == 27 || LO_CASE == 33) snprintf(path, sizeof(path), "parent/tree/child-%03u", index);
            else {
                snprintf(path, sizeof(path), "%s/z", parent);
                if (directory(path)) return 2;
                snprintf(path, sizeof(path), "%s/a", parent);
            }
            if (directory(path)) return 3;
            if (LO_CASE == 28 || LO_CASE == 34) snprintf(parent, sizeof(parent), "%s", path);
        }
        return 0;
    }
    if (directory("parent/tree/a-dir") || directory("parent/tree/c-dir")
        || file("parent/tree/a-dir/a") || file("parent/tree/c-dir/a")) return 4;
    if (LO_CASE == 7 || (LO_CASE >= 11 && LO_CASE <= 15)) {
        if (link("parent/tree/a-dir/a", "parent/tree/a-dir/b")) return 5;
    } else if (file("parent/tree/a-dir/b")) return 6;
    if (LO_CASE == 29) {
        if (file("lock") || link("lock", "parent/tree/b-file")) return 7;
    } else if (LO_CASE == 32) {
        if (link("parent/tree/a-dir/a", "parent/tree/b-file")) return 8;
    } else if (file("parent/tree/b-file")) return 8;
    return 0;
}

int main(void) {
    int setup_status = setup();
    if (setup_status) return 10 + setup_status;
    int saved = fcntl(2, 0, 3);
    if (saved < 0 || dup2(1, 2) < 0) return 20;
#if !defined(__APPLE__)
    unsigned before_sparse = descriptors();
    int sparse = fcntl(saved, 0, 384);
    if (sparse < 384 || sparse >= 512) {
        if (sparse >= 0 && close(sparse)) invalid = 10;
        return 24;
    }
    *ERROR_ADDRESS() = 0;
    if (fcntl(sparse - 1, 1) >= 0 || *ERROR_ADDRESS() != 9) invalid = 10;
#endif
    unsigned baseline = descriptors();
#if !defined(__APPLE__)
    if (baseline == before_sparse + 1) ++sparse_checks; else invalid = 10;
    if (baseline == descriptor_reference()) ++reference_matches; else invalid = 10;
#endif
    char working_directory[4096], lock_path[4096];
    if (!getcwd(working_directory, sizeof(working_directory))) return 22;
    int lock_length = snprintf(lock_path, sizeof(lock_path), "%s%slock", working_directory, same(working_directory, "/") ? "" : "/");
    if (lock_length < 0 || (size_t)lock_length >= sizeof(lock_path)) return 23;
    const char *arguments[] = {"tree-fixture", "run", "--format", "json", "--observe-tree-root", "parent", "--remove-tree-root", "parent/tree", "--max-steps", LO_CASE == 31 ? "2000" : "10000000", "--max-live-bytes", "16777216", "--max-file-directories", "4", "--max-file-entries", LO_CASE == 30 ? "6" : "0", "--lock-file", lock_path, "--", lock_path};
    int status = lo_cli_main(LO_CASE == 29 ? 20 : 16, arguments, 0);
    if (LO_CASE == 21) {
        if (status != 2 || !retained) invalid = 9;
        invocation = 1;
        status = lo_cli_main(16, arguments, 0);
    }
    lo_value_release(retained);
    lo_value_release(diagnostic);
    unsigned after = descriptors();
#if !defined(__APPLE__)
    if (after == descriptor_reference()) ++reference_matches; else invalid = 10;
    if (close(sparse)) invalid = 10;
    if (descriptors() + 1 == after) ++sparse_checks; else invalid = 10;
    if (reference_matches != 2 || sparse_checks != 2 || reference_probes != 1024) invalid = 10;
    if (LO_CASE == 34) {
        char evidence[256];
        int length = snprintf(evidence, sizeof(evidence), "F034 descriptor measurement: observations=%u measurements=%u entries=%u reference_probes=%u high_fd=%d reference_matches=%u sparse_checks=%u\n", metadata_observations, descriptor_measurements, descriptor_entries, reference_probes, sparse, reference_matches, sparse_checks);
        if (length <= 0 || (size_t)length >= sizeof(evidence)
            || write(1, evidence, (size_t)length) != length) invalid = 10;
    }
#endif
    if (dup2(saved, 2) < 0 || close(saved)) return 21;
    int expected = LO_CASE == 0 || LO_CASE == 10 || LO_CASE == 27 || LO_CASE == 28 || LO_CASE >= 32 ? 0
        : LO_CASE == 2 || LO_CASE == 3 || LO_CASE == 16 || LO_CASE == 21 || LO_CASE == 22
        || LO_CASE == 25 || LO_CASE == 26 || LO_CASE >= 29 ? 2 : 1;
    unsigned completed = LO_CASE == 33 ? 161 : LO_CASE == 34 ? 321
        : LO_CASE == 0 || LO_CASE == 3 || LO_CASE == 10 || LO_CASE == 25 || LO_CASE == 32 || LO_CASE == 35 || LO_CASE == 36 ? 7
        : LO_CASE == 1 || LO_CASE == 2 || LO_CASE == 4 || LO_CASE == 5 || LO_CASE == 6 || LO_CASE == 7
        || (LO_CASE >= 11 && LO_CASE <= 15) ? 1 : 0;
    if (invalid || status != expected || removed != completed) {
        char evidence[160];
        int length = snprintf(evidence, sizeof(evidence), "F034 fixture: invalid=%d status=%d expected=%d removed=%u expected=%u guards=%u\n", invalid, status, expected, removed, completed, guards);
        if (length > 0) write(1, evidence, (size_t)length);
        return 30;
    }
    if (after != baseline || peak > admitted + 12) return 31;
    if ((LO_CASE == 3 || LO_CASE == 4) && close_failures != 1) return 32;
    if (lo_runtime_live_contexts() || lo_runtime_live_values()) return 33;
    if ((LO_CASE == 0 || LO_CASE == 10 || LO_CASE >= 32) && guards != completed) return 34;
    if (completed >= 7 && access("parent/tree", 0) == 0) return 35;
    if (completed == 1 && (access("parent/tree/a-dir/a", 0) == 0 || access("parent/tree/a-dir/b", 0) != 0 || access("parent/tree/c-dir/a", 0) != 0)) return 36;
    if (!completed && LO_CASE != 19 && LO_CASE != 20 && LO_CASE != 27 && LO_CASE != 28
        && (access("parent/tree/a-dir/a", 0) != 0 || access("parent/tree/c-dir/a", 0) != 0)) return 37;
    if (((LO_CASE >= 5 && LO_CASE <= 15 && LO_CASE != 6) || (LO_CASE >= 16 && LO_CASE <= 20) || LO_CASE == 26) && !injected) return 38;
    return 0;
}
