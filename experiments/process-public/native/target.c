#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>
#include "fixture_paths.h"

/* Only sealed, finite acceptance modes. No trap, shell, filesystem mutation,
   descendants, sockets, service lookup, or ambient environment is used. */
int main(int argc, char **argv, char **environment) {
    struct stat directory;
    if (argc != 2 || strcmp(argv[0], LO_TARGET) != 0 || environment[0] == NULL || strcmp(environment[0], "NAME=explicit") != 0 || environment[1] != NULL || stat(".", &directory) != 0 || (uint64_t)directory.st_dev != LO_ROOT_DEVICE || (uint64_t)directory.st_ino != LO_ROOT_INODE) return 80;
    for (int descriptor = 0; descriptor < 64; descriptor++) {
        errno = 0;
        int result = fcntl(descriptor, F_GETFD);
        if ((descriptor < 3 && result < 0) || (descriptor >= 3 && (result >= 0 || errno != EBADF))) return 81;
    }
    int mode = strcmp(argv[1], "normal") == 0 ? 0 : strcmp(argv[1], "nonzero") == 0 ? 1 : strcmp(argv[1], "signaled") == 0 ? 2 : strcmp(argv[1], "wait") == 0 ? 3 : -1;
    if (mode < 0) return 82;
    unsigned char input[6];
    size_t offset = 0;
    for (;;) {
        ssize_t count = read(0, input + offset, sizeof(input) - offset);
        if (count < 0 && errno == EINTR) continue;
        if (count < 0 || offset + (size_t)count > 5) return 83;
        if (count == 0) break;
        offset += (size_t)count;
    }
    if (offset != 5 || memcmp(input, "stdin", 5) != 0) return 84;
    if (mode == 3) { for (;;) pause(); }
    static const unsigned char output[] = {0, 255, 'A'}, errors[] = {128, 'e'};
    if (write(1, output, sizeof(output)) != (ssize_t)sizeof(output) || write(2, errors, sizeof(errors)) != (ssize_t)sizeof(errors)) return 85;
    if (mode == 2) { if (kill(getpid(), SIGKILL) != 0) return 86; return 87; }
    return mode == 1 ? 7 : 0;
}
