#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <spawn.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/wait.h>
#include <unistd.h>

#include "fixture_paths.h"

extern char **environ;
static volatile sig_atomic_t cpu_soft_seen = 0;
#define MARKER(name) do { \
    static const char marker[] = "LO_RESOURCE " name "\n"; \
    if (write(STDERR_FILENO, marker, sizeof(marker) - 1) != sizeof(marker) - 1) _exit(90); \
} while (0)

static int report(const char *text) {
    size_t count = strlen(text);
    return write(STDOUT_FILENO, text, count) == (ssize_t)count ? 0 : 91;
}

static int create_child(const char *api) {
    pid_t child = -1;
    int error = 0;
    if (strcmp(api, "spawn") == 0) {
        char *arguments[] = {LO_TARGET, "child-exit", NULL};
        error = posix_spawn(&child, LO_TARGET, NULL, NULL, arguments, environ);
    } else {
        child = strcmp(api, "fork") == 0 ? fork() : vfork();
        if (child == 0) _exit(0);
        if (child < 0) error = errno;
    }
    bool created = error == 0, reaped = false;
    if (created) {
        int status = 0;
        pid_t waited;
        do { waited = waitpid(child, &status, 0); } while (waited < 0 && errno == EINTR);
        reaped = waited == child && WIFEXITED(status) && WEXITSTATUS(status) == 0;
    }
    char output[192];
    int length = snprintf(output, sizeof(output),
        "{\"api\":\"%s\",\"error\":%d,\"child_created\":%s,\"child_reaped\":%s}\n",
        api, error, created ? "true" : "false", reaped ? "true" : "false");
    if (length < 0 || (size_t)length >= sizeof(output)) return 92;
    int written = report(output);
    return written != 0 ? written : created && !reaped ? 93 : 0;
}

static void cpu_signal(int signal_number) {
    (void)signal_number;
    MARKER("cpu-soft");
    cpu_soft_seen = 1;
}

static int cpu_case(bool hard) {
    if (signal(SIGXCPU, hard ? SIG_IGN : cpu_signal) == SIG_ERR) return 94;
    if (hard) MARKER("cpu-hard-ready");
    volatile uint64_t counter = 0;
    while (hard || !cpu_soft_seen) counter++;
    return report("{\"cpu_soft_observed\":true}\n");
}

static int descriptor_case(void) {
    int descriptors[LO_FD_LIMIT], count = 0, error = 0;
    bool closed = true;
    while (count < LO_FD_LIMIT) {
        int descriptor = dup(STDOUT_FILENO);
        if (descriptor < 0) { error = errno; break; }
        descriptors[count++] = descriptor;
    }
    for (int index = 0; index < count; index++) {
        if (close(descriptors[index]) != 0) closed = false;
    }
    char output[128];
    int length = snprintf(output, sizeof(output), "{\"error\":%d,\"duplicates\":%d,\"closed\":%s}\n", error, count, closed ? "true" : "false");
    if (length < 0 || (size_t)length >= sizeof(output)) return 92;
    return report(output);
}

static int limits_case(void) {
    const struct { int resource; rlim_t soft, hard; } expected[] = {
        {RLIMIT_CORE, 0, 0}, {RLIMIT_FSIZE, LO_OUTPUT_LIMIT, LO_OUTPUT_LIMIT},
        {RLIMIT_NOFILE, LO_FD_LIMIT, LO_FD_LIMIT}, {RLIMIT_CPU, 1, 2},
    };
    bool exact = true, raises_denied = true;
    for (size_t index = 0; index < sizeof(expected) / sizeof(expected[0]); index++) {
        struct rlimit actual;
        if (getrlimit(expected[index].resource, &actual) != 0) return 95;
        if (actual.rlim_cur != expected[index].soft || actual.rlim_max != expected[index].hard) exact = false;
        struct rlimit raised = {expected[index].soft, expected[index].hard + 1};
        errno = 0;
        if (setrlimit(expected[index].resource, &raised) != -1 || errno != EPERM) raises_denied = false;
    }
    char output[128];
    int length = snprintf(output, sizeof(output), "{\"limits_exact\":%s,\"hard_raises_denied\":%s}\n", exact ? "true" : "false", raises_denied ? "true" : "false");
    if (length < 0 || (size_t)length >= sizeof(output)) return 92;
    return report(output);
}

static int write_pattern(int descriptor, unsigned char pattern, size_t count) {
    unsigned char bytes[4096];
    memset(bytes, pattern, sizeof(bytes));
    while (count > 0) {
        size_t requested = count < sizeof(bytes) ? count : sizeof(bytes);
        ssize_t written = write(descriptor, bytes, requested);
        if (written < 0 && errno == EINTR) continue;
        if (written <= 0) return 96;
        count -= (size_t)written;
    }
    return 0;
}

int main(int argc, char **argv) {
    if (argc == 2 && strcmp(argv[0], LO_TARGET) == 0 && strcmp(argv[1], "child-exit") == 0) _exit(0);
    MARKER("target-entered");
    if (argc != 3 || strcmp(argv[0], LO_TARGET) != 0 || strcmp(argv[1], "values") != 0 || strcmp(argv[2], "fixed-argument") != 0) return 64;
    if (environ[0] == NULL || environ[1] != NULL || strncmp(environ[0], "LO_RESOURCE_CASE=", 17) != 0) return 66;
    const char *selection = getenv("LO_RESOURCE_CASE");
    if (selection == NULL) return 66;
    if (strcmp(selection, "fork") == 0 || strcmp(selection, "vfork") == 0 || strcmp(selection, "spawn") == 0) return create_child(selection);
    if (strcmp(selection, "cpu-soft") == 0) return cpu_case(false);
    if (strcmp(selection, "cpu-hard") == 0) return cpu_case(true);
    if (strcmp(selection, "fd") == 0) return descriptor_case();
    if (strcmp(selection, "limits") == 0) return limits_case();
    if (strcmp(selection, "wall") == 0) {
        MARKER("wall-ready");
        for (;;) pause();
    }
    if (strcmp(selection, "stdout-exact") == 0) return write_pattern(STDOUT_FILENO, 'o', LO_OUTPUT_LIMIT);
    if (strcmp(selection, "stdout-overflow") == 0) return write_pattern(STDOUT_FILENO, 'o', LO_OUTPUT_LIMIT + 1);
    if (strcmp(selection, "stderr-exact") == 0) return write_pattern(STDERR_FILENO, 'e', LO_OUTPUT_LIMIT - LO_CAPTURE_PREFIX_BYTES);
    if (strcmp(selection, "stderr-overflow") == 0) return write_pattern(STDERR_FILENO, 'e', LO_OUTPUT_LIMIT - LO_CAPTURE_PREFIX_BYTES + 1);
    if (strcmp(selection, "dual-pressure") == 0) {
        for (int index = 0; index < 256; index++) {
            int error = write_pattern(STDOUT_FILENO, 'o', 4096);
            if (error != 0) return error;
            error = write_pattern(STDERR_FILENO, 'e', 4096);
            if (error != 0) return error;
        }
        return 0;
    }
    return 64;
}
