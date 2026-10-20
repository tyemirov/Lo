/* Software guest process protocol for CLI qualification. */
typedef struct stream FILE;
extern int mount(const char *, const char *, const char *, unsigned long, const void *);
extern int open(const char *, int, ...);
extern int dup2(int, int);
extern int fork(void);
extern int execv(const char *, char *const []);
extern int waitpid(int, int *, int);
extern int printf(const char *, ...);
extern int putchar(int);
extern int fflush(FILE *);
extern FILE *fopen(const char *, const char *);
extern int fclose(FILE *);
extern int fileno(FILE *);
extern unsigned long fread(void *, unsigned long, unsigned long, FILE *);
extern int fgetc(FILE *);
extern void rewind(FILE *);
extern int reboot(int);
extern void _exit(int);

static char arguments[1048576];
static char *argv[4096];

static void emit(FILE *file) {
    static const char hex[] = "0123456789abcdef";
    int value;
    rewind(file);
    while ((value = fgetc(file)) >= 0) {
        putchar(hex[(unsigned int)value >> 4]);
        putchar(hex[(unsigned int)value & 15]);
    }
}

int main(void) {
    if (mount("devtmpfs", "/dev", "devtmpfs", 0, 0) != 0) return 70;
    int console = open("/dev/console", 2);
    if (console < 0) return 71;
    for (int fd = 0; fd < 3; fd++) if (dup2(console, fd) < 0) return 72;
    FILE *argument_file = fopen("/arguments", "rb");
    FILE *input = fopen("/stdin", "rb");
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
    fflush(0);
    int child = fork();
    if (child < 0) return 76;
    if (child == 0) {
        if (dup2(fileno(input), 0) < 0 || dup2(fileno(output), 1) < 0 || dup2(fileno(errors), 2) < 0) _exit(77);
        execv("/probe", argv);
        _exit(78);
    }
    int status = 0;
    if (waitpid(child, &status, 0) != child) return 79;
    int code = (status & 127) == 0 ? (status >> 8) & 255 : 128 + (status & 127);
    printf("LO_NATIVE_RESULT:%d:", code);
    emit(output);
    putchar(':');
    emit(errors);
    putchar('\n');
    fflush(0);
    reboot(0x4321fedc);
    return 80;
}
