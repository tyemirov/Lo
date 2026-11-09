#include @READY_PROTOCOL@
#include <bsm/libbsm.h>
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <libproc.h>
#include <signal.h>
#include <stdint.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

#define READY_EVIDENCE @READY_EVIDENCE@
extern int lo_cli_main(int, const char *const *, const char *const *);
static int launcher_mode, ready_seen;
static ssize_t (*original_write)(int, const void *, size_t);

int lo_test_host_init(int argc, char **argv, char **environment) {
    (void)environment;
    alarm(8);
    original_write = dlsym(RTLD_NEXT, "write");
    if (!original_write) return 1;
    launcher_mode = argc == 2 && !strcmp(argv[1], "--lo-process-launcher");
    return 0;
}

static void write_exact(int descriptor, const unsigned char *bytes, size_t length) {
    while (length) {
        ssize_t count = original_write(descriptor, bytes, length);
        if (count < 0 && errno == EINTR) continue;
        if (count <= 0) _exit(81);
        bytes += count;
        length -= (size_t)count;
    }
}

ssize_t write(int descriptor, const void *bytes, size_t length) {
    if (!original_write) {
        original_write = dlsym(RTLD_NEXT, "write");
        if (!original_write) _exit(82);
    }
    if (!launcher_mode || descriptor != STATUS || length != PROCESS_READY_BYTES
        || ((const unsigned char *)bytes)[0] != 'R')
        return original_write(descriptor, bytes, length);
    if (ready_seen++) _exit(83);
    unsigned char changed[PROCESS_READY_BYTES];
    memcpy(changed, bytes, sizeof(changed));
    for (size_t index = 1; index != 6; ++index) if (changed[index]) _exit(84);
    const int words[] = {-1, 7, 5, 0, 1, 2, 3, 4, 6};
    if (LO_CASE < 0 || LO_CASE >= (int)(sizeof(words) / sizeof(words[0]))) _exit(85);
    if (words[LO_CASE] >= 0) changed[6 + 4 * words[LO_CASE] + 3] ^= 1;

    /* Evidence is inside the granted fixture root. No process behavior is
     * replaced: the real launcher still waits for the real transport permit. */
    int evidence = open(READY_EVIDENCE, O_WRONLY | O_APPEND | O_CLOEXEC);
    if (evidence < 0) _exit(86);
    write_exact(evidence, bytes, length);
    write_exact(evidence, changed, sizeof(changed));
    if (close(evidence)) _exit(87);
    write_exact(descriptor, changed, sizeof(changed));
    return (ssize_t)length;
}

static uint32_t word(const unsigned char *bytes) {
    return ((uint32_t)bytes[0] << 24) | ((uint32_t)bytes[1] << 16)
        | ((uint32_t)bytes[2] << 8) | bytes[3];
}

int main(int argc, char **argv, char **environment) {
    int status = lo_cli_main(argc, (const char *const *)argv, (const char *const *)environment);
    if (launcher_mode) return status;
    errno = 0;
    pid_t child = waitpid(-1, NULL, WNOHANG);
    if (child < 0 && errno == ECHILD) return status;

    /* An unreaped child fails the test. This separate teardown uses the actual
     * pre-mutation token and never counts as product cleanup evidence. */
    if (child == 0) {
        unsigned char ready[PROCESS_READY_BYTES];
        int evidence = open(READY_EVIDENCE, O_RDONLY | O_CLOEXEC);
        if (evidence >= 0) {
            ssize_t count = read(evidence, ready, sizeof(ready));
            close(evidence);
            if (count == (ssize_t)sizeof(ready)) {
                audit_token_t token;
                for (size_t index = 0; index != 8; ++index) token.val[index] = word(ready + 6 + 4 * index);
                (void)proc_signal_with_audittoken(&token, SIGKILL);
                (void)waitpid((pid_t)token.val[5], NULL, 0);
            }
        }
    }
    return 88;
}
