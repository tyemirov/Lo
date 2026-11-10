/* Appended to the controlled provider harness. Only descriptor duplication and
 * cleanup create the condition; both subsequent lock requests use real peers. */
#include "lo_runtime.h"
#include "lo_process.h"
#include <dlfcn.h>
#include <stdarg.h>
#include <sys/file.h>

#define SECOND_LOCK @F036_SECOND_LOCK@
#define LATER_LOCK @F036_LATER_LOCK@

extern int lo_boolean_value(const Value *);
extern void lo_f036_capture_entry(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
extern void lo_flow_0(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
extern void lo_flow_1(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
static int held[2] = {-1, -1}, duplicate = -1;
static int duplicates, failed_dup, failed_close, later_acquired, invocation, invalid;

static int capture_path(int descriptor, const char *expected) {
    int (*original)(int, int, ...) = dlsym(RTLD_NEXT, "fcntl");
    char path[1024];
    return original && !original(descriptor, F_GETPATH, path) && !strcmp(path, expected);
}
int flock(int descriptor, int operation) {
    int (*original)(int, int) = dlsym(RTLD_NEXT, "flock");
    if (!original) _exit(81);
    int result = original(descriptor, operation);
    if (!result && operation == (LOCK_EX | LOCK_NB)) {
        if (capture_path(descriptor, FIXTURE_ROOT "/lock")) held[0] = descriptor;
        if (capture_path(descriptor, SECOND_LOCK)) held[1] = descriptor;
        if (capture_path(descriptor, LATER_LOCK)) ++later_acquired;
    }
    return result;
}
int fcntl(int descriptor, int command, ...) {
    int (*original)(int, int, ...) = dlsym(RTLD_NEXT, "fcntl");
    if (!original) _exit(82);
    if (command == F_GETFD || command == F_GETFL || command == F_GETOWN)
        return original(descriptor, command);
    va_list arguments;
    va_start(arguments, command);
    if (command == F_GETPATH) {
        void *path = va_arg(arguments, void *);
        va_end(arguments);
        return original(descriptor, command, path);
    }
    int argument = va_arg(arguments, int);
    va_end(arguments);
    if (command == F_DUPFD_CLOEXEC && held[0] >= 0 && held[1] >= 0
        && (descriptor == held[0] || descriptor == held[1]) && !failed_dup) {
        if (++duplicates == 2) { failed_dup = 1; errno = EMFILE; return -1; }
        duplicate = original(descriptor, command, argument);
        return duplicate;
    }
    return original(descriptor, command, argument);
}
int close(int descriptor) {
    int (*original)(int) = dlsym(RTLD_NEXT, "close");
    if (!original) _exit(83);
    int result = original(descriptor);
    if (LO_CASE == 1 && failed_dup && descriptor == duplicate && !failed_close) {
        ++failed_close;
        if (result) invalid = 1;
        errno = EIO;
        return -1;
    }
    return result;
}
void lo_entry(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    if (invocation == 1) { lo_flow_0(context, arguments, count, site, result); return; }
    if (invocation == 2) { lo_flow_1(context, arguments, count, site, result); return; }
    lo_f036_capture_entry(context, arguments, count, site, result);
    if (result->status != LO_RESULT_HOST_FAILURE || result->value || !failed_dup) { invalid = 2; return; }
    for (invocation = 1; invocation <= 2; ++invocation) {
        Context *peer = lo_context_process_peer(context);
        if (!peer) { invalid = 3; return; }
        ResultSlot probe = {99, 99, 0};
        lo_context_run(peer, &probe);
        if (probe.status != LO_RESULT_OK || !probe.value) invalid = 4;
        else if (lo_boolean_value(probe.value) != (LO_CASE == 1)) invalid = 10 + invocation;
        if (probe.value) lo_value_release(probe.value);
        lo_context_free(peer);
    }
    fixture_log("capture-probed\n");
}
int main(int argc, char **argv, char **environment) {
    int status = f036_provider_main(argc, argv, environment);
    if (status != 2 || invalid || duplicates != 2 || failed_dup != 1
        || failed_close != (LO_CASE == 1) || later_acquired != (LO_CASE == 0))
        return invalid ? 40 + invalid : 60;
    return 0;
}
