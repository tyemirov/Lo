#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <signal.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>
#include "fixture_paths.h"

int main(int argc, char **argv, char **environment) {
    struct stat directory;
    if (argc != 2 || strcmp(argv[0], LO_TARGET) != 0 || environment[0] == NULL || strcmp(environment[0], "NAME=explicit") != 0 || environment[1] != NULL || stat(".", &directory) != 0 || (uint64_t)directory.st_dev != LO_ROOT_DEVICE || (uint64_t)directory.st_ino != LO_ROOT_INODE) return 80;
    for (int descriptor = 0; descriptor < 64; descriptor++) {
        errno = 0;
        int result = fcntl(descriptor, F_GETFD);
        if ((descriptor < 3 && result < 0) || (descriptor >= 3 && (result >= 0 || errno != EBADF))) return 81;
    }
    if (strcmp(argv[1], "nonzero") == 0) return 7;
    if (strcmp(argv[1], "signaled") == 0) { kill(getpid(), SIGKILL); return 87; }
    if (strcmp(argv[1], "wall-timeout") == 0) { for (;;) pause(); }
    if (strcmp(argv[1], "output-limit") == 0) {
        char output[4096] = {0};
        for (;;) { if (write(1, output, sizeof(output)) < 0 && errno != EINTR) return 86; }
    }
    if (strcmp(argv[1], "normal") != 0 && strcmp(argv[1], "output-to-file") != 0) return 82;
    char bytes[6];
    size_t offset = 0;
    for (;;) {
        ssize_t count = read(0, bytes + offset, sizeof(bytes) - offset);
        if (count < 0 && errno == EINTR) continue;
        if (count < 0 || offset + (size_t)count > 5) return 83;
        if (count == 0) break;
        offset += (size_t)count;
    }
    if (offset != 5 || memcmp(bytes, "stdin", 5) != 0) return 84;
    if (write(1, "stdout", 6) != 6 || write(2, "stderr", 6) != 6) return 85;
    return 0;
}
