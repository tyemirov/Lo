#include <errno.h>
#include <libproc.h>
#include <mach/mach.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/event.h>
#include <sys/resource.h>
#include <sys/wait.h>
#include <unistd.h>

struct report {
    pid_t pid, parent, group, session;
    audit_token_t token;
};

static void send_report(int fd) {
    struct report report = {
        .pid = getpid(), .parent = getppid(), .group = getpgrp(), .session = getsid(0)
    };
    mach_msg_type_number_t count = TASK_AUDIT_TOKEN_COUNT;
    if (task_info(mach_task_self(), TASK_AUDIT_TOKEN, (task_info_t)&report.token, &count) != KERN_SUCCESS) {
        _exit(29);
    }
    if (write(fd, &report, sizeof(report)) != sizeof(report)) _exit(30);
}

static int receive_report(int fd, struct report *report) {
    return read(fd, report, sizeof(*report)) == sizeof(*report);
}

static int observe_exit(int queue) {
    struct kevent event;
    struct timespec timeout = {6, 0};
    int count = kevent(queue, NULL, 0, &event, 1, &timeout);
    return count == 1 && (event.fflags & NOTE_EXIT);
}

int main(void) {
    struct rlimit no_core = {0, 0};
    if (setrlimit(RLIMIT_CORE, &no_core)) return 10;
    alarm(12);
    signal(SIGPIPE, SIG_IGN);
    int reports[2], control[2];
    if (pipe(reports)) return 11;
    if (pipe(control)) {
        close(reports[0]);
        close(reports[1]);
        return 11;
    }
    pid_t leader = fork();
    if (leader < 0) {
        close(reports[0]);
        close(reports[1]);
        close(control[0]);
        close(control[1]);
        return 12;
    }
    if (!leader) {
        alarm(5);
        close(reports[0]);
        close(control[1]);
        if (setsid() < 0) _exit(13);
        pid_t intermediate = fork();
        if (intermediate < 0) _exit(14);
        if (!intermediate) {
            alarm(5);
            pid_t detached = fork();
            if (detached < 0) _exit(15);
            if (detached) _exit(0);
            alarm(5);
            if (setsid() < 0) _exit(16);
            send_report(reports[1]);
            char byte = 0;
            if (read(control[0], &byte, 1) != 1) _exit(17);
            send_report(reports[1]);
            if (read(control[0], &byte, 1) != 1) _exit(18);
            _exit(0);
        }
        int status = 0;
        if (waitpid(intermediate, &status, 0) != intermediate || status) _exit(19);
        _exit(0);
    }

    close(reports[1]);
    close(control[0]);
    struct report initial, current;
    int result = 20, leader_reaped = 0, token_known = 0, confirmed = 0, queue = -1;
    if (!receive_report(reports[0], &initial)) goto cleanup;
    token_known = 1;
    queue = kqueue();
    if (queue < 0) {
        result = 23;
        goto cleanup;
    }
    struct kevent change;
    EV_SET(&change, initial.pid, EVFILT_PROC, EV_ADD | EV_ENABLE | EV_CLEAR, NOTE_EXIT, 0, NULL);
    if (kevent(queue, &change, 1, NULL, 0, NULL)) {
        close(queue);
        queue = -1;
        result = 24;
        goto cleanup;
    }
    int status = 0;
    if (waitpid(leader, &status, 0) != leader) {
        result = 21;
        goto cleanup;
    }
    leader_reaped = 1;
    if (status) {
        result = 21;
        goto cleanup;
    }
    if (write(control[1], "R", 1) != 1 || !receive_report(reports[0], &current)) {
        result = 22;
        goto cleanup;
    }
    pid_t children[32];
    int bytes = proc_listchildpids(leader, children, sizeof(children));
    errno = 0;
    int group_result = kill(-leader, 0), group_error = errno;
    int same_identity = !memcmp(&initial.token, &current.token, sizeof(initial.token));
    printf("{\"case\":\"ancestry-after-leader-exit\",\"leader\":%d,\"child\":%d,\"child_parent\":%d,\"child_group\":%d,\"child_session\":%d,\"listed_child_bytes\":%d,\"group_result\":%d,\"group_errno\":%d,\"same_identity\":%s}\n",
        leader, current.pid, current.parent, current.group, current.session, bytes, group_result, group_error, same_identity ? "true" : "false");
    if (!same_identity || write(control[1], "X", 1) != 1) {
        result = 25;
        goto cleanup;
    }
    confirmed = observe_exit(queue);
    result = !confirmed;

cleanup:
    /* EOF releases the helper even if its audit-token report did not arrive. */
    close(control[1]);
    if (token_known && !confirmed) {
        int signal_result = proc_signal_with_audittoken(&initial.token, SIGKILL);
        if (signal_result && signal_result != ESRCH) result = 26;
        if (queue >= 0) confirmed = observe_exit(queue);
    }
    if (!leader_reaped) {
        /* An unreaped direct child retains its PID; this signal cannot hit reuse. */
        if (kill(leader, SIGKILL) && errno != ESRCH) result = 27;
        if (waitpid(leader, NULL, 0) != leader) result = 28;
    }
    /* Each helper retains this writer until exit and has its own five-second alarm. */
    char discarded[256];
    while (read(reports[0], discarded, sizeof(discarded)) > 0) {}
    close(reports[0]);
    if (queue >= 0) close(queue);
    printf("{\"case\":\"controlled-descendant-cleanup\",\"exit_confirmed\":%s}\n", confirmed ? "true" : "false");
    return result;
}
