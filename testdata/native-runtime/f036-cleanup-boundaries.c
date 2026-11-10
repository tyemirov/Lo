/* Appended to the real controlled-provider host with LO_CASE=0. */
#include "lo_runtime.h"
#include "lo_process.h"
#include <stdarg.h>
#include <sys/file.h>

#define BOUNDARY_CASE @F036_BOUNDARY_CASE@
#define LATER_LOCK @F036_LATER_LOCK@
enum { BAD_RECEIPT, CHANNEL_CLOSE, THREAD_CLEAN, THREAD_CLOSE, CHANNEL_CLEAN, INHERITANCE_CLOSE };
extern int lo_boolean_value(const Value *);
extern void lo_f036_boundary_entry(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
extern void lo_flow_0(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
extern void lo_flow_1(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
static int caller, supervisor, held = -1, duplicate = -1, channel = -1;
static int captured, duplicate_closed, failed_setup, failed_close, failed_thread;
static int later_acquired, invocation, invalid;

int lo_test_host_init(int argc, char **argv, char **environment) {
    caller = argc > 1 && !strcmp(argv[1], "run");
    supervisor = argc == 2 && !strcmp(argv[1], "--lo-process-supervisor");
    return f036_boundary_provider_init(argc, argv, environment);
}
static int boundary_path(int descriptor, const char *expected) {
    int (*original)(int, int, ...) = dlsym(RTLD_NEXT, "fcntl");
    char path[1024];
    return original && !original(descriptor, F_GETPATH, path) && !strcmp(path, expected);
}
static int descriptor_count(void) {
    int (*original)(int, int, ...) = dlsym(RTLD_NEXT, "fcntl");
    int count = 0;
    if (!original) _exit(80);
    for (int descriptor = 0; descriptor < 512; ++descriptor)
        if (original(descriptor, F_GETFD) >= 0) ++count;
    return count;
}
int flock(int descriptor, int operation) {
    int (*original)(int, int) = dlsym(RTLD_NEXT, "flock");
    if (!original) _exit(81);
    int result = original(descriptor, operation);
    if (caller && !result && operation == (LOCK_EX | LOCK_NB)) {
        if (boundary_path(descriptor, FIXTURE_ROOT "/lock")) held = descriptor;
        if (boundary_path(descriptor, LATER_LOCK)) ++later_acquired;
    }
    return result;
}
int fcntl(int descriptor, int command, ...) {
    int (*original)(int, int, ...) = dlsym(RTLD_NEXT, "fcntl");
    if (!original) _exit(82);
    if (caller && descriptor == channel && !failed_setup
        && (((BOUNDARY_CASE == CHANNEL_CLOSE || BOUNDARY_CASE == CHANNEL_CLEAN) && command == F_GETFL)
            || (BOUNDARY_CASE == INHERITANCE_CLOSE && command == F_SETFD))) {
        failed_setup = 1;
        fixture_log("boundary-setup-failed\n");
        errno = EIO;
        return -1;
    }
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
    int result = original(descriptor, command, argument);
    if (caller && descriptor == held && command == F_DUPFD_CLOEXEC && result >= 0 && !captured) {
        duplicate = result;
        captured = 1;
    }
    return result;
}
int socketpair(int domain, int type, int protocol, int descriptors[2]) {
    int (*original)(int, int, int, int *) = dlsym(RTLD_NEXT, "socketpair");
    if (!original) _exit(83);
    int result = original(domain, type, protocol, descriptors);
    if (caller && captured && !result && channel < 0 && domain == AF_UNIX && type == SOCK_STREAM)
        channel = descriptors[0];
    return result;
}
int pthread_create(pthread_t *thread, const pthread_attr_t *attributes, void *(*entry)(void *), void *argument) {
    int (*original)(pthread_t *, const pthread_attr_t *, void *(*)(void *), void *) = dlsym(RTLD_NEXT, "pthread_create");
    if (!original) _exit(84);
    if (caller && captured && !failed_thread && (BOUNDARY_CASE == THREAD_CLEAN || BOUNDARY_CASE == THREAD_CLOSE)) {
        failed_thread = 1;
        fixture_log("boundary-thread-failed\n");
        return EAGAIN;
    }
    return original(thread, attributes, entry, argument);
}
int close(int descriptor) {
    int (*original)(int) = dlsym(RTLD_NEXT, "close");
    if (!original) _exit(85);
    int result = original(descriptor);
    if (caller && captured && descriptor == duplicate && !duplicate_closed) {
        duplicate_closed = 1;
        if (result) invalid = 1;
    }
    if (caller && !failed_close
        && (((BOUNDARY_CASE == CHANNEL_CLOSE || BOUNDARY_CASE == INHERITANCE_CLOSE)
                && failed_setup && descriptor == channel)
            || (BOUNDARY_CASE == THREAD_CLOSE && failed_thread && descriptor == duplicate))) {
        failed_close = 1;
        if (result) invalid = 2;
        fixture_log("boundary-close-failed\n");
        errno = EIO;
        return -1;
    }
    return result;
}
ssize_t sendmsg(int descriptor, const struct msghdr *message, int flags) {
    ssize_t (*original)(int, const struct msghdr *, int) = dlsym(RTLD_NEXT, "sendmsg");
    if (!original) _exit(86);
    if (supervisor && BOUNDARY_CASE == BAD_RECEIPT && message->msg_iovlen == 1) {
        const struct iovec *vector = message->msg_iov;
        const unsigned char *bytes = vector->iov_base;
        if (vector->iov_len >= 32 && !memcmp(bytes, "LOS1", 4) && bytes[4] == 0 && bytes[5] == 13) {
            /* Keep the closed frame shape, but forge one confirmed stdout byte.
             * Field ranges remain valid, so the canonical receipt check matters. */
            if (vector->iov_len != 88 || bytes[11] != 56 || message->msg_controllen) _exit(87);
            unsigned char changed[88];
            memcpy(changed, bytes, sizeof(changed));
            changed[75] ^= 1;
            struct iovec copied_vector = {changed, sizeof(changed)};
            struct msghdr copied_message = *message;
            copied_message.msg_iov = &copied_vector;
            ssize_t result = original(descriptor, &copied_message, flags);
            if (result == (ssize_t)sizeof(changed)) fixture_log("boundary-receipt-corrupted\n");
            else if (result >= 0) _exit(88);
            return result;
        }
    }
    return original(descriptor, message, flags);
}
static int frozen_expected(void) {
    return BOUNDARY_CASE != THREAD_CLEAN && BOUNDARY_CASE != CHANNEL_CLEAN;
}
void lo_entry(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    if (invocation == 1) { lo_flow_0(context, arguments, count, site, result); return; }
    if (invocation == 2) { lo_flow_1(context, arguments, count, site, result); return; }
    int baseline = descriptor_count();
    lo_f036_boundary_entry(context, arguments, count, site, result);
    if (result->status != LO_RESULT_HOST_FAILURE || result->value || !captured) { invalid = 3; return; }
    /* Before peers run, the only added descriptor in the clean no-thread case
     * is Main's live session lock. Every captured duplicate must be released. */
    if (BOUNDARY_CASE == THREAD_CLEAN && descriptor_count() != baseline + 1) invalid = 4;
    for (invocation = 1; invocation <= 2; ++invocation) {
        Context *peer = lo_context_process_peer(context);
        if (!peer) { invalid = 5; return; }
        ResultSlot probe = {99, 99, 0};
        lo_context_run(peer, &probe);
        if (probe.status != LO_RESULT_OK || !probe.value) invalid = 6;
        else if (lo_boolean_value(probe.value) != frozen_expected()) invalid = 10 + invocation;
        if (probe.value) lo_value_release(probe.value);
        lo_context_free(peer);
    }
    fixture_log("boundary-probed\n");
}
int main(int argc, char **argv, char **environment) {
    int status = f036_boundary_provider_main(argc, argv, environment);
    int setup_expected = BOUNDARY_CASE == CHANNEL_CLOSE || BOUNDARY_CASE == CHANNEL_CLEAN || BOUNDARY_CASE == INHERITANCE_CLOSE;
    int thread_expected = BOUNDARY_CASE == THREAD_CLEAN || BOUNDARY_CASE == THREAD_CLOSE;
    int close_expected = BOUNDARY_CASE == CHANNEL_CLOSE || BOUNDARY_CASE == THREAD_CLOSE || BOUNDARY_CASE == INHERITANCE_CLOSE;
    int duplicate_expected = thread_expected || BOUNDARY_CASE == CHANNEL_CLEAN;
    if (status != 2 || invalid || !captured || failed_setup != setup_expected || failed_thread != thread_expected
        || failed_close != close_expected || duplicate_closed != duplicate_expected
        || later_acquired != !frozen_expected()) return invalid ? 40 + invalid : 60;
    return 0;
}
