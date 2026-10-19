#include <errno.h>
#include <fcntl.h>
#include <sandbox.h>
#include <stdbool.h>
#include <stdio.h>
#include <sys/mman.h>
#include <unistd.h>

#include "fixture_paths.h"

#define MARKER(phase) do { \
    static const char marker[] = "LO_MAP_PHASE " phase "\n"; \
    if (write(STDERR_FILENO, marker, sizeof(marker) - 1) != sizeof(marker) - 1) _exit(90); \
} while (0)

struct observation {
    bool attempted, map_attempted, unmap_attempted, close_attempted;
    int opened, open_error, mapped, map_error, unmapped, unmap_error, closed, close_error;
};

static struct observation map_fixture(int protection) {
    struct observation item = {.attempted=true};
    MARKER("before-open");
    errno = 0;
    int descriptor = open(LO_FIXTURE, O_RDONLY | O_NOFOLLOW);
    item.open_error = errno;
    item.opened = descriptor;
    MARKER("after-open");
    if (descriptor < 0) return item;
    item.map_attempted = true;
    MARKER("before-mmap");
    errno = 0;
    void *mapping = mmap(NULL, LO_FIXTURE_SIZE, protection, MAP_PRIVATE, descriptor, 0);
    item.map_error = errno;
    item.mapped = mapping == MAP_FAILED ? -1 : 0;
    MARKER("after-mmap");
    if (mapping != MAP_FAILED) {
        item.unmap_attempted = true;
        MARKER("before-munmap");
        errno = 0;
        item.unmapped = munmap(mapping, LO_FIXTURE_SIZE);
        item.unmap_error = errno;
        MARKER("after-munmap");
    }
    item.close_attempted = true;
    MARKER("before-close");
    errno = 0;
    item.closed = close(descriptor);
    item.close_error = errno;
    MARKER("after-close");
    return item;
}

static bool released(struct observation item) {
    return (!item.unmap_attempted || item.unmapped == 0) && (!item.close_attempted || item.closed == 0);
}
static int encode_observation(char *buffer, size_t size, struct observation item) {
    return snprintf(buffer, size,
        "{\"attempted\":%s,\"open\":[%d,%d],\"map_attempted\":%s,\"mmap\":[%d,%d],\"unmap_attempted\":%s,\"munmap\":[%d,%d],\"close_attempted\":%s,\"close\":[%d,%d]}",
        item.attempted?"true":"false",item.opened,item.open_error,item.map_attempted?"true":"false",item.mapped,item.map_error,
        item.unmap_attempted?"true":"false",item.unmapped,item.unmap_error,item.close_attempted?"true":"false",item.closed,item.close_error);
}

int main(void) {
    MARKER("entered");
    MARKER("before-baseline-read");
    struct observation baseline_read = map_fixture(PROT_READ);
    MARKER("after-baseline-read");
    MARKER("before-baseline-executable");
    struct observation baseline_executable = map_fixture(PROT_READ | PROT_EXEC);
    MARKER("after-baseline-executable");
    bool sandbox_attempted = false, installed = false;
    struct observation confined_read = {0}, confined_executable = {0};
    if (released(baseline_read) && released(baseline_executable)) {
        sandbox_attempted = true;
        char *sandbox_error = NULL;
        MARKER("before-sandbox-init");
        if (sandbox_init(LO_PROFILE, 0, &sandbox_error) == 0) {
            installed = true;
            MARKER("after-sandbox-init");
            MARKER("before-confined-read");
            confined_read = map_fixture(PROT_READ);
            MARKER("after-confined-read");
            MARKER("before-confined-executable");
            confined_executable = map_fixture(PROT_READ | PROT_EXEC);
            MARKER("after-confined-executable");
        } else MARKER("sandbox-init-failed");
    }
    char observations[4][512];
    struct observation items[] = {baseline_read,baseline_executable,confined_read,confined_executable};
    for (int index=0; index<4; index++) {
        int count = encode_observation(observations[index],sizeof(observations[index]),items[index]);
        if (count < 0 || (size_t)count >= sizeof(observations[index])) _exit(91);
    }
    MARKER("before-report");
    char report[2560];
    int count = snprintf(report,sizeof(report),"{\"sandbox_attempted\":%s,\"sandbox_installed\":%s,\"baseline_read\":%s,\"baseline_executable\":%s,\"confined_read\":%s,\"confined_executable\":%s}\n",sandbox_attempted?"true":"false",installed?"true":"false",observations[0],observations[1],observations[2],observations[3]);
    if (count < 0 || (size_t)count >= sizeof(report) || write(STDOUT_FILENO,report,(size_t)count) != count) _exit(92);
    MARKER("after-report");
    bool baseline = baseline_read.opened >= 0 && baseline_read.map_attempted && baseline_read.mapped == 0 && baseline_executable.opened >= 0 && baseline_executable.map_attempted && baseline_executable.mapped == 0;
    bool confined = confined_read.opened >= 0 && confined_read.map_attempted && confined_read.mapped == 0 && confined_executable.opened >= 0 && confined_executable.map_attempted && confined_executable.mapped == -1 && confined_executable.map_error == EPERM;
    bool cleanup = released(baseline_read) && released(baseline_executable) && released(confined_read) && released(confined_executable);
    _exit(baseline && installed && confined && cleanup ? 0 : 1);
}
