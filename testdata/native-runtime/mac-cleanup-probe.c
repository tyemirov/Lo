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

static int batch_contains(int argc, char **argv, const char *path) {
    for (int index = 3; index < argc; ++index) {
        if (strcmp(argv[index], path) == 0) return 1;
    }
    return 0;
}

int main(int argc, char **argv) {
    const char *calls = getenv("FIXTURE_CALLS");
    int prior = calls ? open(calls, O_RDONLY) : -1;
    char byte;
    int first = prior >= 0 && read(prior, &byte, 1) == 0;
    if (prior >= 0) close(prior);
    if (argc < 3 || !append_call(argc, argv)) return 70;
    int gateway = strcmp(argv[1], "-Fn") == 0;
    int tree = strcmp(argv[1], "-F0n") == 0 && strcmp(argv[2], "+D") == 0;
    if (gateway || tree ? (argc != 4 || strcmp(argv[2], "+D") != 0)
                : (strcmp(argv[1], "-F0n") != 0 || strcmp(argv[2], "--") != 0 || argc > 103)) return 71;
    const char *behavior = getenv("FIXTURE_BEHAVIOR");
    if (!behavior) return 72;
    if (first && strcmp(behavior, "first-fails") == 0) fputs("first probe failed\n", stderr);
    if (!first && strcmp(behavior, "later-fails") == 0) fputs("later probe failed\n", stderr);
    if (strcmp(behavior, "stderr") == 0) fputs("probe failed\n", stderr);
    if (strcmp(behavior, "whitespace") == 0) fputs(" \t\n", stderr);
    if (strcmp(behavior, "status2") == 0) return 2;
    if (strcmp(behavior, "malformed") == 0) {
        const unsigned char invalid[] = {'n', 0xff, 0};
        return fwrite(invalid, 1, sizeof(invalid), stdout) == sizeof(invalid) ? 0 : 73;
    }
    const char *changed = getenv("FIXTURE_CHANGED");
    if (changed && *changed && (strcmp(behavior, "scoped") != 0 || batch_contains(argc, argv, changed))) {
        if (tree && (strcmp(behavior, "rename-root") == 0 || strcmp(behavior, "replace-root") == 0)) {
            char retained[4096];
            if (snprintf(retained, sizeof(retained), "%s-retained", changed) >= (int)sizeof(retained) || rename(changed, retained)) return 74;
            if (strcmp(behavior, "replace-root") == 0) {
                char replacement[4096];
                if (snprintf(replacement, sizeof(replacement), "%s-replacement", changed) >= (int)sizeof(replacement) || rename(replacement, changed)) return 74;
            }
        } else if (strcmp(behavior, "vanish") == 0) {
            if (unlink(changed)) return 74;
        } else {
            int descriptor = open(changed, O_WRONLY | O_TRUNC);
            if (descriptor < 0 || write(descriptor, "changed", 7) != 7 || close(descriptor)) return 74;
        }
    }
    const char *opened = getenv("FIXTURE_OPENED");
    if (opened && *opened && strcmp(behavior, "scoped") == 0 && !batch_contains(argc, argv, opened)) opened = "";
    if (opened && *opened) {
        if (gateway || strcmp(behavior, "newline-framed-open") == 0) printf("p123\nn%s\n", opened);
        else if (strcmp(behavior, "unterminated-open") == 0) {
            if (fwrite("p123\0n", 1, 6, stdout) != 6 || fwrite(opened, 1, strlen(opened), stdout) != strlen(opened)) return 75;
        }
        else if (strcmp(behavior, "embedded-newline-open") == 0) {
            printf("p123\nn%s", opened);
            if (fwrite("\0\n", 1, 2, stdout) != 2) return 75;
        }
        else if (strcmp(behavior, "record-boundaries") == 0) {
            const char *headers[] = {"p123\0\nf3\0n", "p456\0\nf4\0n"};
            for (int index = 0; index < 2; ++index) {
                if (fwrite(headers[index], 1, 10, stdout) != 10 ||
                    fwrite(opened, 1, strlen(opened), stdout) != strlen(opened) ||
                    fwrite("\0\n", 1, 2, stdout) != 2) return 75;
            }
        }
        else {
            if (fwrite("p123\0\nn", 1, 7, stdout) != 7 ||
                fwrite(opened, 1, strlen(opened), stdout) != strlen(opened) ||
                fwrite("\0\n", 1, 2, stdout) != 2) return 75;
        }
    }
    return opened && *opened ? 0 : 1;
}
