#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

int main(int argc, char **argv) {
    if (argc < 5 || strcmp(argv[1], "--context") || strcmp(argv[2], "desktop-linux")) return 64;
    const char *method = NULL, *path = NULL;
    if (argc == 5 && !strcmp(argv[3], "system") && !strcmp(argv[4], "df")) {
        method = "GET"; path = "/v1.47/system/df";
    } else if (argc == 9 && !strcmp(argv[3], "buildx") && !strcmp(argv[4], "prune") &&
               !strcmp(argv[5], "--builder") && !strcmp(argv[6], "desktop-linux") &&
               !strcmp(argv[7], "--all") && !strcmp(argv[8], "--force")) {
        method = "POST"; path = "/v1.47/build/prune?all=true";
    } else if (argc == 7 && !strcmp(argv[3], "image") && !strcmp(argv[4], "prune") &&
               !strcmp(argv[5], "--all") && !strcmp(argv[6], "--force")) {
        method = "POST"; path = "/v1.47/images/prune?all=true";
    } else return 64;
    char input;
    if (read(STDIN_FILENO, &input, 1) != 0) return 65;
    const char *endpoint = getenv("LO_CLEANUP_DOCKER_SOCKET");
    struct sockaddr_un address = {0};
    address.sun_family = AF_UNIX;
    if (!endpoint || strlen(endpoint) >= sizeof(address.sun_path)) return 66;
    strcpy(address.sun_path, endpoint);
    int descriptor = socket(AF_UNIX, SOCK_STREAM, 0);
    if (descriptor < 0) return errno;
    if (connect(descriptor, (const struct sockaddr *)&address, sizeof(address))) {
        int error = errno;
        close(descriptor);
        return error;
    }
    if (dprintf(descriptor, "%s %s HTTP/1.1\r\nHost: docker-fixture\r\nConnection: close\r\nContent-Length: 0\r\nX-Fixture-Pid: %ld\r\n\r\n", method, path, (long)getpid()) < 0) { close(descriptor); return 67; }
    FILE *response = fdopen(descriptor, "rb");
    if (!response) { close(descriptor); return 68; }
    char buffer[4096] = {0};
    int status = 0, error = 0;
    if (!fgets(buffer, sizeof(buffer), response) || strncmp(buffer, "HTTP/1.1 ", 9) ||
        sscanf(buffer + 9, "%d", &status) != 1 || status < 200 || status > 599) error = 68;
    size_t headers = strlen(buffer);
    while (!error) {
        if (!fgets(buffer, sizeof(buffer), response)) { error = 68; break; }
        headers += strlen(buffer);
        if (headers > 65536 || !strchr(buffer, '\n')) { error = 68; break; }
        if (!strcmp(buffer, "\r\n")) break;
    }
    if (!error) {
        FILE *output = status >= 400 ? stderr : stdout;
        size_t count;
        while ((count = fread(buffer, 1, sizeof(buffer), response)) > 0) {
            if (fwrite(buffer, 1, count, output) != count) { error = 68; break; }
        }
        if (ferror(response)) error = 68;
        if (!error && status >= 400) error = 7;
    }
    if (fclose(response) && !error) error = 68;
    return error;
}
