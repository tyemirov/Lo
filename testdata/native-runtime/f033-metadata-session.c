#include "lo_runtime.h"
#include "lo_file_observer.h"
#include "lo_process.h"

extern uint64_t lo_runtime_live_contexts(void), lo_runtime_live_values(void);
extern int lo_cli_main(int, const char **, const char **);
extern void lo_f033_entry(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
static int invocation, observed, invalid;

static int observe(uintptr_t user, uint32_t phase, const unsigned char *root, uint64_t root_length,
    const unsigned char *name, uint64_t name_length, LoFileMetadata *metadata) {
    (void)user; (void)phase; (void)root; (void)root_length;
    (void)name; (void)name_length;
    ++observed;
    metadata->owner_uid = 777;
    return 0;
}

void lo_entry(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    if (LO_CASE != 1 || invocation++) {
        lo_f033_entry(context, arguments, count, site, result);
        return;
    }
    Context *peer = lo_context_process_peer(context);
    if (!peer) { invalid = 1; return; }
    if (!lo_context_file_metadata_observer((LoContext *)context, observe, 33)) invalid = 2;
    lo_context_run(peer, result);
    lo_context_free(peer);
}

int main(void) {
    if (LO_CASE == 1) {
        const char *arguments[] = {"observer", "run", "--format", "json", "--list-root", "."};
        int status = lo_cli_main(6, arguments, 0);
        if (status || invalid || observed != 1) return 20 + invalid;
        return lo_runtime_live_contexts() || lo_runtime_live_values() ? 23 : 0;
    }
    const ContextOptions options = {100000, 16ULL << 20, 64ULL << 20, 0, 0, 0, 128};
    Context *control = lo_context_new(&options);
    if (!control) return 10;
    int early = lo_context_file_metadata_observer((LoContext *)control, observe, 33);
    lo_context_free(control);
    if (early != 1) return 11;

    Context *original = lo_context_new(&options);
    if (!original) return 12;
    Context *peer = lo_context_process_peer(original);
    if (!peer) { lo_context_free(original); return 13; }
    ResultSlot result = {99, 99, 0};
    lo_context_run(peer, &result);
    int success = result.status == LO_RESULT_OK && result.value;
    lo_value_release(result.value);
    lo_context_free(peer);
    int late = lo_context_file_metadata_observer((LoContext *)original, observe, 33);
    lo_context_free(original);
    if (!success) return 14;
    if (late) return 15;
    return lo_runtime_live_contexts() || lo_runtime_live_values() ? 16 : 0;
}
