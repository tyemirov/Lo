typedef __SIZE_TYPE__ Size;
typedef __UINTPTR_TYPE__ Thread;
typedef __INT64_TYPE__ Offset;
extern int open(const char *, int, ...);
extern int close(int);
extern int dup2(int, int);
extern int fcntl(int, int, ...);
extern Offset lseek(int, Offset, int);
extern long read(int, void *, Size);
extern long write(int, const void *, Size);
extern int mkfifo(const char *, unsigned);
extern unsigned alarm(unsigned);
extern void (*signal(int, void (*)(int)))(int);
extern int pthread_create(Thread *, const void *, void *(*)(void *), void *);
extern int pthread_join(Thread, void **);
extern int lo_cli_main(int, const char **, const char **);
extern void *dlsym(void *, const char *);
extern int poll(void *, unsigned, int);
extern int getpid(void);
extern int kill(int, int);
#ifdef __APPLE__
extern int *__error(void);
#define ERROR_ADDRESS __error
#else
extern int *__errno_location(void);
#define ERROR_ADDRESS __errno_location
#endif
#ifdef __APPLE__
#define CREATE_FLAGS (2 | 0x200 | 0x400)
#else
#define CREATE_FLAGS (2 | 0x40 | 0x200)
#endif
static char payload[1048576];
static int armed, disconnected, payload_descriptor = -1, injected, signal_sent;
static long first_written;
static long (*actual_write)(int, const void *, Size);
long write(int descriptor, const void *bytes, Size count) {
    if (!actual_write) actual_write = dlsym((void *)-1, "write");
    if (!actual_write) return -1;
    if (__atomic_load_n(&armed, __ATOMIC_ACQUIRE) && count == sizeof(payload)
        && payload_descriptor < 0) {
        payload_descriptor = descriptor;
        long result = actual_write(descriptor, bytes, count);
        if (result > 0) {
            first_written = result;
            while (!__atomic_load_n(&disconnected, __ATOMIC_ACQUIRE)) poll(0, 0, 1);
        }
        return result;
    }
    if (descriptor == payload_descriptor && !injected
        && __atomic_load_n(&disconnected, __ATOMIC_ACQUIRE)) {
        ++injected;
        /* The host FIFO can repeat EAGAIN after its final reader closes.
           Inject this exact disconnected write after a real transfer and close.
           SIGPIPE must still be ignored by the native CLI. */
        signal_sent = kill(getpid(), 13) == 0;
        *ERROR_ADDRESS() = 32;
        return -1;
    }
    return actual_write(descriptor, bytes, count);
}
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
static int descriptors(void) {
    int count = 0;
    for (int fd = 0; fd < 256; ++fd) if (fcntl(fd, 1) >= 0) ++count;
    return count;
}
int main(void) {
    int output = open("output.json", CREATE_FLAGS, 0666);
    int error = open("error.json", CREATE_FLAGS, 0666);
    if (output < 0 || error < 0 || dup2(output, 1) < 0 || dup2(error, 2) < 0) return 10;
    int input = open("input", CREATE_FLAGS, 0666);
    if (input < 0 || write(input, payload, sizeof(payload)) != sizeof(payload) || close(input)) return 11;
    int before = descriptors();
    if (mkfifo("pipe", 0666)) return 12;
    int peer = open("pipe", 2);
    if (peer < 0) return 13;
    signal(13, 0);
    alarm(10);
    __atomic_store_n(&armed, 1, __ATOMIC_RELEASE);
    Thread thread;
    if (pthread_create(&thread, 0, execute, 0)) return 14;
    char first;
    if (read(peer, &first, 1) != 1 || close(peer)) return 15;
    __atomic_store_n(&disconnected, 1, __ATOMIC_RELEASE);
    void *result = 0;
    if (pthread_join(thread, &result)) return 16;
    alarm(0);
    if ((__INTPTR_TYPE__)result != (LO_CASE == 0 ? 1 : 0)) return 20;
    if (descriptors() != before) return 21;
    if (injected != 1 || !signal_sent || first_written <= 0 || first_written >= sizeof(payload)) return 28;
    int selected = LO_CASE == 0 ? error : output;
    int empty = LO_CASE == 0 ? output : error;
    if (lseek(empty, 0, 2) != 0 || lseek(selected, 0, 0) < 0) return 22;
    char diagnostic[8192];
    long count = read(selected, diagnostic, sizeof(diagnostic) - 1);
    if (count <= 0) return 23;
    diagnostic[count] = 0;
    if (!contains(diagnostic, "File.WriteFailed")) return 24;
    if (LO_CASE == 0 && !contains(diagnostic, "runtime.unhandled-error")) return 25;
    if (LO_CASE == 1 && !contains(diagnostic, "\"status\":\"ok\"")) return 26;
    return close(output) || close(error) ? 27 : 0;
}
