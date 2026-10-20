#include <errno.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <stdio.h>
#include <sys/socket.h>
#include <sys/wait.h>
#include <unistd.h>

int main(int argc, char **argv) {
    if (argc != 3) return 2;
    pid_t child = fork();
    if (child < 0) return 3;
    if (child == 0) {
        int fd = open(argv[1], O_WRONLY | O_APPEND);
        if (fd >= 0) { close(fd); _exit(4); }
        _exit(errno == EPERM || errno == EACCES || errno == EROFS ? 0 : 5);
    }
    int status;
    if (waitpid(child, &status, 0) != child || !WIFEXITED(status) || WEXITSTATUS(status) != 0) return 6;
    char writable[4096];
    if (snprintf(writable, sizeof writable, "%s/guard-write", argv[2]) >= (int)sizeof writable) return 7;
    int fd = open(writable, O_WRONLY | O_TRUNC);
    if (fd < 0) return 8;
    if (write(fd, "ok", 2) != 2) { close(fd); return 9; }
    close(fd);
    if (unlink(writable) != 0) return 10;
    int sock = socket(AF_INET, SOCK_STREAM, 0);
    int denied = 0;
    if (sock < 0) denied = errno == EPERM || errno == EACCES;
    else {
        struct sockaddr_in address = {0};
        address.sin_family = AF_INET;
        address.sin_port = htons(9);
        address.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
        if (connect(sock, (struct sockaddr *)&address, sizeof address) < 0)
            denied = errno == EPERM || errno == EACCES || errno == ENETUNREACH;
        close(sock);
    }
    if (!denied) return 11;
    puts("{\"readonly_write\":\"denied\",\"network\":\"denied\",\"descendant\":\"denied\",\"writable_state\":\"allowed\"}");
    return 0;
}
