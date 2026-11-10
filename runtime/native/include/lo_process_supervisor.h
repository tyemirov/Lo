#ifndef LO_PROCESS_SUPERVISOR_H
#define LO_PROCESS_SUPERVISOR_H
#include "lo_process_ownership.h"
#include "lo_process.h"

/* Private SDK boundary. No SDK structure layout crosses this interface. */
#define LO_PROCESS_CODE_ID_MAX 64U
typedef struct {
    LoProcessExecutionToken token;
    uint32_t code_size;
    unsigned char code[LO_PROCESS_CODE_ID_MAX];
} LoProcessHostIdentity;

int lo_process_self_token(LoProcessExecutionToken *);
int lo_process_self_identity(LoProcessHostIdentity *);
int lo_process_verify_identity(const LoProcessHostIdentity *expected_image,
    const LoProcessHostIdentity *claimed, int32_t expected_pid);
int lo_process_verify_peer_channel(int descriptor, const LoProcessHostIdentity *claimed, int32_t expected_pid);
int lo_process_kernel_limit(uint32_t requested, const LoProcessExecutionToken *expected);
int lo_process_supervisor_spawn(const char *image, int control, int32_t *pid, uint32_t *cleanup_complete);
size_t lo_process_es_storage(uint32_t tokens);

typedef struct {
    void *context;
    int (*launch)(void *);
    void (*created)(void *, int32_t pid);
    int (*ready)(void *, int32_t pid, const LoProcessExecutionToken *);
    void (*reaped)(void *);
    void (*stopping)(void *, int32_t stage, int32_t classification);
} LoProcessLifecycleHooks;
int lo_native_process_execute_descendants(const char *launcher, int embedded, const char *directory,
    const unsigned char *configuration, size_t configuration_size, const LoProcessRequest *,
    LoProcessResult *, const LoProcessLifecycleHooks *);
#endif
