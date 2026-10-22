typedef struct stream FILE;
extern FILE *fopen(const char *, const char *);
extern int fclose(FILE *);
extern int fprintf(FILE *, const char *, ...);
extern int fscanf(FILE *, const char *, ...);
extern int printf(const char *, ...);
extern int fflush(FILE *);
extern int fork(void);
extern int getpid(void);
extern int setpgid(int, int);
extern int setsid(void);
extern int pipe(int *);
extern long read(int, void *, unsigned long);
extern long write(int, const void *, unsigned long);
extern int pause(void);
extern int kill(int, int);
extern int *__errno_location(void);

static int ready(const char *path, int descriptor) {
    FILE *output = fopen(path, "wb");
    if (!output || fprintf(output, "%d\n", getpid()) < 0 || fclose(output) != 0) return -1;
    return write(descriptor, "x", 1) == 1 ? 0 : -1;
}

static int absent(const char *path) {
    FILE *input = fopen(path, "rb");
    int pid;
    if (!input || fscanf(input, "%d", &pid) != 1 || fclose(input) != 0) return 0;
    return kill(pid, 0) == -1 && *__errno_location() == 3;
}

int main(int argc, char **argv) {
    if (argc != 2) return 70;
    if (argv[1][0] == 'i') {
        if (!absent("/result/parent.pid") || !absent("/result/leaf.pid")) return 71;
        printf("descendants absent\n");
        return 0;
    }
    int descriptors[2];
    if (pipe(descriptors) != 0) return 72;
    int child = fork();
    if (child < 0) return 73;
    if (child == 0) {
        if (setpgid(0, 0) != 0) return 74;
        int leaf = fork();
        if (leaf < 0) return 75;
        if (leaf == 0) {
            if (setsid() < 0 || ready("/result/leaf.pid", descriptors[1]) != 0) return 76;
        } else if (ready("/result/parent.pid", descriptors[1]) != 0) return 77;
        for (;;) pause();
    }
    for (int index = 0; index < 2; index++) {
        char byte;
        if (read(descriptors[0], &byte, 1) != 1) return 78;
    }
    printf("descendants ready\n");
    if (fflush(0) != 0) return 79;
    if (argv[1][0] == 'e') return 7;
    if (argv[1][0] == 's') kill(getpid(), 15);
    for (;;) pause();
}
