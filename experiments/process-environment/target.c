#include <errno.h>
#include <fcntl.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include "fixture_paths.h"

extern char **environ;
#define MARKER(phase) do { \
    static const char marker[] = "LO_PHASE " phase "\n"; \
    if (write(STDERR_FILENO, marker, sizeof(marker) - 1) != sizeof(marker) - 1) _exit(90); \
} while (0)

static bool exact_environment(const char *selection) {
    const char *loaded[] = {"CI=1", "TMPDIR=" LO_ROOT, "LO_TARGET_SENTINEL=explicit", "LO_EMPTY=", "DYLD_INSERT_LIBRARIES=" LO_LIBRARY, NULL};
    const char *empty[] = {NULL};
    const char *values[] = {"A0=after", "A=first=second", "UNICODE=\xce\xbb\nline", "EMPTY=", NULL};
    const char **expected;
    if (strcmp(selection, "loaded") == 0) expected = loaded;
    else if (strcmp(selection, "empty") == 0) expected = empty;
    else if (strcmp(selection, "values") == 0) expected = values;
    else return false;
    size_t index = 0;
    while (expected[index] != NULL) {
        if (environ[index] == NULL || strcmp(environ[index], expected[index]) != 0) return false;
        index++;
    }
    return environ[index] == NULL;
}

int main(int argc, char **argv) {
    MARKER("target.entered");
    if (argc != 3) return 64;
    bool arguments = strcmp(argv[0], LO_TARGET) == 0 && strcmp(argv[2], "fixed-argument") == 0;
    bool environment = exact_environment(argv[1]);
    struct stat directory;
    bool selected_directory = stat(".", &directory) == 0 &&
        (uint64_t)directory.st_dev == LO_EXPECTED_DEVICE && (uint64_t)directory.st_ino == LO_EXPECTED_INODE;
    int descriptors = 0;
    bool standard = true;
    for (int descriptor = 0; descriptor < LO_FD_LIMIT; descriptor++) {
        errno = 0;
        if (fcntl(descriptor, F_GETFD) >= 0) descriptors++;
        else if (errno != EBADF || descriptor < 3) standard = false;
    }
    char report[256];
    int length = snprintf(report, sizeof(report),
        "{\"target_started\":true,\"environment_exact\":%s,\"arguments_exact\":%s,\"directory_exact\":%s,\"open_fds\":%d}\n",
        environment ? "true" : "false", arguments ? "true" : "false", selected_directory ? "true" : "false", standard ? descriptors : -1);
    if (length < 0 || (size_t)length >= sizeof(report) || write(STDOUT_FILENO, report, (size_t)length) != length) return 91;
    MARKER("target.complete");
    return environment && arguments && selected_directory && standard && descriptors == 3 ? 0 : 1;
}
