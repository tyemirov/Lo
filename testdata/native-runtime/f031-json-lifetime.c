#include "lo_runtime.h"

extern int lo_cli_main(int, const char **, const char **);
extern uint64_t lo_runtime_live_contexts(void), lo_runtime_live_values(void);
extern int fcntl(int, int, ...), dup2(int, int), close(int);
extern void lo_op_JSON_2Edecode(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
static unsigned calls;

void lo_f031_decode(Context *context, const Value **arguments, uint64_t count,
                    uint64_t site, ResultSlot *result) {
    ++calls;
    if (LO_CASE == 2) lo_context_cancel(context);
    lo_op_JSON_2Edecode(context, arguments, count, site, result);
}

int main(void) {
    int saved = fcntl(2, 0, 3);
    if (saved < 0 || dup2(1, 2) < 0) return 10;
    const char *arguments[] = {"json-lifetime", "run", "--format", "json",
        "--max-json-depth", "0", "--max-steps", "10000000"};
    int status = lo_cli_main(8, arguments, 0);
    if (dup2(saved, 2) < 0 || close(saved)) return 11;
    if (status != LO_CASE || calls != 1) return 12;
    if (lo_runtime_live_contexts() || lo_runtime_live_values()) return 13;
    return 0;
}
