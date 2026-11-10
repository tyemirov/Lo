#include "lo_runtime.h"
#include "lo_file_observer.h"
#include "lo_host_observer.h"

extern int lo_cli_main(int, const char **, const char **);
extern uint64_t lo_runtime_live_contexts(void), lo_runtime_live_values(void);
extern int rename(const char *, const char *), close(int), fcntl(int, int, ...), dup2(int, int);
extern int symlink(const char *, const char *);
extern char *mkdtemp(char *);
extern void lo_f033_entry(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);

static int invalid, replaced;
static Context *active;

static int directory(const char *path) {
    char temporary[] = "f033-XXXXXX";
    return !mkdtemp(temporary) || rename(temporary, path);
}

static unsigned descriptors(void) {
    unsigned count = 0;
    for (int descriptor = 0; descriptor < 256; ++descriptor) if (fcntl(descriptor, 1) >= 0) ++count;
    return count;
}

static void replace_ancestor(void) {
    ++replaced;
    if (rename("ancestor", "held") || rename("replacement", "ancestor")
        || rename("held/root", "ancestor/root")) invalid = 1;
}

static int observe(uintptr_t user, uint32_t phase, const unsigned char *root, uint64_t root_length,
    const unsigned char *name, uint64_t name_length, LoFileMetadata *metadata) {
    (void)name; (void)name_length;
    if (user != 33 || !root || !root_length || !metadata) { invalid = 2; return -1; }
    if ((LO_CASE == 15 && phase == LO_FILE_DISCOVERY) || LO_CASE == 16) {
        if (!replaced) replace_ancestor();
        else if (replaced == 1) {
            ++replaced;
            if (rename("ancestor/root", "held/root") || rename("ancestor", "replacement")
                || rename("held", "ancestor")) invalid = 7;
        }
    }
    if (!replaced && ((LO_CASE == 0 && phase == LO_FILE_DIRECTORY)
        || ((LO_CASE == 1 || LO_CASE == 2) && phase == LO_FILE_DISCOVERY))) replace_ancestor();
    if (!replaced && ((LO_CASE == 9 || LO_CASE == 13 || LO_CASE == 14)
        || ((LO_CASE == 10 || LO_CASE == 11) && phase == LO_FILE_DISCOVERY))) {
        replace_ancestor();
        if (LO_CASE == 13) return -1;
        if (LO_CASE == 14) { lo_context_cancel(active); return 0; }
        return 5;
    }
    return 0;
}

static int volume(uintptr_t user, const unsigned char *root, uint64_t length, LoVolumeMetadata *metadata) {
    if (user != 33 || !root || !length || !metadata || replaced) { invalid = 3; return -1; }
    replace_ancestor();
    return LO_CASE == 12 ? 5 : 0;
}

void lo_entry(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    active = context;
    if (LO_CASE == 4 || LO_CASE == 5 || LO_CASE == 8) {
        ++replaced;
        if (rename("ancestor", "held") || (LO_CASE == 8
            ? symlink("replacement", "ancestor") : rename("replacement", "ancestor"))) invalid = 5;
    }
    if (LO_CASE == 7) {
        ++replaced;
        if (rename("ancestor/root", "ancestor/old-root")) invalid = 6;
    }
    int installed = LO_CASE == 3 || LO_CASE == 12
        ? lo_context_volume_observer(context, volume, 33)
        : lo_context_file_metadata_observer((LoContext *)context, observe, 33);
    if (!installed) invalid = 4;
    lo_f033_entry(context, arguments, count, site, result);
}

int main(void) {
    if (directory("ancestor") || directory("replacement")) return 10;
    if (LO_CASE != 4 && LO_CASE != 6 && LO_CASE != 8
        && (directory("ancestor/root") || directory("ancestor/root/child"))) return 10;
    if (LO_CASE >= 15 && directory("ancestor/root/another")) return 10;
    int saved = fcntl(2, 0, 3);
    if (saved < 0 || dup2(1, 2) < 0) return 11;
    unsigned baseline = descriptors();
    const char *arguments[] = {"witnesses", "run", "--format", "json", "--list-root", "ancestor/root"};
    int status = lo_cli_main(6, arguments, 0);
    unsigned after = descriptors();
    if (dup2(saved, 2) < 0 || close(saved)) return 12;
    int expected = LO_CASE == 6 || LO_CASE == 7 ? 1 : 2;
    if (invalid || replaced != (LO_CASE == 6 ? 0 : 1) || status != expected) return 20 + invalid;
    if (after != baseline || lo_runtime_live_contexts() || lo_runtime_live_values()) return 30;
    return 0;
}
