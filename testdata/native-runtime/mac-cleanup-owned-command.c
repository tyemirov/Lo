#define main cleanup_command_main
#include "mac-cleanup-command.c"
#undef main
#include "process-ownership-fixture.h"
#include <sys/wait.h>

int main(int argc, char **argv) {
    const char *endpoint = getenv("FIXTURE_OWNER_SOCKET");
    if (!endpoint || fixture_report(endpoint, FIXTURE_ENTER, NULL, argv[0])) return 79;
    const char *tool = strrchr(argv[0], '/');
    tool = tool ? tool + 1 : argv[0];
    if (!strcmp(tool, "npm")) {
        audit_token_t actor;
        if (fixture_token(&actor)) return 79;
        pid_t child = fork();
        if (child < 0) return 79;
        if (!child) {
            if (fixture_report(endpoint, FIXTURE_FORK, &actor, NULL)) _exit(79);
            audit_token_t token;
            if (fixture_token(&token)) _exit(79);
            char text[65];
            fixture_token_text(&token, text);
            const char *marker = getenv("FIXTURE_OWNER_CHILD");
            int descriptor = marker ? open(marker, O_WRONLY) : -1;
            if (descriptor < 0 || write(descriptor, text, 64) != 64 || close(descriptor)) _exit(79);
            _exit(0);
        }
        int status;
        if (waitpid(child, &status, 0) != child || !WIFEXITED(status) || WEXITSTATUS(status)) return 79;
    }
    return cleanup_command_main(argc, argv);
}
