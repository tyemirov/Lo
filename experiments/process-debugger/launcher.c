#include <signal.h>

#define main lo_preexec_main
#include "../process-preexec/launcher.c"
#undef main

int main(int argc, char **argv) {
    static const char ready[] = "LO_DEBUG_READY\n";
    if (write(STDERR_FILENO, ready, sizeof(ready) - 1) != sizeof(ready) - 1) _exit(94);
    if (raise(SIGSTOP) != 0) _exit(95);
    return lo_preexec_main(argc, argv);
}
