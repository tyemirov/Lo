/* Controlled external tools for native compiler boundary tests. */
extern int open(const char *, int, ...);
extern int close(int);
extern long write(int, const void *, unsigned long);
extern int ftruncate(int, long);
extern char *getcwd(char *, unsigned long);
extern char *getenv(const char *);
extern unsigned long strlen(const char *);
extern int strcmp(const char *, const char *);
extern int fork(void);
extern void _exit(int);
struct timespec { long seconds, nanoseconds; };
extern int nanosleep(const struct timespec *, struct timespec *);
static char work[4096], root[4096], path[8192], json[131072];
static unsigned long used;

static char *last_separator(char *text) {
    char *last = 0;
    for (; *text; text++) if (*text == '/') last = text;
    return last;
}
static int emit(int descriptor, const char *bytes, unsigned long length) {
    while (length) {
        long count = write(descriptor, bytes, length);
        if (count <= 0) return -1;
        bytes += count; length -= (unsigned long)count;
    }
    return 0;
}
static int text(const char *name, const char *contents) {
    unsigned long length = strlen(root), suffix = strlen(name);
    if (length + suffix + 2 > sizeof(path)) return 50;
    for (unsigned long index = 0; index < length; index++) path[index] = root[index];
    path[length++] = '/';
    for (unsigned long index = 0; index <= suffix; index++) path[length + index] = name[index];
    int descriptor = open(path, 1);
    if (descriptor < 0) return 51;
    int failed = emit(descriptor, contents, strlen(contents));
    return close(descriptor) != 0 || failed ? 52 : 0;
}
static int append(char byte) {
    if (used + 1 >= sizeof(json)) return -1;
    json[used++] = byte;
    return 0;
}
static int json_string(const char *text) {
    static const char digits[] = "0123456789abcdef";
    if (append('"')) return -1;
    for (const unsigned char *byte = (const unsigned char *)text; *byte; byte++) {
        if (*byte < 32) {
            if (append('\\') || append('u') || append('0') || append('0') || append(digits[*byte >> 4]) || append(digits[*byte & 15])) return -1;
        } else {
            if ((*byte == '"' || *byte == '\\') && append('\\')) return -1;
            if (append((char)*byte)) return -1;
        }
    }
    return append('"');
}
static void delay(long seconds, long nanoseconds) {
    struct timespec duration = {seconds, nanoseconds};
    nanosleep(&duration, 0);
}
int main(int argc, char **argv) {
    if (!getcwd(work, sizeof(work))) return 53;
    unsigned long length = strlen(work);
    for (unsigned long index = 0; index <= length; index++) root[index] = work[index];
    char *separator = last_separator(root);
    if (!separator) return 54;
    *separator = 0;
    separator = last_separator(root);
    if (!separator) return 55;
    *separator = 0;
    if (text("observed-work", work) != 0) return 56;
    const char *name = last_separator(argv[0]);
    name = name ? name + 1 : argv[0];
    const char *action = name;
    while (*action && *action != '-') action++;
    if (*action == '-') action++;
    if (strcmp(action, "paths") == 0) {
        const char *keys[] = {"HOME", "TMPDIR", "TMP", "TEMP", "PATH", "XDG_CACHE_HOME", "CCACHE_DIR", "CLANG_MODULE_CACHE_PATH", "GOCACHE", "GOMODCACHE", "GOPATH", "GOTOOLCHAIN", "GOWORK", "GOFLAGS", "CC", "CFLAGS", "CI", "LC_ALL", "LANG", "ZERO_AR_DATE", 0};
        if (append('{') || json_string("work") || append(':') || json_string(work)) return 57;
        for (unsigned long index = 0; keys[index]; index++) {
            const char *value = getenv(keys[index]);
            if (append(',') || json_string(keys[index]) || append(':') || json_string(value ? value : "")) return 58;
        }
        if (append('}')) return 59;
        json[used] = 0;
        if (text("paths.json", json)) return 60;
        emit(2, "controlled path observation\n", 28);
        return 42;
    }
    if (strcmp(action, "failure") == 0) {
        const char *message = "controlled compiler failure\n";
        emit(2, message, strlen(message));
        return 42;
    }
    if (strcmp(action, "overflow") == 0) {
        static char bytes[4096];
        for (unsigned long index = 0; index < sizeof(bytes); index++) bytes[index] = 'x';
        for (unsigned long index = 0; index < 512; index++) if (emit(2, bytes, sizeof(bytes))) return 61;
        delay(10, 0);
        return 42;
    }
    if (strcmp(action, "artifact-overflow") == 0) {
        const char *output = 0;
        int compile = 0;
        for (int index = 1; index < argc; index++) {
            if (strcmp(argv[index], "-c") == 0) compile = 1;
            if (strcmp(argv[index], "-o") == 0 && index + 1 < argc) output = argv[++index];
        }
        if (!output) return 62;
        if (compile) return 0;
        int descriptor = open(output, 1);
        if (descriptor < 0) return 63;
        int failed = ftruncate(descriptor, (64L << 20) + 1) != 0;
        if (close(descriptor) != 0 || failed) return 64;
        return 0;
    }
    if (strcmp(action, "cancel") == 0) {
        int child = fork();
        if (child < 0) return 65;
        if (child == 0) {
            delay(0, 700000000);
            text("child-lifecycle", "unexpected child completion");
            _exit(0);
        }
        if (text("ready", "ready") != 0) return 66;
        delay(10, 0);
        return 42;
    }
    emit(2, "unknown controlled LLVM action\n", 31);
    return 67;
}
