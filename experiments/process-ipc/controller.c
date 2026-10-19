#include "ipc.h"
#include <errno.h>
#include <mach/exception_types.h>
#include <mach/thread_status.h>
#include <poll.h>
#include <servers/bootstrap.h>
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

static int parent_canary(mach_port_t *canary, kern_return_t setup[LO_IPC_CHANNELS]) {
    if (mach_port_allocate(mach_task_self(), MACH_PORT_RIGHT_RECEIVE, canary) != KERN_SUCCESS) return 1;
    if (mach_port_insert_right(mach_task_self(), *canary, *canary, MACH_MSG_TYPE_MAKE_SEND) != KERN_SUCCESS) return 1;
    setup[0] = mach_ports_register(mach_task_self(), canary, 1);
    setup[1] = task_set_special_port(mach_task_self(), TASK_BOOTSTRAP_PORT, *canary);
    /* Own a cache reference independently of the controller's original send. */
    if (mach_port_mod_refs(mach_task_self(), *canary, MACH_PORT_RIGHT_SEND, 1) != KERN_SUCCESS) return 1;
    mach_port_t previous = bootstrap_port;
    bootstrap_port = *canary;
    if (MACH_PORT_VALID(previous) && mach_port_deallocate(mach_task_self(), previous) != KERN_SUCCESS) return 1;
    setup[2] = task_set_exception_ports(mach_task_self(), EXC_MASK_ALL, *canary, EXCEPTION_DEFAULT, THREAD_STATE_NONE);
    thread_t initial = mach_thread_self();
    if (!MACH_PORT_VALID(initial)) return 1;
    setup[3] = thread_set_exception_ports(initial, EXC_MASK_ALL, *canary, EXCEPTION_DEFAULT, THREAD_STATE_NONE);
    return mach_port_deallocate(mach_task_self(), initial) == KERN_SUCCESS ? 0 : 1;
}

static int messages(mach_port_t canary, unsigned int counts[LO_IPC_CHANNELS]) {
    for (int index = 0; index < 16; index++) {
        struct { mach_msg_header_t header; mach_msg_max_trailer_t trailer; } message;
        memset(&message, 0, sizeof(message));
        kern_return_t result = mach_msg(&message.header, MACH_RCV_MSG | MACH_RCV_TIMEOUT, 0, sizeof(message), canary, 0, MACH_PORT_NULL);
        if (result == MACH_RCV_TIMED_OUT) return 0;
        if (result != KERN_SUCCESS) return 1;
        int channel = message.header.msgh_id - LO_IPC_MESSAGE_BASE;
        if (channel < 0 || channel >= LO_IPC_CHANNELS || message.header.msgh_size != sizeof(mach_msg_header_t) || (message.header.msgh_bits & MACH_MSGH_BITS_COMPLEX) != 0 || MACH_PORT_VALID(message.header.msgh_remote_port)) {
            mach_msg_destroy(&message.header);
            return 1;
        }
        counts[channel]++;
    }
    return 1;
}

