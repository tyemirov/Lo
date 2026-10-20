typedef __SIZE_TYPE__ Size;
typedef __UINTPTR_TYPE__ Thread;
extern int open(const char *, int, ...);
extern int close(int);
extern int dup2(int, int);
extern long lseek(int, long, int);
extern long read(int, void *, Size);
extern long write(int, const void *, Size);
extern int mkfifo(const char *, unsigned);
extern unsigned alarm(unsigned);
extern int pthread_create(Thread *, const void *, void *(*)(void *), void *);
extern int pthread_join(Thread, void **);
extern int pthread_kill(Thread, int);
extern int lo_cli_main(int, const char **, const char **);
static char payload[1048576];
static void *execute(void *unused) {
    (void)unused;
    const char *arguments[] = {"fixture", "run", "--format", "json", "--read-root", ".", "--write-root", "."};
    return (void *)(__INTPTR_TYPE__)lo_cli_main(8, arguments, 0);
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
    int output = open("output.json", 2 | 0x40 | 0x200, 0666);
    int error = open("error.json", 2 | 0x40 | 0x200, 0666);
    if (output < 0 || error < 0 || dup2(output, 1) < 0 || dup2(error, 2) < 0) return 10;
    if (mkfifo("pipe", 0666) != 0) return 11;
    int peer = open("pipe", 2);
    if (peer < 0) return 12;
    for (Size index = 0; index < sizeof(payload); ++index) payload[index] = 'x';
    if (LO_CASE == 1) {
        int input = open("input", 1 | 0x40 | 0x200, 0666);
        if (input < 0 || write(input, payload, sizeof(payload)) != sizeof(payload) || close(input)) return 13;
    }
    Thread thread;
    alarm(10);
    if (pthread_create(&thread, 0, execute, 0)) return 14;
    if (LO_CASE == 0) {
        Size written = 0;
        while (written < sizeof(payload)) {
            long count = write(peer, payload + written, sizeof(payload) - written);
            if (count <= 0) return 15;
            written += count;
        }
    } else {
        char first;
        if (read(peer, &first, 1) != 1) return 16;
    }
    if (pthread_kill(thread, 2)) return 17;
    void *result = 0;
    if (pthread_join(thread, &result)) return 18;
    alarm(0);
    if ((__INTPTR_TYPE__)result != 2) return 20;
    if (lseek(output, 0, 2) != 0 || lseek(error, 0, 0) < 0) return 21;
    char diagnostic[8192];
    long count = read(error, diagnostic, sizeof(diagnostic) - 1);
    if (count <= 0) return 22;
    diagnostic[count] = 0;
    if (!contains(diagnostic, "\"code\":\"runtime.cancelled\"")) return 23;
    if (open("later", 0) >= 0) return 24;
    return close(peer) || close(output) || close(error) ? 25 : 0;
}
