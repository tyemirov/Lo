#include <fcntl.h>
#include <stdlib.h>
#include <unistd.h>

// Force the mapped executable vnode's cached name to its hardlink alias.
__attribute__((constructor))
static void observe_entry_alias(void) {
    const char *path = getenv("LO_TEST_ENTRY_ALIAS");
    if (!path) _exit(97);
    int file = open(path, O_RDONLY);
    if (file < 0 || close(file)) _exit(98);
}
