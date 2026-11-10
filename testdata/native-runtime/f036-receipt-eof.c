#include "lo_runtime.h"
extern void lo_eof_entry(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
static int eof_supervisor, eof_receipt;

void lo_entry(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    lo_eof_entry(context, arguments, count, site, result);
    fixture_log("compiled-entry-returned\n");
}

int lo_test_host_init(int argc, char **argv, char **environment) {
    eof_supervisor = argc == 2 && !strcmp(argv[1], "--lo-process-supervisor");
    return eof_provider_init(argc, argv, environment);
}

ssize_t sendmsg(int descriptor, const struct msghdr *message, int flags) {
    __typeof__(&sendmsg) original = (__typeof__(&sendmsg))dlsym(RTLD_NEXT, "sendmsg");
    if (!original) _exit(81);
    ssize_t result = original(descriptor, message, flags);
    if (eof_supervisor && descriptor == 3 && result >= 32 && message->msg_iovlen == 1) {
        const unsigned char *bytes = message->msg_iov[0].iov_base;
        if (!memcmp(bytes, "LOS1", 4) && bytes[4] == 0 && bytes[5] == 13) eof_receipt = 1;
    }
    return result;
}

int close(int descriptor) {
    __typeof__(&close) original = (__typeof__(&close))dlsym(RTLD_NEXT, "close");
    if (!original) _exit(81);
    if (eof_supervisor && descriptor == 3 && eof_receipt) {
        eof_receipt = 0;
        fixture_log("final-receipt-before-held-eof\n");
        struct timespec remaining = {0, 600000000};
        while (nanosleep(&remaining, &remaining) && errno == EINTR) {}
        fixture_log("held-eof-released\n");
    }
    return original(descriptor);
}
