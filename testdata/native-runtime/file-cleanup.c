typedef __SIZE_TYPE__ Size;
typedef __INT64_TYPE__ Offset;
extern int open(const char *, int, ...);
extern int fcntl(int, int, ...);
extern int dup2(int, int);
extern Offset lseek(int, Offset, int);
extern long read(int, void *, Size);
extern long write(int, const void *, Size);
extern int mkdir(const char *, unsigned);
extern char *getcwd(char *, Size);
extern void *dlsym(void *, const char *);
#ifndef __APPLE__
extern long readlink(const char *, char *, Size);
#endif
extern int lo_cli_main(int, const char **, const char **);
#ifdef __APPLE__
extern int *__error(void);
#define ERROR_ADDRESS __error
#define CREATE_FLAGS (2 | 0x200 | 0x400)
#else
extern int *__errno_location(void);
#define ERROR_ADDRESS __errno_location
#define CREATE_FLAGS (2 | 0x40 | 0x200)
#endif

static char cleanup_root[4096];
static int close_failures;
static int equal(const char *left, const char *right) {
    while (*left && *left == *right) { ++left; ++right; }
    return *left == *right;
}
static int cleanup_descriptor(int descriptor) {
    if (!cleanup_root[0] || close_failures) return 0;
    char path[4096];
#ifdef __APPLE__
    if (fcntl(descriptor, 50, path)) return 0;
#else
    char link[64] = "/proc/self/fd/", digits[16];
    unsigned value = (unsigned)descriptor;
    Size length = 14, count = 0;
    do { digits[count++] = (char)('0' + value % 10); value /= 10; } while (value);
    while (count) link[length++] = digits[--count];
    link[length] = 0;
    long size = readlink(link, path, sizeof(path) - 1);
    if (size < 0) return 0;
    path[size] = 0;
#endif
    return equal(path, cleanup_root);
}
// Only the retained read root reports a close failure.
int close(int descriptor) {
    int inject = cleanup_descriptor(descriptor);
    int (*real_close)(int) = (int (*)(int))dlsym((void *)-1, "close");
    if (!real_close) { *ERROR_ADDRESS() = 5; return -1; }
    int result = real_close(descriptor);
    if (inject && result == 0) {
        ++close_failures;
        *ERROR_ADDRESS() = 5;
        return -1;
    }
    return result;
}
static int contains(const char *text, const char *needle) {
    for (; *text; ++text) {
        Size index = 0;
        while (needle[index] && text[index] == needle[index]) ++index;
        if (!needle[index]) return 1;
    }
    return 0;
}
int main(void) {
    if (mkdir("cleanup-root", 0777) || !getcwd(cleanup_root, sizeof(cleanup_root))) return 13;
    Size length = 0;
    while (cleanup_root[length]) ++length;
    if (length && cleanup_root[length - 1] == '/') --length;
    static const char suffix[] = "/cleanup-root";
    if (length + sizeof(suffix) > sizeof(cleanup_root)) return 14;
    for (Size index = 0; index < sizeof(suffix); ++index) cleanup_root[length + index] = suffix[index];
    int output = open("cleanup-diagnostic.json", CREATE_FLAGS, 0666);
    int saved = fcntl(2, 0, 3);
    if (output < 0 || saved < 0 || dup2(output, 2) < 0) return 10;
    const char *arguments[] = {"fixture", "run", "--format", "json", "--read-root", "cleanup-root", "--max-steps", LO_CASE == 2 ? "1" : "1000"};
    int status = lo_cli_main(8, arguments, 0);
    if (dup2(saved, 2) < 0 || lseek(output, 0, 0) < 0) return 11;
    char diagnostic[8192];
    long count = read(output, diagnostic, sizeof(diagnostic) - 1);
    if (count <= 0) return 12;
    diagnostic[count] = 0;
    if (close_failures != 1) { write(2, diagnostic, (Size)count); return 26; }
    if (status != (LO_CASE == 0 ? 1 : 2)) return 20;
    if (!contains(diagnostic, "close runtime resources")) {
        write(2, diagnostic, (Size)count);
        return 21;
    }
    if (LO_CASE != 1 && !contains(diagnostic, "; cleanup: close runtime resources")) return 25;
    if (LO_CASE == 0 && (!contains(diagnostic, "\"kind\":\"Math.DivideByZero\"") ||
                        !contains(diagnostic, "\"cleanup_errors\":["))) return 22;
    if (LO_CASE == 2 && (!contains(diagnostic, "\"code\":\"runtime.limit\"") ||
                        !contains(diagnostic, "\"cleanup_errors\":["))) return 23;
    if (LO_CASE == 1 && !contains(diagnostic, "\"code\":\"runtime.host-failure\"")) return 24;
    return 0;
}
