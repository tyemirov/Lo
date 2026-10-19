#include "ipc.h"
#include <sandbox.h>
#include <string.h>
#include <unistd.h>
#include "fixture_paths.h"

int main(int argc, char **argv, char **environment) {
    if (argc != 2 || environment[0] == NULL || environment[1] == NULL || environment[2] != NULL || strcmp(environment[0], "CI=1") != 0 || strcmp(environment[1], "TMPDIR=" LO_ROOT) != 0) return 64;
    int variant;
    for (variant = 0; variant < 3; variant++) if (strcmp(argv[1], lo_ipc_variant_name(variant)) == 0) break;
    if (variant == 3 || lo_ipc_local_fixture(variant) != 0) return 65;
    if (variant == 0) return 0;
    if (variant == 2) {
        char *error = NULL;
        if (sandbox_init(LO_PROFILE, 0, &error) != 0) { if (error != NULL) sandbox_free_error(error); return 66; }
    }
    char *arguments[] = {LO_TARGET, argv[1], NULL};
    char *selected_environment[] = {"CI=1", "TMPDIR=" LO_ROOT, NULL};
    execve(LO_TARGET, arguments, selected_environment);
    return 67;
}
