#include "capture.h"

extern int rename(const char *, const char *), linkat(int, const char *, int, const char *, int);
extern int lo_cli_main(int, const char *const *, const char *const *);
extern unsigned long long lo_runtime_live_contexts(void), lo_runtime_live_values(void);

int main(int argc, char **argv) {
    if (argc != 1) return 31;
    char running[4096];
    LoCaptureSize length = 0;
    while (argv[0][length] && length + 16 < sizeof(running)) {
        running[length] = argv[0][length];
        ++length;
    }
    if (argv[0][length]) return 32;
    const char suffix[] = ".running";
    for (LoCaptureSize index = 0; index < sizeof(suffix); ++index) {
        running[length + index] = suffix[index];
    }
    int capture = lo_capture_start();
    if (capture) return capture;
    if (rename(argv[0], running)) return lo_capture_cleanup(33);
    if (linkat(-2, lo_capture_path, -2, argv[0], 0)) return lo_capture_cleanup(34);
    const char *arguments[] = {"untrusted-argv-zero", "run", "--lock-entry"};
    int status = lo_cli_main(3, arguments, 0);
    capture = lo_capture_finish();
    if (capture) return capture;
    if (status) return 35;
    LoCaptureSize index = 0;
    while (running[index] && index < lo_capture_length && running[index] == lo_capture_bytes[index]) ++index;
    if (running[index] || lo_capture_length != index + 1 || lo_capture_bytes[index] != '\n') return 36;
    if (lo_runtime_live_contexts() || lo_runtime_live_values()) return 37;
    return 0;
}
