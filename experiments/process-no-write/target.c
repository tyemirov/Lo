#include <errno.h>
#include <fcntl.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>
#include "fixture_paths.h"

static char report[16384];
static size_t used;
static bool cleanup_ok = true;
static bool first = true;

#define MARKER(text) do { static const char bytes[] = "LO_NO_WRITE " text "\n"; if (write(2, bytes, sizeof(bytes)-1) != sizeof(bytes)-1) _exit(90); } while (0)

static void closed(int descriptor) {
    if (descriptor >= 0 && close(descriptor) != 0) cleanup_ok = false;
}

static void observation(const char *name, int result, int error, bool readable, bool unreachable) {
    int count = snprintf(report + used, sizeof(report) - used,
        "%s\"%s\":{\"result\":%d,\"errno\":%d,\"read_exact\":%s,\"unreachable\":%s}",
        first ? "" : ",", name, result, error, readable ? "true" : "false", unreachable ? "true" : "false");
    if (count < 0 || (size_t)count >= sizeof(report) - used) _exit(91);
    used += (size_t)count;
    first = false;
}

static bool exact_read(const char *path, const char *baseline) {
    int input = open(path, O_RDONLY), reference = open(baseline, O_RDONLY);
    bool equal = input >= 0 && reference >= 0;
    unsigned char actual[4096], expected[4096];
    while (equal) {
        ssize_t left = read(input, actual, sizeof(actual));
        ssize_t right = read(reference, expected, sizeof(expected));
        if (left < 0 || right < 0 || left != right || (left > 0 && memcmp(actual, expected, (size_t)left) != 0)) equal = false;
        if (left <= 0 || right <= 0) break;
    }
    closed(input); closed(reference);
    return equal;
}

static void path_for(char path[1024], const char *selection, const char *name) {
    int count = snprintf(path, 1024, "cases/%s/%s", selection, name);
    if (count < 0 || count >= 1024) _exit(92);
}

static void open_attempt(const char *selection, const char *name, int flags, bool mapping) {
    char path[1024], baseline[1024];
    path_for(path, selection, name); path_for(baseline, selection, "read-baseline");
    bool readable = exact_read(path, baseline);
    int descriptor = open(path, flags);
    int error = descriptor < 0 ? errno : 0;
    observation(name, descriptor < 0 ? -1 : 0, error, readable, false);
    if (mapping) {
        if (descriptor < 0) observation("shared-mmap", -1, 0, readable, true);
        else {
            void *mapped = mmap(NULL, 1, PROT_READ | PROT_WRITE, MAP_SHARED, descriptor, 0);
            int map_error = mapped == MAP_FAILED ? errno : 0;
            observation("shared-mmap", mapped == MAP_FAILED ? -1 : 0, map_error, readable, false);
            if (mapped != MAP_FAILED) {
                ((unsigned char *)mapped)[0] = 'X';
                if (munmap(mapped, 1) != 0) cleanup_ok = false;
            }
        }
    } else if (descriptor >= 0 && (flags & O_TRUNC) == 0) {
        if (write(descriptor, "X", 1) != 1) cleanup_ok = false;
    }
    closed(descriptor);
}

