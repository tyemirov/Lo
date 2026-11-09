#define main confinement_control_main
#include "process-ownership-command.c"
#undef main
#include <dlfcn.h>
#include <sys/resource.h>

static void confinement_trace(const char *path, const char *phase, int result) {
    int descriptor = open(path, O_WRONLY | O_APPEND | O_CLOEXEC);
    if (descriptor < 0) _exit(100);
    char message[160];
    int count = snprintf(message, sizeof(message), "pid=%d %s=%d\n", getpid(), phase, result);
    if (count < 0 || (size_t)count >= sizeof(message)
        || fixture_io(descriptor, message, (size_t)count, 1) || close(descriptor)) _exit(101);
}
static int confinement_state(void) {
    struct rlimit processes;
    if (getuid() == 0 || getuid() != geteuid() || getrlimit(RLIMIT_NPROC, &processes)) return 80;
    return processes.rlim_cur == 4096 && processes.rlim_max == 4096 ? 0 : 81;
}
static int confinement_syscalls(void) {
    int state = confinement_state(); if (state) return state;
    uid_t user = getuid(); gid_t group = getgid();
#define DENIED(call) do { errno = 0; if ((call) != -1 || errno != EPERM) return 82; } while (0)
    DENIED(setuid(user));
    DENIED(seteuid(user));
    DENIED(setreuid(user, user));
    DENIED(setgid(group));
    DENIED(setegid(group));
    DENIED(setregid(group, group));
    struct rlimit processes, descriptors;
    if (getrlimit(RLIMIT_NPROC, &processes) || getrlimit(RLIMIT_NOFILE, &descriptors)) return 83;
    DENIED(setrlimit(RLIMIT_NPROC, &processes));
    DENIED(setrlimit(RLIMIT_NOFILE, &descriptors));
    processes.rlim_cur--;
    DENIED(setrlimit(RLIMIT_NPROC, &processes));
#undef DENIED
    return confinement_state();
}
static int confinement_wait(pid_t child) {
    int status;
    if (waitpid(child, &status, 0) != child || !WIFEXITED(status)) return 84;
    return WEXITSTATUS(status);
}
static int confinement_spawn(int argc, char **argv) {
    (void)argc;
    posix_spawnattr_t attributes;
    if (posix_spawnattr_init(&attributes)) return 85;
    int expected_denial = !strcmp(argv[2], "spawn-uid") || !strcmp(argv[2], "spawn-persona");
    int configured = 0;
    if (!strcmp(argv[2], "spawn-uid")) {
        int (*set_uid)(const posix_spawnattr_t *, uid_t) = dlsym(RTLD_DEFAULT, "posix_spawnattr_set_uid_np");
        configured = !set_uid || set_uid(&attributes, 0);
    } else if (!strcmp(argv[2], "spawn-persona")) {
        int (*set_persona)(const posix_spawnattr_t *, uid_t, uint32_t) = dlsym(RTLD_DEFAULT, "posix_spawnattr_set_persona_np");
        int (*set_uid)(const posix_spawnattr_t *, uid_t) = dlsym(RTLD_DEFAULT, "posix_spawnattr_set_persona_uid_np");
        configured = !set_persona || !set_uid || set_persona(&attributes, 0, 1) || set_uid(&attributes, 0);
    } else {
        int (*set_descriptors)(const posix_spawnattr_t *, uint32_t, uint32_t) = dlsym(RTLD_DEFAULT, "posix_spawnattr_set_filedesclimit_ext");
        configured = !set_descriptors || set_descriptors(&attributes, 64, 64)
            || posix_spawnattr_setflags(&attributes, POSIX_SPAWN_RESETIDS | POSIX_SPAWN_SETSID);
    }
    if (configured) { posix_spawnattr_destroy(&attributes); return 86; }
    audit_token_t actor; if (fixture_token(&actor)) { posix_spawnattr_destroy(&attributes); return 87; }
    char token[65]; fixture_token_text(&actor, token);
    char *arguments[] = {argv[0], argv[1], "spawn-state", argv[3], argv[4], "spawn", token, NULL};
    char *environment[] = {NULL}; pid_t child = 0;
    confinement_trace(argv[3], "spawn-enter", 0);
    int spawned = posix_spawn(&child, argv[0], NULL, &attributes, arguments, environment);
    confinement_trace(argv[3], "spawn-return", spawned);
    int destroyed = posix_spawnattr_destroy(&attributes);
    int child_status = spawned ? 0 : confinement_wait(child);
    confinement_trace(argv[3], "spawn-child-status", child_status);
    if (destroyed) return 88;
    if (expected_denial) return spawned == EPERM ? 0 : 89;
    return spawned ? 90 : child_status;
}
static int confinement_write(const char *path, int allowed) {
    errno = 0; int descriptor = open(path, O_WRONLY | O_TRUNC | O_NOFOLLOW | O_CLOEXEC);
    if (!allowed) {
        if (descriptor >= 0) { close(descriptor); return 91; }
        return errno == EPERM || errno == EACCES ? 0 : 92;
    }
    if (descriptor < 0) return 93;
    int failed = fixture_io(descriptor, "changed", 7, 1), closed = close(descriptor);
    return failed || closed ? 94 : 0;
}
static int confinement_endpoint(const char *path, int allowed) {
    int descriptor = socket(AF_UNIX, SOCK_STREAM, 0); if (descriptor < 0) return 95;
    struct sockaddr_un address = {0}; address.sun_family = AF_UNIX;
    if (strlen(path) >= sizeof(address.sun_path)) { close(descriptor); return 96; }
    strcpy(address.sun_path, path);
    errno = 0; int connected = connect(descriptor, (struct sockaddr *)&address, sizeof(address));
    int error = errno;
    if (!allowed) {
        int closed = close(descriptor);
        return connected == -1 && (error == EPERM || error == EACCES) && !closed ? 0 : 97;
    }
    unsigned char reply = 0;
    int failed = connected || fixture_io(descriptor, "x", 1, 1) || fixture_io(descriptor, &reply, 1, 0);
    int closed = close(descriptor);
    return failed || closed || reply != 'y' ? 98 : 0;
}
int main(int argc, char **argv) {
    if (argc > 1 && (!strcmp(argv[1], "--host-signal") || !strcmp(argv[1], "--host-alive")))
        return confinement_control_main(argc, argv);
    alarm(12);
    if (argc < 7) return 64;
    int trace = strncmp(argv[2], "scope-", 6) != 0;
    audit_token_t actor = {0}; uint32_t kind = FIXTURE_ENTER;
    if (!strcmp(argv[5], "spawn") || !strcmp(argv[5], "exec")) {
        if (fixture_parse_token(argv[6], &actor)) return 65;
        kind = !strcmp(argv[5], "spawn") ? FIXTURE_SPAWN : FIXTURE_EXEC;
    }
    if (fixture_report(argv[1], kind, &actor, argv[0])) return 66;
    if (trace) confinement_trace(argv[3], "ownership-accepted", 0);
    int state = confinement_state(); if (state) return state;
    if (!strcmp(argv[2], "syscalls-fork")) {
        if (fixture_token(&actor)) return 67;
        pid_t child = fork(); if (child < 0) return 68;
        if (!child) {
            alarm(10);
            if (fixture_report(argv[1], FIXTURE_FORK, &actor, NULL)) _exit(69);
            _exit(confinement_syscalls());
        }
        return confinement_wait(child);
    }
    if (!strcmp(argv[2], "spawn-state")) {
        struct rlimit descriptors;
        if (getrlimit(RLIMIT_NOFILE, &descriptors) || descriptors.rlim_cur != 64 || descriptors.rlim_max != 64)
            return 99;
        confinement_trace(argv[3], "inherited-limits-confirmed", 0);
        return confinement_syscalls();
    }
    if (!strncmp(argv[2], "spawn-", 6)) return confinement_spawn(argc, argv);
    if (!strcmp(argv[2], "scope-exec")) {
        if (argc != 12 || fixture_token(&actor)) return 70;
        char token[65]; fixture_token_text(&actor, token);
        char *arguments[] = {argv[7], argv[1], "scope-check", argv[3], argv[4], "exec", token,
            argv[7], argv[8], argv[9], argv[10], argv[11], NULL};
        char *environment[] = {NULL}; execve(argv[7], arguments, environment); return 71;
    }
    if (!strcmp(argv[2], "scope-check")) {
        if (argc != 12) return 72;
        int result = confinement_write(argv[8], 1);
        if (!result) result = confinement_write(argv[9], 0);
        if (!result) result = confinement_endpoint(argv[10], 1);
        if (!result) result = confinement_endpoint(argv[11], 0);
        return result;
    }
    return 73;
}
