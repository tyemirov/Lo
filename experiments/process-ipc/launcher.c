#include "ipc.h"
#include <sandbox.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>
#include "fixture_paths.h"

int main(int argc, char **argv, char **environment) {
    if (argc != 2 || lo_ipc_variant(argv[1]) < 0 || environment[0] == NULL || environment[1] == NULL || environment[2] != NULL || strcmp(environment[0], "CI=1") != 0 || strcmp(environment[1], "TMPDIR=" LO_ROOT) != 0) return 64;
    int variant = lo_ipc_variant(argv[1]);
    if (lo_ipc_snapshot(argv[1], "pre-normalization", 0) != 0) return 65;
    int normalized = variant == 0 ? 0 : lo_normalize_inherited_rights();
    printf("{\"kind\":\"normalization\",\"variant\":\"%s\",\"attempted\":%s,\"complete\":%s}\n", argv[1], variant == 0 ? "false" : "true", normalized == 0 ? "true" : "false");
    if (fflush(stdout) != 0 || normalized != 0) return 66;
    if (lo_ipc_snapshot(argv[1], "pre-exec", 0) != 0) return 67;
    if (variant == 2) {
        char *error = NULL;
        if (sandbox_init(LO_PROFILE, 0, &error) != 0) {
            if (error != NULL) sandbox_free_error(error);
            return 68;
        }
    }
    char *arguments[] = {LO_TARGET, argv[1], NULL};
    char *selected_environment[] = {"CI=1", "TMPDIR=" LO_ROOT, NULL};
    execve(LO_TARGET, arguments, selected_environment);
    return 69;
}
