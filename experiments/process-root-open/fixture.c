#include <errno.h>
#include <fcntl.h>
#include <sandbox.h>
#include <stdbool.h>
#include <stdio.h>
#include <unistd.h>

#include "fixture_paths.h"

#define MARKER(phase) do { \
    static const char marker[] = "LO_ROOT_PHASE " phase "\n"; \
    if (write(STDERR_FILENO, marker, sizeof(marker) - 1) != sizeof(marker) - 1) _exit(90); \
} while (0)

struct observation {
    bool attempted, close_attempted;
    int opened, open_error, closed, close_error;
};

static struct observation root_handle(void) {
    struct observation item = {.attempted=true};
    MARKER("before-open");
    errno = 0;
    int descriptor = open("/", O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    item.open_error = errno;
    item.opened = descriptor;
    MARKER("after-open");
    if (descriptor >= 0) {
        item.close_attempted = true;
        MARKER("before-close");
        errno = 0;
        item.closed = close(descriptor);
        item.close_error = errno;
        MARKER("after-close");
    }
    return item;
}

int main(void) {
    MARKER("entered");
    MARKER("before-baseline");
    struct observation baseline = root_handle();
    MARKER("after-baseline");
    bool sandbox_attempted = false, installed = false;
    struct observation confined = {0};
    if (!baseline.close_attempted || baseline.closed == 0) {
        sandbox_attempted = true;
        char *sandbox_error = NULL;
        MARKER("before-sandbox-init");
        if (sandbox_init(LO_PROFILE, 0, &sandbox_error) == 0) {
            installed = true;
            MARKER("after-sandbox-init");
            MARKER("before-confined");
            confined = root_handle();
            MARKER("after-confined");
        } else MARKER("sandbox-init-failed");
    }
    MARKER("before-report");
    char report[1024];
    int count = snprintf(report, sizeof(report),
        "{\"sandbox_attempted\":%s,\"sandbox_installed\":%s,\"baseline\":{\"attempted\":%s,\"open\":[%d,%d],\"close_attempted\":%s,\"close\":[%d,%d]},\"confined\":{\"attempted\":%s,\"open\":[%d,%d],\"close_attempted\":%s,\"close\":[%d,%d]}}\n",
        sandbox_attempted?"true":"false", installed?"true":"false",
        baseline.attempted?"true":"false", baseline.opened, baseline.open_error,
        baseline.close_attempted?"true":"false", baseline.closed, baseline.close_error,
        confined.attempted?"true":"false", confined.opened, confined.open_error,
        confined.close_attempted?"true":"false", confined.closed, confined.close_error);
    if (count < 0 || (size_t)count >= sizeof(report) || write(STDOUT_FILENO, report, (size_t)count) != count) _exit(91);
    MARKER("after-report");
    bool baseline_complete = baseline.opened >= 0 && baseline.close_attempted && baseline.closed == 0;
    bool cleanup_complete = !confined.close_attempted || confined.closed == 0;
    _exit(baseline_complete && installed && confined.attempted && cleanup_complete ? 0 : 1);
}
