/* Run the declared qualification cases as PID 1. */
typedef struct stream FILE;
extern int mount(const char *, const char *, const char *, unsigned long, const void *);
extern int open(const char *, int, ...);
extern int dup2(int, int);
extern int chdir(const char *);
extern int fork(void);
extern int execve(const char *, char *const [], char *const []);
extern int waitpid(int, int *, int);
extern int printf(const char *, ...);
extern int fprintf(FILE *, const char *, ...);
extern int snprintf(char *, unsigned long, const char *, ...);
extern int fflush(FILE *);
extern FILE *fopen(const char *, const char *);
extern int fclose(FILE *);
extern int fileno(FILE *);
extern unsigned long fread(void *, unsigned long, unsigned long, FILE *);
extern void rewind(FILE *);
extern int fscanf(FILE *, const char *, ...);
extern int reboot(int);
extern void _exit(int);
extern int kill(int, int);
extern int setpgid(int, int);
extern int usleep(unsigned int);
extern int *__errno_location(void);
extern int socket(int, int, int);
extern int ioctl(int, unsigned long, ...);
extern int close(int);
extern int close_range(unsigned int, unsigned int, int);
extern long lseek(int, long, int);
extern long pread(int, void *, unsigned long, long);
struct timespec { long seconds; long nanoseconds; };
extern int clock_gettime(int, struct timespec *);
static char arguments[1048576], variables[1048576], cwd[4096], output_paths[131072];
static char *argv[4096], *environment[4096], *outputs[34];
static char path[256];
static unsigned long case_number;
static FILE *export_channel;

static long long now(void) {
    struct timespec time;
    if (clock_gettime(1, &time) != 0) return -1;
    return (long long)time.seconds * 1000000000LL + time.nanoseconds;
}

static int fail(const char *reason) {
    printf("qualification guest failed: %s\n", reason);
    if (export_channel && (fprintf(export_channel, "LO_QUALIFICATION_ERROR:%s\n", reason) < 0 ||
                           fflush(export_channel) != 0))
        printf("qualification export channel write failed\n");
    if (fflush(0) != 0) { reboot(0x4321fedc); return 90; }
    reboot(0x4321fedc);
    return 90;
}

static FILE *file(const char *name, const char *access) {
    snprintf(path, sizeof(path), "/cases/%lu/%s", case_number, name);
    return fopen(path, access);
}

static int list(const char *name, char *data, unsigned long capacity, char **items,
                unsigned long max_items, int allow_empty) {
    FILE *input = file(name, "rb");
    if (!input) return -1;
    unsigned long length = fread(data, 1, capacity, input);
    fclose(input);
    if (length == 0 || length == capacity || data[length - 1] != 0) return -1;
    if (allow_empty && length == 1) { items[0] = 0; return 0; }
    unsigned long count = 0;
    items[count++] = data;
    for (unsigned long index = 0; index + 1 < length; index++) {
        if (data[index] == 0) {
            if (count + 1 >= max_items) return -1;
            items[count++] = data + index + 1;
        }
    }
    items[count] = 0;
    return (int)count;
}

static int emit(const char *name, FILE *stream) {
    static const char digits[] = "0123456789abcdef";
    unsigned char bytes[4096];
    char hex[8193];
    if (!stream) return -1;
    rewind(stream);
    unsigned long count, total = 0;
    while ((count = fread(bytes, 1, sizeof(bytes), stream)) != 0) {
        total += count;
        if (total > 67108864) return -1;
        for (unsigned long index = 0; index < count; index++) {
            hex[index * 2] = digits[bytes[index] >> 4];
            hex[index * 2 + 1] = digits[bytes[index] & 15];
        }
        hex[count * 2] = 0;
        if (fprintf(export_channel, "LO_QUALIFICATION_DATA:%lu:%s:%s\n", case_number, name, hex) < 0) return -1;
    }
    if (fprintf(export_channel, "LO_QUALIFICATION_STREAM:%lu:%s:%lu\n", case_number, name, total) < 0) return -1;
    return 0;
}

static int progress(FILE *output, long long elapsed) {
    static const char digits[] = "0123456789abcdef";
    unsigned char bytes[512];
    char hex[1025];
    long length = lseek(fileno(output), 0, 1);
    if (length < 0) return -1;
    long offset = length > 512 ? length - 512 : 0;
    long count = pread(fileno(output), bytes, sizeof(bytes), offset);
    if (count < 0) return -1;
    for (long index = 0; index < count; index++) {
        hex[index * 2] = digits[bytes[index] >> 4];
        hex[index * 2 + 1] = digits[bytes[index] & 15];
    }
    hex[count * 2] = 0;
    if (fprintf(export_channel, "LO_QUALIFICATION_PROGRESS:%lu:%lld:%ld:%s\n", case_number, elapsed, length, hex) < 0) return -1;
    return fflush(export_channel);
}

static int loopback(void) {
    int descriptor = socket(2, 2, 0);
    if (descriptor < 0) return -1;
    struct { char name[16]; short flags; char padding[22]; } request = {{'l', 'o', 0}, 0x49, {0}};
    int result = ioctl(descriptor, 0x8914, &request);
    close(descriptor);
    return result;
}

