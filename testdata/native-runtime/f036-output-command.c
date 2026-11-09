#define main f036_base_command_main
#include "process-ownership-command.c"
#undef main

int main(int argc, char **argv) {
    if (argc != 6 || strcmp(argv[2], "output-child")) return f036_base_command_main(argc, argv);
    alarm(10);
    if (fixture_report(argv[1], FIXTURE_ENTER, NULL, argv[0])) return 80;
    audit_token_t parent;
    if (fixture_token(&parent)) return 81;
    pid_t child = fork();
    if (child < 0) return 82;
    if (!child) {
        alarm(8);
        if (fixture_report(argv[1], FIXTURE_FORK, &parent, NULL)) _exit(83);
        if (fixture_touch(argv[3], "ready")) _exit(84);
        if (fixture_io(1, "owned-output", 12, 1)) _exit(85);
        for (;;) pause();
    }
    return fixture_wait_path(argv[3]);
}
