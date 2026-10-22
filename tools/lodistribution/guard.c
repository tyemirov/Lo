/* Verify inherited filesystem and network denial in the clean Darwin lane. */
typedef __SIZE_TYPE__ Size;
extern int open(const char *, int, ...);
extern int close(int);
extern long write(int, const void *, Size);
extern int *__error(void);
extern int fork(void);
extern int waitpid(int, int *, int);
extern void _exit(int);
extern int socket(int, int, int);
extern int connect(int, const void *, unsigned int);

static int denied(void) { return *__error() == 1 || *__error() == 13; }
static int examine(int argc, char **argv) {
    for (int i = 3; i < argc; i++) {
        int fd = open(argv[i], 0);
        if (fd >= 0) { close(fd); return 10 + i; }
        if (!denied()) return 30 + i;
    }
    int fd = open(argv[1], 1);
    if (fd >= 0) { close(fd); return 51; }
    if (!denied()) return 52;
    int connection = socket(2, 1, 0);
    if (connection < 0) { if (!denied()) return 53; }
    else {
        struct { unsigned char size, family; unsigned short port; unsigned int address; unsigned char zero[8]; } address = {16, 2, 0x0900, 0x0100007f, {0}};
        int result = connect(connection, &address, 16);
        int rejected = result < 0 && denied();
        close(connection);
        if (!rejected) return 54;
    }
    fd = open(argv[2], 1);
    if (fd < 0 || write(fd, "ok", 2) != 2) return 55;
    return close(fd) == 0 ? 0 : 56;
}
int main(int argc, char **argv) {
    if (argc < 4) return 2;
    int child = fork();
    if (child < 0) return 3;
    if (child == 0) _exit(examine(argc, argv));
    int status = 0;
    if (waitpid(child, &status, 0) != child || status != 0) return 4;
    int result = examine(argc, argv);
    if (result) return result;
    static const char message[] = "filesystem, network, and descendant restrictions verified\n";
    return write(1, message, sizeof(message)-1) == sizeof(message)-1 ? 0 : 5;
}
