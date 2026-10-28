#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static int arguments_match(int argc, char **argv, const char *const *expected, int count) {
    if (argc != count + 1) return 0;
    for (int index = 0; index < count; index++)
        if (strcmp(argv[index + 1], expected[index])) return 0;
    return 1;
}

int main(int argc, char **argv) {
    const char *tool = strrchr(argv[0], '/');
    tool = tool ? tool + 1 : argv[0];
    const char *names[] = {"CI", "HOMEBREW_NO_AUTO_UPDATE", "HOMEBREW_NO_ANALYTICS", "GOTOOLCHAIN", "MISE_AUTO_INSTALL", "UV_PYTHON_DOWNLOADS", "LO_CLEANUP_INHERITED"};
    const char *values[] = {"1", "1", "1", "local", "0", "never", "retained"};
    for (int index = 0; index < 7; index++) {
        const char *value = getenv(names[index]);
        if (!value || strcmp(value, values[index])) return 70;
    }
    char directory[4096];
    const char *expected_directory = getenv("LO_CLEANUP_DIRECTORY");
    const char *root = getenv("LO_CLEANUP_ROOT");
    if (!root || !expected_directory || !getcwd(directory, sizeof(directory)) || strcmp(directory, expected_directory)) return 71;
    char input;
    if (read(STDIN_FILENO, &input, 1) != 0) return 72;

    const char *npm[] = {"cache", "clean", "--force"};
    const char *go[] = {"clean", "-cache", "-testcache"};
    const char *uv[] = {"cache", "clean", "--force"};
    const char *pnpm[] = {"store", "prune", "--yes"};
    const char *pip_probe[] = {"-m", "pip", "--version"};
    const char *pip_clean[] = {"-m", "pip", "cache", "purge"};
    if (!strcmp(tool, "python3") && arguments_match(argc, argv, pip_probe, 3)) {
        if (getenv("LO_CLEANUP_PIP_ABSENT")) return 1;
        return fputs("pip fixture\n", stdout) < 0 ? 74 : 0;
    }
    if (!((!strcmp(tool, "npm") && arguments_match(argc, argv, npm, 3)) ||
          (!strcmp(tool, "go") && arguments_match(argc, argv, go, 3)) ||
          (!strcmp(tool, "uv") && arguments_match(argc, argv, uv, 3)) ||
          (!strcmp(tool, "pnpm") && arguments_match(argc, argv, pnpm, 3)) ||
          (!strcmp(tool, "python3") && arguments_match(argc, argv, pip_clean, 4)))) return 73;
    if (getenv("LO_CLEANUP_FAIL")) {
        if (fputs("package fixture failure\n", stderr) < 0) return 74;
        return 7;
    }
    char target[4096];
    int length = snprintf(target, sizeof(target), "%s/%s.cache", root, tool);
    if (length < 0 || (size_t)length >= sizeof(target)) return 75;
    if (unlink(target)) return errno;
    return fputs("cleaned\n", stdout) < 0 ? 74 : 0;
}
