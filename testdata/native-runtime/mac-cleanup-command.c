#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static int is(const char *left, const char *right) {
    return left && strcmp(left, right) == 0;
}

static int record(int argc, char **argv, const char *tool) {
    const char *path = getenv("FIXTURE_CALLS");
    int descriptor = path ? open(path, O_WRONLY | O_APPEND) : -1;
    if (descriptor < 0 || write(descriptor, tool, strlen(tool)) < 0) return 0;
    for (int index = 1; index < argc; ++index) {
        if (write(descriptor, "\t", 1) != 1 ||
            write(descriptor, argv[index], strlen(argv[index])) < 0) {
            close(descriptor);
            return 0;
        }
    }
    return write(descriptor, "\n", 1) == 1 && close(descriptor) == 0;
}

int main(int argc, char **argv) {
    const char *tool = strrchr(argv[0], '/');
    tool = tool ? tool + 1 : argv[0];
    const char *behavior = getenv("FIXTURE_BEHAVIOR");
    if (!record(argc, argv, tool)) return 70;
    const char *policy[][2] = {
        {"CI", "1"}, {"HOMEBREW_NO_AUTO_UPDATE", "1"}, {"HOMEBREW_NO_ANALYTICS", "1"},
        {"GOTOOLCHAIN", "local"}, {"MISE_AUTO_INSTALL", "0"}, {"UV_PYTHON_DOWNLOADS", "never"}
    };
    for (unsigned index = 0; index < sizeof(policy) / sizeof(policy[0]); ++index) {
        if (!is(getenv(policy[index][0]), policy[index][1])) return 78;
    }
    char input;
    if (read(0, &input, 1) != 0) return 78;

    if (is(tool, "getconf")) {
        const char *root = getenv("FIXTURE_USER_TEMP");
        if (argc != 2 || !is(argv[1], "DARWIN_USER_TEMP_DIR") || !root) return 71;
        puts(root);
        if (is(behavior, "temp-stderr")) fputs("unexpected discovery error\n", stderr);
        return is(behavior, "temp-failure") ? 2 : 0;
    }
    if (is(tool, "du")) {
        if (argc < 4 || !is(argv[1], "-k") || !is(argv[2], "-s")) return 72;
        if (is(behavior, "inventory-malformed")) puts("malformed inventory");
        else if (is(behavior, "inventory-empty")) { }
        else if (is(behavior, "inventory-partial")) printf("1024\t%s\n", argv[3]);
        else if (is(behavior, "inventory-duplicate")) for (int index = 3; index < argc; ++index) printf("1024\t%s\n", argv[3]);
        else if (is(behavior, "inventory-unknown")) for (int index = 3; index < argc; ++index) printf("1024\t%s\n", index == 3 ? "/unselected/fixture" : argv[index]);
        else if (is(behavior, "inventory-negative")) for (int index = 3; index < argc; ++index) printf("-1\t%s\n", argv[index]);
        else for (int index = 3; index < argc; ++index) printf("%d\t%s\n", index * 1024, argv[index]);
        if (is(behavior, "inventory-stderr")) fputs("incomplete inventory\n", stderr);
        return is(behavior, "inventory-failure") ? 2 : 0;
    }
    if (is(tool, "lsof")) {
        if (argc < 4 || !(is(argv[1], "-F0n") || is(argv[1], "-Fn")) ||
            !(is(argv[2], "--") || is(argv[2], "+D"))) return 73;
        if (is(behavior, "probe-failure")) fputs("controlled probe error\n", stderr);
        return 1;
    }
    if (is(tool, "docker")) {
        if (argc < 5 || !is(argv[1], "--context") || !is(argv[2], "desktop-linux")) return 74;
        if (is(argv[3], "context") && is(argv[4], "inspect")) {
            if (argc != 6 || !is(argv[5], "desktop-linux")) return 74;
            const char *document = getenv("FIXTURE_DOCKER_JSON");
            if (!document) return 74;
            puts(document);
            if (is(behavior, "docker-context-stderr")) fputs("incomplete context\n", stderr);
            return is(behavior, "docker-context-failure") ? 3 : 0;
        }
        if (is(argv[3], "buildx") && is(argv[4], "inspect")) {
            if (argc != 6 || !is(argv[5], "desktop-linux")) return 74;
            const char *builder = getenv("FIXTURE_DOCKER_BUILDER");
            if (!builder) return 74;
            puts(builder);
            if (is(behavior, "docker-builder-stderr")) fputs("incomplete builder\n", stderr);
            return is(behavior, "docker-builder-failure") ? 3 : 0;
        }
        if (argc == 5 && is(argv[3], "system") && is(argv[4], "df")) {
            puts("fixture Docker inventory");
            return is(behavior, "docker-usage-failure") ? 3 : 0;
        }
        if (argc == 9 && is(argv[3], "buildx") && is(argv[4], "prune") &&
            is(argv[5], "--builder") && is(argv[6], "desktop-linux") &&
            is(argv[7], "--all") && is(argv[8], "--force")) return is(behavior, "docker-build-failure") ? 3 : 0;
        if (argc == 7 && is(argv[3], "image") && is(argv[4], "prune") &&
            is(argv[5], "--all") && is(argv[6], "--force")) return is(behavior, "docker-image-failure") ? 3 : 0;
        return 74;
    }
    if (is(tool, "python3")) {
        if (argc == 4 && is(argv[1], "-m") && is(argv[2], "pip") && is(argv[3], "--version")) {
            puts("pip fixture");
            return is(behavior, "pip-absent") ? 1 : 0;
        }
        if (argc != 5 || !is(argv[1], "-m") || !is(argv[2], "pip") ||
            !is(argv[3], "cache") || !is(argv[4], "purge")) return 75;
    } else if (is(tool, "npm")) {
        if (argc != 4 || !is(argv[1], "cache") || !is(argv[2], "clean") || !is(argv[3], "--force")) return 76;
        if (is(behavior, "npm-failure") || is(behavior, "multiple-failures")) return 3;
    } else if (is(tool, "go")) {
        if (argc != 4 || !is(argv[1], "clean") || !is(argv[2], "-cache") || !is(argv[3], "-testcache")) return 76;
    } else if (is(tool, "uv")) {
        if (argc != 4 || !is(argv[1], "cache") || !is(argv[2], "clean") || !is(argv[3], "--force")) return 76;
        if (is(behavior, "multiple-failures")) return 4;
    } else if (is(tool, "pnpm")) {
        if (argc != 4 || !is(argv[1], "store") || !is(argv[2], "prune") || !is(argv[3], "--yes")) return 76;
    } else return 77;
    printf("%s fixture complete\n", tool);
    return 0;
}