static int stop_case(void) {
    /* As PID 1, all remaining guest processes belong to this case. */
    while (1) {
        if (kill(-1, 9) != 0 && *__errno_location() != 3) return -1;
        int waited = waitpid(-1, 0, 1);
        if (waited > 0) continue;
        if (waited < 0) {
            int error = *__errno_location();
            if (error == 10) return 0;
            if (error != 4) return -1;
        }
        /* Repeat the signal for descendants that fork before their parent stops. */
        if (usleep(10000) != 0 && *__errno_location() != 4) return -1;
    }
}

int main(void) {
    if (mount("devtmpfs", "/dev", "devtmpfs", 0, 0) != 0 ||
        mount("proc", "/proc", "proc", 0, 0) != 0) return fail("mount");
    int console = open("/dev/console", 2);
    if (console < 0) return fail("console");
    for (int descriptor = 0; descriptor < 3; descriptor++)
        if (dup2(console, descriptor) < 0) return fail("stdio");
    export_channel = fopen("/dev/vport0p1", "wb");
    if (!export_channel) return fail("export-channel-missing");
    if (loopback() != 0) return fail("loopback");
    FILE *count_file = fopen("/case-count", "rb");
    unsigned long cases = 0;
    if (!count_file || fscanf(count_file, "%lu", &cases) != 1 || cases == 0 || cases > 4096)
        return fail("case-count");
    fclose(count_file);
    for (case_number = 0; case_number < cases; case_number++) {
        if (list("arguments", arguments, sizeof(arguments), argv, 4096, 0) <= 0 ||
            list("environment", variables, sizeof(variables), environment, 4096, 0) <= 0)
            return fail("case-arguments");
        FILE *directory = file("cwd", "rb");
        unsigned long length = directory ? fread(cwd, 1, sizeof(cwd), directory) : 0;
        if (directory) fclose(directory);
        if (length == 0 || length == sizeof(cwd) || cwd[length - 1] != 0) return fail("case-cwd");
        FILE *timeout_file = file("timeout", "rb");
        unsigned long timeout = 0;
        if (!timeout_file || fscanf(timeout_file, "%lu", &timeout) != 1 || timeout == 0 || timeout > 7200)
            return fail("case-timeout");
        fclose(timeout_file);
        int output_count = list("outputs", output_paths, sizeof(output_paths), outputs, 34, 1);
        if (output_count < 0) return fail("case-output-paths");
        FILE *input = file("stdin", "rb"), *output = file("stdout", "w+b"), *errors = file("stderr", "w+b");
        if (!input || !output || !errors) return fail("case-streams");
        if (fflush(export_channel) != 0) return fail("export-channel-flush");
        long long start = now();
        if (start < 0) return fail("monotonic-clock");
        int child = fork();
        if (child < 0) return fail("case-fork");
        if (child == 0) {
            if (setpgid(0, 0) != 0 || chdir(cwd) != 0 || dup2(fileno(input), 0) < 0 ||
                dup2(fileno(output), 1) < 0 || dup2(fileno(errors), 2) < 0 || close_range(3, 0xffffffffU, 0) != 0) _exit(77);
            execve(argv[0], argv, environment);
            _exit(78);
        }
        setpgid(child, child);
        int status = 0, timed_out = 0;
        long long observed = start;
        while (1) {
            int waited = waitpid(child, &status, 1);
            if (waited == child) break;
            if (waited < 0) return fail("case-wait");
            long long current = now();
            if (current - observed >= 5000000000LL) {
                if (progress(output, current - start) != 0) return fail("export-channel-progress");
                observed = current;
            }
            if (current - start >= (long long)timeout * 1000000000LL) {
                timed_out = 1;
                break;
            }
            usleep(10000);
        }
        if (stop_case() != 0) return fail("case-cleanup");
        long long elapsed = now() - start;
        int code = timed_out ? 124 : ((status & 127) == 0 ? (status >> 8) & 255 : 128 + (status & 127));
        if (emit("stdout", output) != 0 || emit("stderr", errors) != 0) return fail("case-stream-limit");
        fclose(input); fclose(output); fclose(errors);
        for (int index = 0; index < output_count; index++) {
            FILE *exported = fopen(outputs[index], "rb");
            if (fprintf(export_channel, "LO_QUALIFICATION_OUTPUT:%lu:%d:%d\n", case_number, index, exported != 0) < 0)
                return fail("export-channel-output");
            if (exported) {
                char name[32];
                snprintf(name, sizeof(name), "file%d", index);
                if (emit(name, exported) != 0) return fail("case-output-limit");
                fclose(exported);
            }
        }
        if (fprintf(export_channel, "LO_QUALIFICATION_RESULT:%lu:%d:%lld\n", case_number, code, elapsed) < 0)
            return fail("export-channel-result");
        if (fflush(export_channel) != 0) return fail("export-channel-flush");
    }
    if (fprintf(export_channel, "LO_QUALIFICATION_COMPLETE:%lu\n", cases) < 0 || fflush(export_channel) != 0)
        return fail("export-channel-completion");
    if (fclose(export_channel) != 0) { export_channel = 0; return fail("export-channel-close"); }
    export_channel = 0;
    reboot(0x4321fedc);
    return 91;
}
