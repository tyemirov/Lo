typedef __SIZE_TYPE__ Size;
typedef __INT64_TYPE__ Offset;
extern int open(const char *, int, ...);
extern int fcntl(int, int, ...);
extern int dup2(int, int);
extern Offset lseek(int, Offset, int);
extern long read(int, void *, Size);
extern int lo_cli_main(int, const char **);
#ifdef __APPLE__
extern int *__error(void);
#define ERROR_ADDRESS __error
#define CREATE_FLAGS (2 | 0x200 | 0x400)
#else
extern int *__errno_location(void);
#define ERROR_ADDRESS __errno_location
#define CREATE_FLAGS (2 | 0x40 | 0x200)
#endif

// The dependency deliberately fails every close. Process exit releases the
// isolated fixture descriptors; the real CLI must preserve the first error.
int close(int descriptor) {
    (void)descriptor;
    *ERROR_ADDRESS() = 5;
    return -1;
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
    int output = open("cleanup-diagnostic.json", CREATE_FLAGS, 0666);
    int saved = fcntl(2, 0, 3);
    if (output < 0 || saved < 0 || dup2(output, 2) < 0) return 10;
    const char *arguments[] = {"fixture", "run", "--format", "json", "--read-root", ".", "--max-steps", LO_CASE == 2 ? "1" : "1000"};
    int status = lo_cli_main(8, arguments);
    if (dup2(saved, 2) < 0 || lseek(output, 0, 0) < 0) return 11;
    char diagnostic[8192];
    long count = read(output, diagnostic, sizeof(diagnostic) - 1);
    if (count <= 0) return 12;
    diagnostic[count] = 0;
    if (status != (LO_CASE == 0 ? 1 : 2)) return 20;
    if (!contains(diagnostic, "close runtime resources")) return 21;
    if (LO_CASE == 0 && (!contains(diagnostic, "\"kind\":\"Math.DivideByZero\"") ||
                        !contains(diagnostic, "\"cleanup_errors\":["))) return 22;
    if (LO_CASE == 2 && (!contains(diagnostic, "\"code\":\"runtime.limit\"") ||
                        !contains(diagnostic, "\"cleanup_errors\":["))) return 23;
    if (LO_CASE == 1 && !contains(diagnostic, "\"code\":\"runtime.host-failure\"")) return 24;
    return 0;
}
