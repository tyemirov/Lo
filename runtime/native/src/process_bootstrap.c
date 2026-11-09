#include "lo_process_supervisor.h"
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <errno.h>
#include <fcntl.h>
#include <libproc.h>
#include <mach/mach.h>
#include <signal.h>
#include <spawn.h>
#include <stdlib.h>
#include <string.h>
#include <sys/proc_info.h>
#include <sys/resource.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

int lo_process_self_token(LoProcessExecutionToken *result) {
    if (!result) return EINVAL;
    audit_token_t token;
    mach_msg_type_number_t count = TASK_AUDIT_TOKEN_COUNT;
    if (task_info(mach_task_self(), TASK_AUDIT_TOKEN, (task_info_t)&token, &count) != KERN_SUCCESS
        || count != TASK_AUDIT_TOKEN_COUNT) return EIO;
    _Static_assert(sizeof(result->word) == sizeof(token.val), "audit token size");
    memcpy(result->word, token.val, sizeof(result->word));
    return 0;
}
static int lo_process_code_identity(SecCodeRef code, LoProcessHostIdentity *identity) {
    if (SecCodeCheckValidity(code, kSecCSStrictValidate, NULL) != errSecSuccess) return EPERM;
    SecStaticCodeRef static_code = NULL;
    if (SecCodeCopyStaticCode(code, kSecCSDefaultFlags, &static_code) != errSecSuccess) return EIO;
    CFDictionaryRef information = NULL;
    OSStatus status = SecCodeCopySigningInformation(static_code, kSecCSSigningInformation, &information);
    CFRelease(static_code);
    if (status != errSecSuccess || !information) return EIO;
    CFTypeRef value = CFDictionaryGetValue(information, kSecCodeInfoUnique);
    int result = EINVAL;
    if (value && CFGetTypeID(value) == CFDataGetTypeID()) {
        CFIndex length = CFDataGetLength((CFDataRef)value);
        if (length > 0 && length <= LO_PROCESS_CODE_ID_MAX) {
            identity->code_size = (uint32_t)length;
            memset(identity->code, 0, sizeof(identity->code));
            memcpy(identity->code, CFDataGetBytePtr((CFDataRef)value), (size_t)length);
            result = 0;
        }
    }
    CFRelease(information);
    return result;
}
int lo_process_self_identity(LoProcessHostIdentity *identity) {
    if (!identity) return EINVAL;
    memset(identity, 0, sizeof(*identity));
    int error = lo_process_self_token(&identity->token);
    if (error) return error;
    SecCodeRef self = NULL;
    if (SecCodeCopySelf(kSecCSDefaultFlags, &self) != errSecSuccess || !self) return EIO;
    error = lo_process_code_identity(self, identity);
    CFRelease(self);
    return error;
}
static int lo_process_valid_identity(const LoProcessHostIdentity *identity) {
    if (!identity || identity->code_size == 0 || identity->code_size > LO_PROCESS_CODE_ID_MAX
        || identity->token.word[5] == 0 || identity->token.word[5] > INT32_MAX) return 0;
    for (size_t index = identity->code_size; index < sizeof(identity->code); ++index)
        if (identity->code[index]) return 0;
    return 1;
}
int lo_process_verify_identity(const LoProcessHostIdentity *expected,
    const LoProcessHostIdentity *claimed, int32_t expected_pid) {
    if (!lo_process_valid_identity(expected) || !lo_process_valid_identity(claimed)
        || expected_pid <= 0 || claimed->token.word[5] != (uint32_t)expected_pid
        || expected->code_size != claimed->code_size
        || memcmp(expected->code, claimed->code, expected->code_size)) return EINVAL;
    audit_token_t token;
    memcpy(token.val, claimed->token.word, sizeof(token.val));
    CFDataRef data = CFDataCreate(kCFAllocatorDefault, (const UInt8 *)&token, sizeof(token));
    if (!data) return ENOMEM;
    const void *keys[] = {kSecGuestAttributeAudit};
    const void *values[] = {data};
    CFDictionaryRef attributes = CFDictionaryCreate(kCFAllocatorDefault, keys, values, 1,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFRelease(data);
    if (!attributes) return ENOMEM;
    SecCodeRef guest = NULL;
    OSStatus status = SecCodeCopyGuestWithAttributes(NULL, attributes, kSecCSDefaultFlags, &guest);
    CFRelease(attributes);
    if (status != errSecSuccess || !guest) return EPERM;
    LoProcessHostIdentity actual = {0};
    int error = lo_process_code_identity(guest, &actual);
    CFRelease(guest);
    if (error) return error;
    return actual.code_size == expected->code_size
        && !memcmp(actual.code, expected->code, expected->code_size) ? 0 : EPERM;
}
int lo_process_verify_peer_channel(int descriptor, const LoProcessHostIdentity *claimed, int32_t expected_pid) {
    if (!lo_process_valid_identity(claimed)) return EINVAL;
    audit_token_t peer;
    socklen_t size = sizeof(peer);
    if (getsockopt(descriptor, SOL_LOCAL, LOCAL_PEERTOKEN, &peer, &size) || size != sizeof(peer)) return EIO;
    /* Check after the peer's handshake. Darwin resolves the socket's last
     * owner to its current audit token; this is not a creation-time token. */
    if (memcmp(peer.val, claimed->token.word, sizeof(peer.val))) return EPERM;
    LoProcessHostIdentity self;
    int error = lo_process_self_identity(&self);
    if (error) return error;
    return lo_process_verify_identity(&self, claimed, expected_pid);
}
int lo_process_kernel_limit(uint32_t requested, const LoProcessExecutionToken *expected) {
    if (!requested || !expected) return EINVAL;
    LoProcessExecutionToken actual;
    int error = lo_process_self_token(&actual);
    if (error) return error;
    if (memcmp(actual.word, expected->word, sizeof(actual.word))) return EPERM;
    struct proc_bsdinfo process;
    if (proc_pidinfo(getpid(), PROC_PIDTBSDINFO, 0, &process, sizeof(process)) != sizeof(process)) return EIO;
    if (process.pbi_uid == 0 || process.pbi_uid != process.pbi_ruid || process.pbi_uid != process.pbi_svuid
        || process.pbi_uid != actual.word[1] || process.pbi_ruid != actual.word[3]) return EPERM;
    struct rlimit inherited;
    if (getrlimit(RLIMIT_NPROC, &inherited)) return errno;
    if (inherited.rlim_max != RLIM_INFINITY && inherited.rlim_max < (rlim_t)requested) return ENOTSUP;
    struct rlimit selected = {(rlim_t)requested, (rlim_t)requested};
    if (setrlimit(RLIMIT_NPROC, &selected)) return errno;
    if (getrlimit(RLIMIT_NPROC, &inherited)) return errno;
    return inherited.rlim_cur == selected.rlim_cur && inherited.rlim_max == selected.rlim_max ? 0 : EIO;
}
static void lo_bootstrap_close(int descriptor, uint32_t *complete) {
    if (descriptor >= 0 && close(descriptor)) *complete = 0;
}
int lo_process_supervisor_spawn(const char *image, int control, int32_t *result_pid, uint32_t *complete) {
    if (!image || image[0] != '/' || !result_pid || !complete || control < 0) return EINVAL;
    *result_pid = 0; *complete = 1;
    int control_copy = fcntl(control, F_DUPFD_CLOEXEC, 16);
    if (control_copy < 0) return errno;
    int null_file = open("/dev/null", O_RDWR | O_NOFOLLOW | O_CLOEXEC);
    if (null_file < 0) { int error = errno; lo_bootstrap_close(control_copy, complete); return error; }
    int null_copy = fcntl(null_file, F_DUPFD_CLOEXEC, 16);
    int saved = errno;
    lo_bootstrap_close(null_file, complete);
    if (null_copy < 0 || !*complete) {
        lo_bootstrap_close(control_copy, complete); lo_bootstrap_close(null_copy, complete);
        return null_copy < 0 ? saved : EIO;
    }
    posix_spawn_file_actions_t actions;
    posix_spawnattr_t attributes;
    int error = posix_spawn_file_actions_init(&actions);
    if (error) goto descriptors;
    error = posix_spawnattr_init(&attributes);
    if (error) {
        if (posix_spawn_file_actions_destroy(&actions)) *complete = 0;
        goto descriptors;
    }
    for (int descriptor = 0; descriptor != 3 && !error; ++descriptor)
        error = posix_spawn_file_actions_adddup2(&actions, null_copy, descriptor);
    if (!error) error = posix_spawn_file_actions_adddup2(&actions, control_copy, 3);
    sigset_t defaults, blocked;
    sigfillset(&defaults); sigemptyset(&blocked);
    if (!error) error = posix_spawnattr_setsigdefault(&attributes, &defaults);
    if (!error) error = posix_spawnattr_setsigmask(&attributes, &blocked);
    if (!error) error = posix_spawnattr_setflags(&attributes,
        POSIX_SPAWN_CLOEXEC_DEFAULT | POSIX_SPAWN_SETSIGDEF | POSIX_SPAWN_SETSIGMASK | POSIX_SPAWN_SETSID);
    pid_t pid = 0;
    char *arguments[] = {(char *)image, "--lo-process-supervisor", NULL};
    char *environment[] = {"CI=1", NULL};
    if (!error) error = posix_spawn(&pid, image, &actions, &attributes, arguments, environment);
    if (pid > 0) *result_pid = (int32_t)pid;
    if (posix_spawnattr_destroy(&attributes)) *complete = 0;
    if (posix_spawn_file_actions_destroy(&actions)) *complete = 0;
descriptors:
    lo_bootstrap_close(control_copy, complete); lo_bootstrap_close(null_copy, complete);
    return error ? error : *complete ? 0 : EIO;
}
