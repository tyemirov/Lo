#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <sandbox.h>
#include <signal.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/wait.h>
#include <unistd.h>

#include "fixture_paths.h"

struct observation {
    int result;
    int error;
};

static struct observation read_fixture(const char *path, const char *expected) {
    int descriptor = open(path, O_RDONLY | O_NOFOLLOW);
    if (descriptor < 0) return (struct observation){-1, errno};
    char bytes[64];
    ssize_t count = read(descriptor, bytes, sizeof(bytes));
    int failure = count < 0 ? errno : 0;
    if (close(descriptor) != 0 && failure == 0) failure = errno;
    if (failure != 0) return (struct observation){-1, failure};
    bool equal = count == (ssize_t)strlen(expected) && memcmp(bytes, expected, (size_t)count) == 0;
    return (struct observation){equal ? 0 : -1, equal ? 0 : EIO};
}

static struct observation write_fixture(const char *path) {
    int descriptor = open(path, O_WRONLY | O_TRUNC | O_NOFOLLOW);
    if (descriptor < 0) return (struct observation){-1, errno};
    const char content[] = "probe-write";
    ssize_t count = write(descriptor, content, sizeof(content) - 1);
    int failure = count < 0 ? errno : count == sizeof(content) - 1 ? 0 : EIO;
    if (close(descriptor) != 0 && failure == 0) failure = errno;
    return (struct observation){failure == 0 ? 0 : -1, failure};
}

static struct observation connect_fixture(unsigned short port) {
    int descriptor = socket(AF_INET, SOCK_STREAM, 0);
    if (descriptor < 0) return (struct observation){-1, errno};
    int failure = 0;
    if (fcntl(descriptor, F_SETFL, O_NONBLOCK) < 0) {
        failure = errno;
    } else {
        struct sockaddr_in address = {.sin_family = AF_INET, .sin_port = htons(port), .sin_addr.s_addr = htonl(INADDR_LOOPBACK)};
        if (connect(descriptor, (const struct sockaddr *)&address, sizeof(address)) != 0) {
            failure = errno;
            if (failure == EINPROGRESS) {
                struct pollfd pending = {.fd = descriptor, .events = POLLOUT};
                int ready = poll(&pending, 1, 250);
                if (ready > 0) {
                    socklen_t size = sizeof(failure);
                    if (getsockopt(descriptor, SOL_SOCKET, SO_ERROR, &failure, &size) != 0) failure = errno;
                } else {
                    failure = ready == 0 ? ETIMEDOUT : errno;
                }
            }
        }
    }
    if (close(descriptor) != 0 && failure == 0) failure = errno;
    return (struct observation){failure == 0 ? 0 : -1, failure};
}

static struct observation fork_fixture(void) {
    pid_t child = fork();
    if (child < 0) return (struct observation){-1, errno};
    if (child == 0) _exit(0);
    int status = 0;
    pid_t waited;
    do { waited = waitpid(child, &status, 0); } while (waited < 0 && errno == EINTR);
    if (waited != child) return (struct observation){1, errno};
    return (struct observation){1, WIFEXITED(status) && WEXITSTATUS(status) == 0 ? 0 : ECHILD};
}

static bool denied(struct observation item) {
    return item.result == -1 && (item.error == EPERM || item.error == EACCES);
}

int main(int argc, char **argv) {
    if (argc != 2) return 2;
    char *end = NULL;
    errno = 0;
    unsigned long port = strtoul(argv[1], &end, 10);
    if (errno != 0 || *end != '\0' || end == argv[1] || port == 0 || port > 65535) return 2;
    if (signal(SIGPIPE, SIG_IGN) == SIG_ERR) return 2;

    // No test operation runs before successful confinement of this trusted fixture.
    char *sandbox_error = NULL;
    if (sandbox_init(LO_PROFILE, 0, &sandbox_error) != 0) {
        const char failure[] = "{\"installed\":false}\n";
        (void)write(STDOUT_FILENO, failure, sizeof(failure) - 1);
        _exit(3);
    }
    struct observation allowed_read = read_fixture(LO_ALLOWED_READ, "allowed-fixture");
    struct observation denied_read = read_fixture(LO_DENIED_READ, "denied-fixture");
    struct observation allowed_write = write_fixture(LO_ALLOWED_WRITE);
    struct observation denied_write = write_fixture(LO_DENIED_WRITE);
    struct observation network = connect_fixture((unsigned short)port);
    struct observation child = fork_fixture();
    char report[1024];
    int count = snprintf(report, sizeof(report),
        "{\"installed\":true,\"allowed_read\":[%d,%d],\"denied_read\":[%d,%d],"
        "\"allowed_write\":[%d,%d],\"denied_write\":[%d,%d],\"network\":[%d,%d],\"fork\":[%d,%d]}\n",
        allowed_read.result, allowed_read.error, denied_read.result, denied_read.error,
        allowed_write.result, allowed_write.error, denied_write.result, denied_write.error,
        network.result, network.error, child.result, child.error);
    if (count < 0 || (size_t)count >= sizeof(report) || write(STDOUT_FILENO, report, (size_t)count) != count) _exit(4);
    bool passed = allowed_read.result == 0 && allowed_write.result == 0 && denied(denied_read) && denied(denied_write) && denied(network) && denied(child);
    _exit(passed ? 0 : 1);
}
