#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <unistd.h>

#include "fixture_paths.h"

__attribute__((constructor)) static void observe_confinement(void) {
    static const char marker[] = "LO_PHASE constructor.entered\n";
    if (write(STDERR_FILENO, marker, sizeof(marker) - 1) != sizeof(marker) - 1) _exit(90);
    int descriptor = open(LO_DENIED_WRITE, O_WRONLY | O_NOFOLLOW);
    int error = descriptor < 0 ? errno : 0;
    int result = descriptor < 0 ? -1 : 0;
    if (descriptor >= 0 && close(descriptor) != 0) _exit(92);
    char report[128];
    int length = snprintf(report, sizeof(report), "LO_CONSTRUCTOR denied_write_result=%d errno=%d\n", result, error);
    if (length < 0 || (size_t)length >= sizeof(report) || write(STDERR_FILENO, report, (size_t)length) != length) _exit(91);
}
