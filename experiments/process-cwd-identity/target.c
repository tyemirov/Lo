#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <stdint.h>
#include <inttypes.h>
#include <sys/stat.h>
#include <signal.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/wait.h>
#include <unistd.h>

#include "fixture_paths.h"

extern char **environ;
#define MARKER(phase) do { \
    static const char marker[] = "LO_PHASE " phase "\n"; \
    if (write(STDERR_FILENO, marker, sizeof(marker) - 1) != sizeof(marker) - 1) _exit(91); \
} while (0)
struct observation { int result; int error; };

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
    const char bytes[] = "probe-write";
    ssize_t count = write(descriptor, bytes, sizeof(bytes) - 1);
    int failure = count < 0 ? errno : count == sizeof(bytes) - 1 ? 0 : EIO;
    if (close(descriptor) != 0 && failure == 0) failure = errno;
    return (struct observation){failure == 0 ? 0 : -1, failure};
}
static struct observation connect_fixture(unsigned short port) {
    int descriptor = socket(AF_INET, SOCK_STREAM, 0);
    if (descriptor < 0) return (struct observation){-1, errno};
    int failure = 0;
    if (fcntl(descriptor, F_SETFL, O_NONBLOCK) < 0) failure = errno;
    else {
        struct sockaddr_in address = {.sin_family=AF_INET, .sin_port=htons(port), .sin_addr.s_addr=htonl(INADDR_LOOPBACK)};
        if (connect(descriptor, (const struct sockaddr *)&address, sizeof(address)) != 0) {
            failure = errno;
            if (failure == EINPROGRESS) {
                struct pollfd pending = {.fd=descriptor, .events=POLLOUT};
                int ready = poll(&pending, 1, 250);
                if (ready > 0) {
                    socklen_t length = sizeof(failure);
                    if (getsockopt(descriptor, SOL_SOCKET, SO_ERROR, &failure, &length) != 0) failure = errno;
                } else failure = ready == 0 ? ETIMEDOUT : errno;
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
    return (struct observation){1, waited == child && WIFEXITED(status) && WEXITSTATUS(status) == 0 ? 0 : ECHILD};
}
static bool denied(struct observation item) { return item.result == -1 && item.error == EPERM; }

int main(int argc, char **argv) {
    MARKER("target.entered");
    if (argc != 3) return 2;
    char *end = NULL;
    errno = 0;
    unsigned long port = strtoul(argv[1], &end, 10);
    if (errno != 0 || end == argv[1] || *end != '\0' || port == 0 || port > 65535) return 2;
    if (signal(SIGPIPE, SIG_IGN) == SIG_ERR) return 2;
    MARKER("target.before-arguments");
    bool arguments = strcmp(argv[0], LO_TARGET) == 0 && strcmp(argv[2], "fixed-argument") == 0;
    MARKER("target.after-arguments");
    MARKER("target.before-environment");
    int environment_count = 0;
    for (char **entry = environ; *entry != NULL; entry++) environment_count++;
    const char *sentinel = getenv("LO_PREEXEC_SENTINEL"), *ci = getenv("CI"), *temporary = getenv("TMPDIR");
    bool environment = environment_count == 3 && sentinel != NULL && strcmp(sentinel,"explicit") == 0 && ci != NULL && strcmp(ci,"1") == 0 && temporary != NULL && strcmp(temporary,LO_ROOT) == 0;
    MARKER("target.after-environment");
    MARKER("target.before-directory");
    char cwd[4096];
    errno = 0;
    bool cwd_present = getcwd(cwd,sizeof(cwd)) != NULL;
    int cwd_error = cwd_present ? 0 : errno;
    bool cwd_exact = cwd_present && strcmp(cwd,LO_ROOT) == 0;
    struct stat directory_info;
    errno = 0;
    bool directory_present = stat(".", &directory_info) == 0;
    int directory_error = directory_present ? 0 : errno;
    uintmax_t directory_device = directory_present ? (uintmax_t)directory_info.st_dev : 0;
    uintmax_t directory_inode = directory_present ? (uintmax_t)directory_info.st_ino : 0;
    bool directory = directory_present && directory_device == LO_EXPECTED_DEVICE && directory_inode == LO_EXPECTED_INODE;
    MARKER("target.after-directory");
    MARKER("target.before-descriptors");
    int open_descriptors = 0;
    bool standard_descriptors = true;
    for (int descriptor=0; descriptor<LO_FD_LIMIT; descriptor++) {
        bool opened = fcntl(descriptor,F_GETFD) >= 0;
        if (opened) open_descriptors++;
        if (descriptor < 3 && !opened) standard_descriptors = false;
    }
    MARKER("target.after-descriptors");
    MARKER("target.before-allowed-read");
    struct observation allowed_read = read_fixture(LO_ALLOWED_READ,"allowed-fixture");
    MARKER("target.after-allowed-read");
    MARKER("target.before-denied-read");
    struct observation denied_read = read_fixture(LO_DENIED_READ,"denied-fixture");
    MARKER("target.after-denied-read");
    MARKER("target.before-allowed-write");
    struct observation allowed_write = write_fixture(LO_ALLOWED_WRITE);
    MARKER("target.after-allowed-write");
    MARKER("target.before-denied-write");
    struct observation denied_write = write_fixture(LO_DENIED_WRITE);
    MARKER("target.after-denied-write");
    MARKER("target.before-network");
    struct observation network = connect_fixture((unsigned short)port);
    MARKER("target.after-network");
    MARKER("target.before-fork");
    struct observation child = fork_fixture();
    MARKER("target.after-fork");
    MARKER("target.before-report");
    char report[1536];
    int count = snprintf(report,sizeof(report),"{\"target_started\":true,\"arguments_exact\":%s,\"environment_exact\":%s,\"getcwd\":{\"present\":%s,\"exact\":%s,\"errno\":%d},\"directory_identity\":{\"present\":%s,\"errno\":%d,\"device\":%ju,\"inode\":%ju},\"standard_fds_open\":%s,\"open_fds_below_limit\":%d,\"fd_limit\":%d,\"allowed_read\":[%d,%d],\"denied_read\":[%d,%d],\"allowed_write\":[%d,%d],\"denied_write\":[%d,%d],\"network\":[%d,%d],\"fork\":[%d,%d]}\n",arguments?"true":"false",environment?"true":"false",cwd_present?"true":"false",cwd_exact?"true":"false",cwd_error,directory_present?"true":"false",directory_error,directory_device,directory_inode,standard_descriptors?"true":"false",open_descriptors,LO_FD_LIMIT,allowed_read.result,allowed_read.error,denied_read.result,denied_read.error,allowed_write.result,allowed_write.error,denied_write.result,denied_write.error,network.result,network.error,child.result,child.error);
    if (count < 0 || (size_t)count >= sizeof(report) || write(STDOUT_FILENO,report,(size_t)count) != count) _exit(93);
    MARKER("target.after-report");
    bool passed = arguments && environment && directory && standard_descriptors && open_descriptors == 3 && allowed_read.result == 0 && allowed_write.result == 0 && denied(denied_read) && denied(denied_write) && denied(network) && denied(child);
    _exit(passed ? 0 : 1);
}
