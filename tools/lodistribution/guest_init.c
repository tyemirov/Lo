/* PID 1 for isolated installed-compiler qualification. */
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
extern int ferror(FILE *);
extern int fseek(FILE *, long, int);
extern int fflush(FILE *);
extern FILE *fopen(const char *, const char *);
extern int fclose(FILE *);
extern int fileno(FILE *);
extern unsigned long fread(void *, unsigned long, unsigned long, FILE *);
extern int reboot(int);
extern void _exit(int);
typedef struct directory DIR;
struct directory_entry {
    unsigned long inode;
    long offset;
    unsigned short length;
    unsigned char kind;
    char name[256];
};
extern DIR *opendir(const char *);
extern struct directory_entry *readdir(DIR *);
extern int closedir(DIR *);
static char arguments[1048576];
static char *argv[4096];
static char *environment[] = {"CI=1", "PATH=", "HOME=/home", "TMPDIR=/tmp", "TMP=/tmp", "LO_BUILD_ROOT=/tmp",
    "TEMP=/tmp", "XDG_CACHE_HOME=/cache", "LC_ALL=C", "TZ=UTC", "GOMAXPROCS=2", 0};

static int entries(FILE *export, const char *path) {
    DIR *directory = opendir(path);
    if (!directory) return 0;
    struct directory_entry *entry;
    while ((entry = readdir(directory)) != 0) {
        if (entry->name[0] == '.' && (entry->name[1] == 0 ||
            (entry->name[1] == '.' && entry->name[2] == 0))) continue;
        if (fprintf(export, "LO_DISTRIBUTION_ENTRY:%s/%s\n", path, entry->name) < 0) {
            closedir(directory);
            return -1;
        }
    }
    return closedir(directory);
}

static int emit(FILE *export, const char *name, FILE *file, unsigned long *size) {
    static const char digits[] = "0123456789abcdef";
    unsigned char bytes[4096];
    char hex[8193];
    *size = 0;
    if (!file) return 0;
    if (fseek(file, 0, 0) != 0) return -1;
    unsigned long count, total = 0;
    while ((count = fread(bytes, 1, sizeof(bytes), file)) != 0) {
        total += count;
        for (unsigned long i = 0; i < count; i++) {
            hex[i * 2] = digits[bytes[i] >> 4];
            hex[i * 2 + 1] = digits[bytes[i] & 15];
        }
        hex[count * 2] = 0;
        if (fprintf(export, "LO_DISTRIBUTION_DATA:%s:%s\n", name, hex) < 0) return -1;
    }
    if (ferror(file)) return -1;
    *size = total;
    return 0;
}

int main(void) {
    if (mount("devtmpfs", "/dev", "devtmpfs", 0, 0) != 0 ||
        mount("proc", "/proc", "proc", 0, 0) != 0) return 70;
    int console = open("/dev/console", 2);
    if (console < 0) return 71;
    for (int fd = 0; fd < 3; fd++) if (dup2(console, fd) < 0) return 72;
    FILE *export = fopen("/dev/vport0p1", "wbe");
    if (!export) {
        printf("distribution guest export transport unavailable: /dev/vport0p1\n");
        fflush(0);
        reboot(0x4321fedc);
        return 81;
    }
    FILE *argument_file = fopen("/arguments", "rb");
    FILE *input = fopen("/dev/null", "rb");
    FILE *output = fopen("/stdout", "w+b");
    FILE *errors = fopen("/stderr", "w+b");
    if (!argument_file || !input || !output || !errors) return 73;
    unsigned long length = fread(arguments, 1, sizeof(arguments), argument_file);
    fclose(argument_file);
    if (length == 0 || length == sizeof(arguments) || arguments[length - 1] != 0) return 74;
    unsigned long count = 0;
    argv[count++] = arguments;
    for (unsigned long i = 0; i + 1 < length; i++) {
        if (arguments[i] == 0) {
            if (count + 1 >= sizeof(argv) / sizeof(argv[0])) return 75;
            argv[count++] = arguments + i + 1;
        }
    }
    argv[count] = 0;
    if (fflush(0) != 0) return 82;
    int child = fork();
    if (child < 0) return 76;
    if (child == 0) {
        if (chdir("/unrelated") != 0 || dup2(fileno(input), 0) < 0 ||
            dup2(fileno(output), 1) < 0 || dup2(fileno(errors), 2) < 0) _exit(77);
        execve(argv[0], argv, environment);
        _exit(78);
    }
    int status = 0;
    if (waitpid(child, &status, 0) != child) return 79;
    int code = (status & 127) == 0 ? (status >> 8) & 255 : 128 + (status & 127);
    unsigned long ignored, artifact_bytes;
    FILE *artifact = fopen("/result/program", "rb");
    if (emit(export, "stdout", output, &ignored) != 0 ||
        emit(export, "stderr", errors, &ignored) != 0 ||
        emit(export, "artifact", artifact, &artifact_bytes) != 0) {
        reboot(0x4321fedc);
        return 83;
    }
    if ((artifact && fclose(artifact) != 0) || fclose(output) != 0 || fclose(errors) != 0 ||
        fprintf(export, "LO_DISTRIBUTION_ARTIFACT:%lu\n", artifact_bytes) < 0 ||
        entries(export, "/result") != 0 || entries(export, "/tmp") != 0 || entries(export, "/tmp/work") != 0 ||
        fprintf(export, "LO_DISTRIBUTION_RESULT:%d\n", code) < 0 || fflush(export) != 0) {
        reboot(0x4321fedc);
        return 84;
    }
    if (fclose(export) != 0) {
        reboot(0x4321fedc);
        return 85;
    }
    reboot(0x4321fedc);
    return 80;
}
