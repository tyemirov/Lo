#include <errno.h>
#include <stdlib.h>
#include <unistd.h>

int main(int argc, char **argv) {
    (void)argc;
    const char *worker = getenv("LO_CLEANUP_WORKER");
    if (!worker) return EINVAL;
    execv(worker, argv);
    return errno;
}
