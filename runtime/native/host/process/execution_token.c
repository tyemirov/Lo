#include "protocol.h"
#include <dlfcn.h>
#include <errno.h>
#include <libproc.h>
#include <mach/mach.h>
#include <string.h>

int lo_process_execution_token(uint32_t words[8]) {
    if (!words) return EINVAL;
    audit_token_t token;
    mach_msg_type_number_t count = TASK_AUDIT_TOKEN_COUNT;
    if (task_info(mach_task_self(), TASK_AUDIT_TOKEN, (task_info_t)&token, &count) != KERN_SUCCESS
        || count != TASK_AUDIT_TOKEN_COUNT) return EIO;
    _Static_assert(sizeof(token.val) == 8 * sizeof(uint32_t), "audit token size");
    memcpy(words, token.val, sizeof(token.val));
    return 0;
}
int lo_process_execution_matches(int32_t pid, const uint32_t words[8]) {
    if (!words || pid <= 0 || words[5] != (uint32_t)pid) return EINVAL;
    uint32_t parent[8];
    int error = lo_process_execution_token(parent);
    if (error) return error;
    for (size_t index = 0; index != 8; ++index) {
        if (index != 5 && index != 7 && parent[index] != words[index]) return EPERM;
    }
    __typeof__(&proc_pidpath_audittoken) inspect_token =
        (__typeof__(&proc_pidpath_audittoken))dlsym(RTLD_DEFAULT, "proc_pidpath_audittoken");
    if (!inspect_token) return ENOTSUP;
    audit_token_t token;
    memcpy(token.val, words, sizeof(token.val));
    char path[PROC_PIDPATHINFO_MAXSIZE];
    // This API binds the inspection to PID plus execution version. Credentials
    // above must match the trusted parent before the launcher receives permit.
    // The returned pathname grants no additional filesystem authority.
    errno = 0;
    int length = inspect_token(&token, path, sizeof(path));
    return length > 0 ? 0 : errno ? errno : EIO;
}
