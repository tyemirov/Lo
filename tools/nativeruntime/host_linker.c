typedef __SIZE_TYPE__ Size;
extern void *calloc(Size, Size);
extern int execvp(const char *, char *const *);
extern long write(int, const void *, Size);

int main(int argc, char **argv) {
    char **arguments = calloc((Size)argc + 4, sizeof(char *));
    if (!arguments) return 127;
    arguments[0] = LO_CLANG;
    arguments[1] = "--ld-path=" LO_LINKER;
    arguments[2] = "-isysroot";
    arguments[3] = LO_SYSROOT;
    for (int index = 1; index < argc; ++index) arguments[index + 3] = argv[index];
    execvp(LO_CLANG, arguments);
    const char message[] = "native host linker: exec failed\n";
    write(2, message, sizeof(message) - 1);
    return 127;
}
