#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static int append_call(int argc, char **argv) {
    const char *path = getenv("FIXTURE_CALLS");
    int descriptor = path ? open(path, O_WRONLY | O_APPEND) : -1;
    if (descriptor < 0) return 0;
    for (int index = 1; index < argc; ++index) {
        if (write(descriptor, argv[index], strlen(argv[index])) < 0 ||
            write(descriptor, "\n", 1) != 1) {
            close(descriptor);
            return 0;
        }
    }
    return close(descriptor) == 0;
}

int main(int argc, char **argv) {
    const char *calls = getenv("FIXTURE_CALLS");
    int prior = calls ? open(calls, O_RDONLY) : -1;
    char byte;
    int first = prior >= 0 && read(prior, &byte, 1) == 0;
    if (prior >= 0) close(prior);
    if (argc < 3 || !append_call(argc, argv)) return 70;
    int gateway = strcmp(argv[1], "-Fn") == 0;
    if (gateway ? (argc != 4 || strcmp(argv[2], "+D") != 0)
                : (strcmp(argv[1], "-F0n") != 0 || strcmp(argv[2], "--") != 0 || argc > 103)) return 71;
    const char *behavior = getenv("FIXTURE_BEHAVIOR");
    if (!behavior) return 72;
    if (first && strcmp(behavior, "first-fails") == 0) fputs("first probe failed\n", stderr);
    if (strcmp(behavior, "stderr") == 0) fputs("probe failed\n", stderr);
    if (strcmp(behavior, "whitespace") == 0) fputs(" \t\n", stderr);
    if (strcmp(behavior, "status2") == 0) return 2;
    if (strcmp(behavior, "malformed") == 0) {
        const unsigned char invalid[] = {'n', 0xff, 0};
        return fwrite(invalid, 1, sizeof(invalid), stdout) == sizeof(invalid) ? 0 : 73;
    }
    const char *changed = getenv("FIXTURE_CHANGED");
    if (changed && *changed) {
        if (strcmp(behavior, "vanish") == 0) {
            if (unlink(changed)) return 74;
        } else {
            int descriptor = open(changed, O_WRONLY | O_TRUNC);
            if (descriptor < 0 || write(descriptor, "changed", 7) != 7 || close(descriptor)) return 74;
        }
    }
    const char *opened = getenv("FIXTURE_OPENED");
    if (opened && *opened) {
        if (gateway) printf("p123\nn%s\n", opened);
        else {
            if (fwrite("p123\0n", 1, 6, stdout) != 6 ||
                fwrite(opened, 1, strlen(opened), stdout) != strlen(opened) ||
                fwrite("\0\n", 1, 2, stdout) != 2) return 75;
        }
    }
    return opened && *opened ? 0 : 1;
}
