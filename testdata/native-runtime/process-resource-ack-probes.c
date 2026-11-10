#include "lo_runtime.h"
#include "lo_process.h"
extern int lo_boolean_value(const Value *);
extern void lo_resource_pressure_entry(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
extern void lo_flow_0(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
extern void lo_flow_1(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
static int pressure_probe_invocation, pressure_probe_error, pressure_probe_complete;

void lo_entry(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    if (pressure_probe_invocation == 1) { lo_flow_0(context, arguments, count, site, result); return; }
    if (pressure_probe_invocation == 2) { lo_flow_1(context, arguments, count, site, result); return; }
    lo_resource_pressure_entry(context, arguments, count, site, result);
    if (result->status != LO_RESULT_HOST_FAILURE || result->value) { pressure_probe_error = 1; return; }
    for (pressure_probe_invocation = 1; pressure_probe_invocation <= 2; ++pressure_probe_invocation) {
        Context *peer = lo_context_process_peer(context);
        if (!peer) { pressure_probe_error = 2; return; }
        ResultSlot probe = {99, 99, 0};
        lo_context_run(peer, &probe);
        if (probe.status != LO_RESULT_OK || !probe.value || !lo_boolean_value(probe.value)) pressure_probe_error = 3;
        if (probe.value) lo_value_release(probe.value);
        lo_context_free(peer);
    }
    pressure_probe_complete = 1;
    pressure_log("resource-ack-locks-frozen\n");
}
int main(int argc, char **argv, char **environment) {
    int status = pressure_provider_main(argc, argv, environment);
    return status == 2 && pressure_probe_complete && !pressure_probe_error ? 0 : 120 + pressure_probe_error;
}
