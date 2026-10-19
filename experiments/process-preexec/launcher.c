#include <errno.h>
#include <sandbox.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

#include "fixture_paths.h"

extern char **environ;

#define MARKER(phase) do { \
    static const char marker[] = "LO_PHASE " phase "\n"; \
    if (write(STDERR_FILENO, marker, sizeof(marker) - 1) != sizeof(marker) - 1) _exit(90); \
} while (0)

int main(int argc, char **argv) {
    MARKER("launcher.entered");
    if (argc != 2) return 2;
    char *end = NULL;
    errno = 0;
    unsigned long port = strtoul(argv[1], &end, 10);
    if (errno != 0 || end == argv[1] || *end != '\0' || port == 0 || port > 65535) return 2;
    char *sandbox_error = NULL;
    MARKER("launcher.before-sandbox-init");
    if (sandbox_init(LO_PROFILE, 0, &sandbox_error) != 0) {
        MARKER("launcher.sandbox-init-failed");
        const char failure[] = "{\"launcher_installed\":false}\n";
        if (write(STDOUT_FILENO, failure, sizeof(failure) - 1) != sizeof(failure) - 1) _exit(92);
        _exit(3);
    }
    MARKER("launcher.after-sandbox-init");
    char *arguments[] = {LO_TARGET, argv[1], "fixed-argument", NULL};
    MARKER("launcher.before-execve");
    execve(LO_TARGET, arguments, environ);
    int exec_error = errno;
    MARKER("launcher.execve-returned");
    char failure[128];
    int count = snprintf(failure, sizeof(failure), "{\"launcher_installed\":true,\"exec_failed\":true,\"errno\":%d}\n", exec_error);
    if (count < 0 || (size_t)count >= sizeof(failure) || write(STDOUT_FILENO, failure, (size_t)count) != count) _exit(92);
    _exit(4);
}
