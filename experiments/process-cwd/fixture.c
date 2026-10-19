#include <errno.h>
#include <sandbox.h>
#include <stdbool.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

#include "fixture_paths.h"

#define MARKER(phase) do { \
    static const char marker[] = "LO_CWD_PHASE " phase "\n"; \
    if (write(STDERR_FILENO, marker, sizeof(marker) - 1) != sizeof(marker) - 1) _exit(90); \
} while (0)

struct observation { bool attempted, present, exact; int error; };

static struct observation observe_cwd(void) {
    char buffer[4096];
    errno = 0;
    char *result = getcwd(buffer, sizeof(buffer));
    int failure = result == NULL ? errno : 0;
    return (struct observation){.attempted=true, .present=result != NULL, .exact=result != NULL && strcmp(buffer, LO_ROOT) == 0, .error=failure};
}

int main(void) {
    MARKER("entered");
    MARKER("before-baseline");
    struct observation baseline = observe_cwd();
    MARKER("after-baseline");
    char *sandbox_error = NULL;
    bool installed = false;
    struct observation confined = {0};
    MARKER("before-sandbox-init");
    if (sandbox_init(LO_PROFILE, 0, &sandbox_error) == 0) {
        installed = true;
        MARKER("after-sandbox-init");
        MARKER("before-confined");
        confined = observe_cwd();
        MARKER("after-confined");
    } else MARKER("sandbox-init-failed");
    MARKER("before-report");
    char report[768];
    int count = snprintf(report, sizeof(report),
        "{\"sandbox_installed\":%s,\"baseline\":{\"attempted\":%s,\"present\":%s,\"exact\":%s,\"errno\":%d},\"confined\":{\"attempted\":%s,\"present\":%s,\"exact\":%s,\"errno\":%d}}\n",
        installed?"true":"false", baseline.attempted?"true":"false", baseline.present?"true":"false", baseline.exact?"true":"false", baseline.error,
        confined.attempted?"true":"false", confined.present?"true":"false", confined.exact?"true":"false", confined.error);
    if (count < 0 || (size_t)count >= sizeof(report) || write(STDOUT_FILENO, report, (size_t)count) != count) _exit(91);
    MARKER("after-report");
    _exit(baseline.present && baseline.exact && installed && confined.attempted ? 0 : 1);
}