int main(int argc, char **argv) {
    if (argc != 2 || lo_ipc_variant(argv[1]) < 0) return 64;
    const char *control_text = getenv("LO_IPC_CONTROL_FD");
    char *end = NULL;
    long parsed = control_text == NULL ? -1 : strtol(control_text, &end, 10);
    if (parsed < 3 || parsed >= 64 || end == NULL || *end != '\0') return 64;
    int control = (int)parsed;
    unsigned char admission;
    ssize_t count;
    do { count = read(control, &admission, 1); } while (count < 0 && errno == EINTR);
    if (count != 1 || admission != 'A') return 65;
    struct rlimit core = {0, 0}, descriptors = {64, 64}, cpu = {5, 5};
    if (setrlimit(RLIMIT_CORE, &core) != 0 || setrlimit(RLIMIT_NOFILE, &descriptors) != 0 || setrlimit(RLIMIT_CPU, &cpu) != 0) return 66;
    mach_port_t canary = MACH_PORT_NULL;
    kern_return_t setup[LO_IPC_CHANNELS] = {-1, -1, -1, -1};
    int setup_failed = parent_canary(&canary, setup);
    if (setup_failed) {
        int cleanup_failed = lo_normalize_inherited_rights();
        if (MACH_PORT_VALID(canary) && mach_port_destroy(mach_task_self(), canary) != KERN_SUCCESS) cleanup_failed = 1;
        printf("{\"kind\":\"controller\",\"variant\":\"%s\",\"setup_failed\":true,\"setup_status\":[%d,%d,%d,%d],\"child_created\":false,\"cleanup_complete\":%s}\n", argv[1], setup[0], setup[1], setup[2], setup[3], cleanup_failed ? "false" : "true");
        return 67;
    }
    fflush(stdout);
    double started = monotonic();
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
                /* A lost wait relationship cannot justify a later PID signal.
                   Keep this controller alive for the outer retained-group
                   owner; do not report cleanup or attempt another child. */
                printf("{\"kind\":\"ownership-unresolved\",\"variant\":\"%s\",\"wait_errno\":%d}\n", argv[1], errno);
                fflush(stdout);
                for (;;) { struct timespec delay = {0, 100000000}; nanosleep(&delay, NULL); }
            }
            struct pollfd request = {control, POLLIN | POLLHUP, 0};
            int ready = poll(&request, 1, 10);
            if (ready < 0 && errno != EINTR) cancelled = 1;
            if (ready > 0 && request.revents != 0) {
                unsigned char message;
                (void)read(control, &message, 1);
                cancelled = 1; /* Every post-admission message or EOF cancels. */
            }
            double now = monotonic();
            if (now - started >= 5) timed_out = 1;
            if ((cancelled || timed_out || cleanup_deadline_exceeded) && !signal_attempted) {
                signal_attempted = 1;
                stop_started = now;
                if (kill(child, SIGKILL) != 0) signal_error = errno;
            }
            if (signal_attempted && now - stop_started >= 2) cleanup_deadline_exceeded = 1;
        }
    }
    unsigned int received[LO_IPC_CHANNELS] = {0, 0, 0, 0};
    int messages_failed = child > 0 && joined ? messages(canary, received) : 1;
    int rights_failed = lo_normalize_inherited_rights();
    if (mach_port_destroy(mach_task_self(), canary) != KERN_SUCCESS) rights_failed = 1;
    int cleanup_complete = (child < 0 || joined) && rights_failed == 0 && close(control) == 0;
    printf("{\"kind\":\"controller\",\"variant\":\"%s\",\"setup_failed\":false,\"setup_status\":[%d,%d,%d,%d],\"child_created\":%s,\"child_joined\":%s,\"exited\":%s,\"exit_code\":%d,\"signal\":%d,\"cancelled\":%s,\"timed_out\":%s,\"signal_attempted\":%s,\"signal_errno\":%d,\"cleanup_deadline_exceeded\":%s,\"canary_messages\":[%u,%u,%u,%u],\"message_cleanup_complete\":%s,\"cleanup_complete\":%s,\"elapsed_seconds\":%.6f}\n", argv[1], setup[0], setup[1], setup[2], setup[3], child > 0 ? "true" : "false", joined ? "true" : "false", joined && WIFEXITED(status) ? "true" : "false", joined && WIFEXITED(status) ? WEXITSTATUS(status) : -1, joined && WIFSIGNALED(status) ? WTERMSIG(status) : 0, cancelled ? "true" : "false", timed_out ? "true" : "false", signal_attempted ? "true" : "false", signal_error, cleanup_deadline_exceeded ? "true" : "false", received[0], received[1], received[2], received[3], messages_failed ? "false" : "true", cleanup_complete ? "true" : "false", monotonic() - started);
    return cleanup_complete && child > 0 && joined && WIFEXITED(status) && WEXITSTATUS(status) == 0 && !cancelled && !timed_out && !cleanup_deadline_exceeded && !messages_failed ? 0 : 69;
}