int main(int argc, char **argv) {
    MARKER("target-entered");
    const char *selection = getenv("LO_NO_WRITE_CASE");
    if (argc != 3 || strcmp(argv[1], "values") != 0 || strcmp(argv[2], "fixed-argument") != 0 || selection == NULL) return 64;
    const char *group = NULL;
    if (strncmp(selection, "control-", 8) == 0) group = selection + 8;
    else if (strncmp(selection, "confined-", 9) == 0) group = selection + 9;
    else return 64;
    if (strcmp(group, "write") != 0 && strcmp(group, "namespace") != 0 && strcmp(group, "unix") != 0 && strcmp(group, "descriptors") != 0) return 64;
    used = (size_t)snprintf(report, sizeof(report), "{\"operations\":{");
    if (strcmp(group, "write") == 0) {
        open_attempt(selection, "writeonly", O_WRONLY, false);
        open_attempt(selection, "readwrite", O_RDWR, false);
        open_attempt(selection, "truncopen", O_WRONLY | O_TRUNC, false);
        open_attempt(selection, "mapfile", O_RDWR, true);
        char path[1024], baseline[1024];
        path_for(path, selection, "truncate"); path_for(baseline, selection, "read-baseline");
        bool readable = exact_read(path, baseline);
        int result = truncate(path, 0); int error = result < 0 ? errno : 0;
        observation("truncate", result, error, readable, false);
    } else if (strcmp(group, "namespace") == 0) {
        char source[1024], destination[1024], temporary[1024];
        path_for(temporary, selection, "temporary-XXXXXX");
        int descriptor = mkstemp(temporary); int error = descriptor < 0 ? errno : 0;
        observation("mkstemp", descriptor < 0 ? -1 : 0, error, false, false);
        if (descriptor >= 0) { closed(descriptor); if (unlink(temporary) != 0) cleanup_ok = false; }
        path_for(source, selection, "unlink-file");
        int result = unlink(source); error = result < 0 ? errno : 0;
        observation("unlink", result, error, false, false);
        path_for(source, selection, "rename-source"); path_for(destination, selection, "rename-destination");
        result = rename(source, destination); error = result < 0 ? errno : 0;
        observation("rename", result, error, false, false);
        path_for(source, selection, "link-source"); path_for(destination, selection, "link-destination");
        result = link(source, destination); error = result < 0 ? errno : 0;
        observation("link", result, error, false, false);
        path_for(destination, selection, "symlink-destination");
        result = symlink("symlink-source", destination); error = result < 0 ? errno : 0;
        observation("symlink", result, error, false, false);
        open_attempt(selection, "hard-alias", O_WRONLY, false);
        open_attempt(selection, "symlink-alias", O_WRONLY, false);
    } else if (strcmp(group, "unix") == 0) {
        struct sockaddr_un address;
        memset(&address, 0, sizeof(address)); address.sun_family = AF_UNIX;
        char path[1024]; path_for(path, selection, "socket");
        if (strlen(path) >= sizeof(address.sun_path)) return 65;
        memcpy(address.sun_path, path, strlen(path) + 1);
        int descriptor = socket(AF_UNIX, SOCK_STREAM, 0);
        if (descriptor < 0) return 66;
        int result = connect(descriptor, (struct sockaddr *)&address, sizeof(address)); int error = result < 0 ? errno : 0;
        observation("unix-connect", result, error, false, false);
        closed(descriptor);
    } else {
        const char *numbers = getenv("LO_CANARY_FDS");
        if (numbers == NULL) return 67;
        while (*numbers != '\0') {
            char *end = NULL;
            long descriptor = strtol(numbers, &end, 10);
            if (end == numbers || descriptor < 3 || descriptor > 97 || (*end != ',' && *end != '\0')) return 67;
            errno = 0;
            int result = fcntl((int)descriptor, F_GETFD); int error = result < 0 ? errno : 0;
            char name[32]; snprintf(name, sizeof(name), "fd-%ld", descriptor);
            observation(name, result < 0 ? -1 : 0, error, false, false);
            numbers = *end == ',' ? end + 1 : end;
        }
        int count = 0;
        bool standard = true;
        for (int descriptor = 0; descriptor < LO_FD_LIMIT; descriptor++) {
            errno = 0;
            int result = fcntl(descriptor, F_GETFD);
            if (result >= 0) count++;
            else if (errno != EBADF || descriptor < 3) standard = false;
        }
        observation("standard-fds", standard ? count : -1, 0, false, false);
    }
    int count = snprintf(report + used, sizeof(report) - used, "},\"cleanup_ok\":%s}\n", cleanup_ok ? "true" : "false");
    if (count < 0 || (size_t)count >= sizeof(report) - used) return 91;
    used += (size_t)count;
    if (write(1, report, used) != (ssize_t)used) return 91;
    MARKER("target-complete");
    return cleanup_ok ? 0 : 1;
}
