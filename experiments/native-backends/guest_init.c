extern int mount(const char *, const char *, const char *, unsigned long, const void *);
extern int open(const char *, int, ...);
extern int dup2(int, int);
extern int fork(void);
extern int execl(const char *, const char *, ...);
extern int waitpid(int, int *, int);
extern int printf(const char *, ...);
extern int fflush(void *);
extern int reboot(int);
extern void _exit(int);

int main(void) {
    if (mount("devtmpfs", "/dev", "devtmpfs", 0, 0) != 0) return 70;
    int console = open("/dev/console", 2);
    if (console < 0) return 71;
    for (int fd = 0; fd < 3; fd++) if (dup2(console, fd) < 0) return 72;
    printf("BEGIN_LO_NATIVE_PROBE\n");
    fflush(0);
    int child = fork();
    if (child < 0) return 73;
    if (child == 0) { execl("/probe", "probe", (char *)0); _exit(74); }
    int status = 0;
    if (waitpid(child, &status, 0) != child) return 75;
    int code = (status & 127) == 0 ? (status >> 8) & 255 : 128 + (status & 127);
    printf("END_LO_NATIVE_PROBE:%d\n", code);
    fflush(0);
    reboot(0x4321fedc);
    return 76;
}
