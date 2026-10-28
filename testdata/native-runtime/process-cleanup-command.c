#include <errno.h>
#include <stdio.h>
#include <spawn.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <sys/wait.h>
#include <unistd.h>

extern char **environ;

static int write_file(const char *path) {
    FILE *file = fopen(path, "wb");
    if (!file) return errno;
    int error = fwrite("changed", 1, 7, file) == 7 ? 0 : errno;
    if (fclose(file) && !error) error = errno;
    return error;
}

static int connect_endpoint(const char *path) {
    int descriptor = socket(AF_UNIX, SOCK_STREAM, 0);
    if (descriptor < 0) return errno;
    struct sockaddr_un address = {0};
    address.sun_family = AF_UNIX;
    if (strlen(path) >= sizeof(address.sun_path)) { close(descriptor); return EINVAL; }
    strcpy(address.sun_path, path);
    int error = connect(descriptor, (const struct sockaddr *)&address, sizeof(address)) < 0 ? errno : 0;
    if (close(descriptor) && !error) error = errno;
    return error;
}

int main(int argc, char **argv) {
    if (argc < 3) return EINVAL;
    if (!strcmp(argv[1], "write")) return write_file(argv[2]);
    if (!strcmp(argv[1], "flood") || !strcmp(argv[1], "wait")) {
        int error = write_file(argv[2]);
        if (error) return error;
        if (!strcmp(argv[1], "flood")) {
            char buffer[65536];
            memset(buffer, 'x', sizeof(buffer));
            for (int index = 0; index < 17; index++) {
                size_t sent = 0;
                while (sent < sizeof(buffer)) {
                    ssize_t count = write(STDOUT_FILENO, buffer + sent, sizeof(buffer) - sent);
                    if (count <= 0) return errno;
                    sent += (size_t)count;
                }
            }
        }
        for (;;) pause();
    }
    if (!strcmp(argv[1], "unlink")) return unlink(argv[2]) < 0 ? errno : 0;
    if (!strcmp(argv[1], "connect")) return connect_endpoint(argv[2]);
    if (!strcmp(argv[1], "exec-write") && argc == 4) {
        char *arguments[] = {argv[2], "write", argv[3], NULL};
        execv(argv[2], arguments);
        return errno;
    }
    if (!strcmp(argv[1], "link") && argc == 4) return link(argv[2], argv[3]) < 0 ? errno : 0;
    if (!strcmp(argv[1], "rename") && argc == 4) return rename(argv[2], argv[3]) < 0 ? errno : 0;
    if (!strcmp(argv[1], "symlink-write") && argc == 4) {
        if (symlink(argv[2], argv[3]) < 0) return errno;
        return write_file(argv[3]);
    }
    if (!strcmp(argv[1], "fork")) {
        pid_t child = fork();
        if (child < 0) return errno;
        if (!child) _exit(0);
        int status;
        while (waitpid(child, &status, 0) < 0) if (errno != EINTR) return errno;
        return WIFEXITED(status) ? WEXITSTATUS(status) : ECHILD;
    }
    if (!strcmp(argv[1], "spawn") && argc == 4) {
        pid_t child;
        char *arguments[] = {argv[2], "write", argv[3], NULL};
        if (posix_spawn(&child, argv[2], NULL, NULL, arguments, environ)) return 1;
        int status;
        while (waitpid(child, &status, 0) < 0) if (errno != EINTR) return errno;
        return WIFEXITED(status) ? WEXITSTATUS(status) : ECHILD;
    }
    return EINVAL;
}
