#include "ipc.h"
#include <string.h>
#include "fixture_paths.h"

int main(int argc, char **argv, char **environment) {
    if (argc != 2 || strcmp(argv[0], LO_TARGET) != 0 || lo_ipc_variant(argv[1]) < 0 || environment[0] == NULL || environment[1] == NULL || environment[2] != NULL || strcmp(environment[0], "CI=1") != 0 || strcmp(environment[1], "TMPDIR=" LO_ROOT) != 0) return 70;
    return lo_ipc_snapshot(argv[1], "post-exec", lo_ipc_variant(argv[1]) == 0);
}
