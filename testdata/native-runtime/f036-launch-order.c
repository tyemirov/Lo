#include @ORDER_PROTOCOL@
#define ORDER_READY_READ @ORDER_READY_READ@
#define ORDER_READY_WRITE @ORDER_READY_WRITE@

static int order_launcher, order_supervisor, order_status = -1;
static int order_pending;
static LoProcessOwnershipEvent order_event;
static unsigned char order_path[1025];

static int order_spawn_denied(void) { return order_supervisor && LO_CASE == 26; }

static void order_defer(struct fixture_owner *owner, LoProcessOwnershipEvent *event) {
    if (LO_CASE == 25) {
        if (order_pending || event->path_size >= sizeof(order_path)) _exit(81);
        order_event = *event;
        memcpy(order_path, event->path, event->path_size);
        order_event.path = order_path;
        order_pending = 1;
        fixture_log("E-pending\n");
    } else {
        fixture_emit(owner, event, 1);
        fixture_log("E-launcher\n");
    }
}

static void order_release(struct fixture_owner *owner) {
    if (order_pending && !access(FIXTURE_RELEASE, F_OK)) {
        order_pending = 0;
        fixture_emit(owner, &order_event, 1);
        fixture_log("E-delayed\n");
    }
}

int lo_test_host_init(int argc, char **argv, char **environment) {
    order_launcher = argc == 2 && !strcmp(argv[1], "--lo-process-launcher");
    order_supervisor = argc == 2 && !strcmp(argv[1], "--lo-process-supervisor");
    return order_provider_init(argc, argv, environment);
}

ssize_t write(int descriptor, const void *bytes, size_t size) {
    ssize_t (*original)(int, const void *, size_t) = dlsym(RTLD_NEXT, "write");
    if (!original) _exit(82);
    if (order_launcher && descriptor == STATUS && size == PROCESS_READY_BYTES
        && ((const unsigned char *)bytes)[0] == 'R') {
        if (fixture_touch(ORDER_READY_WRITE, "ready")) _exit(83);
        if (LO_CASE == 24) { errno = EIO; return -1; }
    }
    return original(descriptor, bytes, size);
}

ssize_t read(int descriptor, void *bytes, size_t size) {
    ssize_t (*original)(int, void *, size_t) = dlsym(RTLD_NEXT, "read");
    if (!original) _exit(84);
    ssize_t result = original(descriptor, bytes, size);
    if (order_supervisor && LO_CASE == 25) {
        if (result == 6 && size == 6 && !memcmp(bytes, "R\0\0\0\0\0", 6)) order_status = descriptor;
        else if (descriptor == order_status && result == 32 && size == 32) {
            order_status = -1;
            if (fixture_touch(ORDER_READY_READ, "read")) _exit(85);
        }
    }
    return result;
}
