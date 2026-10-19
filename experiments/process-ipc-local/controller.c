#include <errno.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>
#include "fixture_paths.h"

static double monotonic(void) {
    struct timespec now;
    if (clock_gettime(CLOCK_MONOTONIC, &now) != 0) _exit(90);
    return now.tv_sec + now.tv_nsec / 1000000000.0;
}

int main(int argc, char **argv) {
    if (argc != 2 || (strcmp(argv[1], "local-control") != 0 && strcmp(argv[1], "normalized-control") != 0 && strcmp(argv[1], "normalized-confined") != 0)) return 64;
    const char *text = getenv("LO_IPC_LOCAL_CONTROL_FD");
    char *end = NULL;
    long parsed = text == NULL ? -1 : strtol(text, &end, 10);
    if (parsed < 3 || parsed >= 64 || end == NULL || *end != '\0') return 64;
    int control = (int)parsed;
    unsigned char admission;
    ssize_t count;
    do { count = read(control, &admission, 1); } while (count < 0 && errno == EINTR);
    if (count != 1 || admission != 'A') return 65;
    struct rlimit core = {0, 0}, descriptors = {64, 64}, cpu = {5, 5};
    if (setrlimit(RLIMIT_CORE, &core) != 0 || setrlimit(RLIMIT_NOFILE, &descriptors) != 0 || setrlimit(RLIMIT_CPU, &cpu) != 0) return 66;
    double started = monotonic();
    /* This controller installs no Mach canary and messages no endpoint. */
    pid_t child = fork();
    if (child == 0) {
        for (int descriptor = 3; descriptor < 64; descriptor++) close(descriptor);
        char *arguments[] = {LO_LAUNCHER, argv[1], NULL};
        char *environment[] = {"CI=1", "TMPDIR=" LO_ROOT, NULL};
        execve(LO_LAUNCHER, arguments, environment);
        _exit(68);
    }
    int status = 0, joined = 0, cancelled = 0, timed_out = 0, signal_attempted = 0, signal_error = 0, cleanup_deadline_exceeded = 0;
    double stop_started = 0;
    if (child > 0) {
        while (!joined) {
            pid_t observed = waitpid(child, &status, WNOHANG);
            if (observed == child) { joined = 1; break; }
            if (observed < 0 && errno != EINTR) {
                printf("{\"kind\":\"ownership-unresolved\",\"variant\":\"%s\",\"wait_errno\":%d}\n", argv[1], errno);
                fflush(stdout);
                /* Do not signal a PID after losing the wait relationship. */
                for (;;) { struct timespec delay = {0, 100000000}; nanosleep(&delay, NULL); }
            }
            struct pollfd request = {control, POLLIN | POLLHUP, 0};
            int ready = poll(&request, 1, 10);
            if (ready < 0 && errno != EINTR) cancelled = 1;
            if (ready > 0 && request.revents != 0) { unsigned char message; (void)read(control, &message, 1); cancelled = 1; }
            double now = monotonic();
            if (now - started >= 5) timed_out = 1;
            if ((cancelled || timed_out) && !signal_attempted) {
                signal_attempted = 1;
                stop_started = now;
                if (kill(child, SIGKILL) != 0) signal_error = errno;
            }
            if (signal_attempted && now - stop_started >= 2) cleanup_deadline_exceeded = 1;
        }
    }
    int cleanup_complete = (child < 0 || joined) && close(control) == 0;
    printf("{\"kind\":\"controller\",\"variant\":\"%s\",\"child_created\":%s,\"child_joined\":%s,\"exited\":%s,\"exit_code\":%d,\"signal\":%d,\"cancelled\":%s,\"timed_out\":%s,\"signal_attempted\":%s,\"signal_errno\":%d,\"cleanup_deadline_exceeded\":%s,\"cleanup_complete\":%s,\"elapsed_seconds\":%.6f}\n", argv[1], child > 0 ? "true" : "false", joined ? "true" : "false", joined && WIFEXITED(status) ? "true" : "false", joined && WIFEXITED(status) ? WEXITSTATUS(status) : -1, joined && WIFSIGNALED(status) ? WTERMSIG(status) : 0, cancelled ? "true" : "false", timed_out ? "true" : "false", signal_attempted ? "true" : "false", signal_error, cleanup_deadline_exceeded ? "true" : "false", cleanup_complete ? "true" : "false", monotonic() - started);
    return cleanup_complete && child > 0 && joined && WIFEXITED(status) && WEXITSTATUS(status) == 0 && !cancelled && !timed_out && !cleanup_deadline_exceeded ? 0 : 69;
}
