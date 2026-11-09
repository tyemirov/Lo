#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

static int pressure_role, pressure_configured, pressure_received, pressure_descriptor = -1, pressure_injected, pressure_ack_changed;
static void pressure_bootstrap(int argc, char **argv) {
    pressure_role = argc == 2 && !strcmp(argv[1], "--lo-process-supervisor") ? 2 : 1;
    if (argc == 2 && (!strcmp(argv[1], "--lo-process-launcher") || !strcmp(argv[1], "--lo-console-writer"))) pressure_role = 0;
}
static void pressure_log(const char *text) {
    int saved = errno;
    int descriptor = open(@F036_PRESSURE_LOG@, O_WRONLY | O_APPEND | O_CLOEXEC);
    if (descriptor < 0) _exit(114);
    size_t size = strlen(text), offset = 0;
    while (offset < size) {
        ssize_t count = write(descriptor, text + offset, size - offset);
        if (count < 0 && errno == EINTR) continue;
        if (count <= 0) _exit(114);
        offset += (size_t)count;
    }
    if (close(descriptor)) _exit(114);
    errno = saved;
}
static int pressure_kind(const struct msghdr *message, ssize_t count) {
    if (!message || !message->msg_iov || message->msg_iovlen < 1
        || message->msg_iov[0].iov_len < 6 || count < 6) return -1;
    const unsigned char *bytes = message->msg_iov[0].iov_base;
    return memcmp(bytes, "LOS1", 4) ? -1 : ((int)bytes[4] << 8) | bytes[5];
}
ssize_t sendmsg(int descriptor, const struct msghdr *message, int flags) {
    __typeof__(&sendmsg) original = (__typeof__(&sendmsg))dlsym(RTLD_NEXT, "sendmsg");
    if (!original) _exit(115);
    int kind = pressure_kind(message, 32);
    if (pressure_role == 1 && kind == 1 && !pressure_configured++) {
        int size = 4096;
        if (setsockopt(descriptor, SOL_SOCKET, SO_SNDBUF, &size, sizeof(size))) _exit(116);
        pressure_log("send-buffer-bounded\n");
    }
    if (pressure_role == 2 && (LO_CASE == 2 || LO_CASE == 3) && kind == 15 && !pressure_ack_changed) {
        if (message->msg_iovlen != 1 || message->msg_iov[0].iov_len != 40
            || message->msg_controllen || pressure_descriptor < 0) _exit(118);
        unsigned char bytes[40]; memcpy(bytes, message->msg_iov[0].iov_base, sizeof(bytes));
        struct iovec vector = {bytes, sizeof(bytes)};
        struct msghdr changed = *message; changed.msg_iov = &vector;
        union { struct cmsghdr align; unsigned char bytes[CMSG_SPACE(sizeof(int))]; } control = {0};
        if (LO_CASE == 2) bytes[39] = 1; /* First zero-based index must be zero. */
        else {
            bytes[15] = 1;
            changed.msg_control = control.bytes; changed.msg_controllen = sizeof(control.bytes);
            struct cmsghdr *header = CMSG_FIRSTHDR(&changed);
            header->cmsg_level = SOL_SOCKET; header->cmsg_type = SCM_RIGHTS;
            header->cmsg_len = CMSG_LEN(sizeof(int));
            memcpy(CMSG_DATA(header), &pressure_descriptor, sizeof(pressure_descriptor));
        }
        ssize_t result = original(descriptor, &changed, flags); int error = errno;
        if (result == (ssize_t)sizeof(bytes)) {
            pressure_ack_changed = 1;
            pressure_log(LO_CASE == 2 ? "wrong-resource-ack-index\n" : "descriptor-bearing-resource-ack\n");
        } else if (result >= 0) _exit(119);
        errno = error; return result;
    }
    ssize_t result = original(descriptor, message, flags); int error = errno;
    if (pressure_role == 1 && kind == 3 && result > 0) pressure_log("resource-sent\n");
    if (pressure_role == 1 && result < 0 && error == EMSGSIZE) pressure_log("resource-send-emsgsize\n");
    errno = error; return result;
}
ssize_t recvmsg(int descriptor, struct msghdr *message, int flags) {
    __typeof__(&recvmsg) original = (__typeof__(&recvmsg))dlsym(RTLD_NEXT, "recvmsg");
    if (!original) _exit(115);
    ssize_t result = original(descriptor, message, flags); int error = errno;
    if (pressure_role == 2 && pressure_kind(message, result) == 3) {
        pressure_log("resource-received\n");
        if (!pressure_received++) {
            struct cmsghdr *header = CMSG_FIRSTHDR(message);
            if (!header || header->cmsg_level != SOL_SOCKET || header->cmsg_type != SCM_RIGHTS
                || header->cmsg_len != CMSG_LEN(sizeof(int))) _exit(117);
            memcpy(&pressure_descriptor, CMSG_DATA(header), sizeof(pressure_descriptor));
            /* The real first descriptor has reached the supervisor. Hold its
             * public receive boundary before runtime adoption and body read. */
            struct timespec remaining = {0, 200000000};
            while (nanosleep(&remaining, &remaining) && errno == EINTR) {}
            pressure_log("first-resource-reader-resumed\n");
        }
    }
    errno = error; return result;
}
int fstat(int descriptor, struct stat *information) {
    __typeof__(&fstat) original = (__typeof__(&fstat))dlsym(RTLD_NEXT, "fstat");
    if (!original) _exit(115);
    if (LO_CASE == 1 && pressure_role == 2 && descriptor == pressure_descriptor && !pressure_injected++) {
        pressure_log("resource-adoption-rejected\n"); errno = EIO; return -1;
    }
    return original(descriptor, information);
}
int close(int descriptor) {
    __typeof__(&close) original = (__typeof__(&close))dlsym(RTLD_NEXT, "close");
    if (!original) _exit(115);
    int result = original(descriptor); int error = errno;
    if (pressure_role == 2 && descriptor == pressure_descriptor) {
        pressure_descriptor = -1;
        pressure_log(result == 0 ? "received-resource-closed\n" : "received-resource-close-failed\n");
    }
    errno = error; return result;
}

@F036_PRESSURE_PROVIDER@

@F036_ACK_PROBES@
