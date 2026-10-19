#include "ipc.h"
#include <string.h>
#include "fixture_paths.h"

int main(int argc, char **argv, char **environment) {
    if (argc != 2 || strcmp(argv[0], LO_TARGET) != 0 || environment[0] == NULL || environment[1] == NULL || environment[2] != NULL || strcmp(environment[0], "CI=1") != 0 || strcmp(environment[1], "TMPDIR=" LO_ROOT) != 0) return 70;
    int variant = strcmp(argv[1], "normalized-control") == 0 ? 1 : strcmp(argv[1], "normalized-confined") == 0 ? 2 : -1;
    return variant < 0 ? 71 : lo_ipc_inventory("post-exec", variant, MACH_PORT_NULL, 0, 1);
}
