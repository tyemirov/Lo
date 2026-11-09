#include "lo_process_ownership.h"
#include <EndpointSecurity/EndpointSecurity.h>
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <libproc.h>
#include <mach/mach_time.h>
#include <pthread.h>
#include <sched.h>
#include <stdbool.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

/* Compile this boundary with the selected SDK. Runtime symbol admission is
 * explicit, so the no-descendant profile does not depend on macOS 27 symbols. */
#define LO_ES_APIS(X) \
    X(es_new_descendants_client) X(es_delete_client) X(es_subscribe) \
    X(es_respond_auth_result) \
    X(es_unmute_all_paths) X(es_unmute_all_target_paths) \
    X(es_muted_processes_events) X(es_unmute_process) X(es_release_muted_processes) \
    X(es_muting_inverted) X(es_set_deadline_miss_mode) X(es_get_deadline_miss_mode) \
    X(es_sync_client)
struct lo_es_api {
#define LO_ES_DECLARE(name) __typeof__(&name) name;
    LO_ES_APIS(LO_ES_DECLARE)
#undef LO_ES_DECLARE
    __typeof__(&proc_signal_with_audittoken) signal;
};
struct lo_es_pending { uint64_t request; const es_message_t *message; int replied, error; };
struct lo_es_owner {
    LoProcessOwnershipConfiguration configuration;
    LoProcessOwnershipEmit emit;
    void *destination, *library;
    struct lo_es_api api;
    es_client_t *client;
    pthread_mutex_t serial, operations, pending_lock;
    pthread_cond_t replied;
    struct lo_es_pending pending;
    mach_timebase_info_data_t timebase;
    uint64_t next_request;
    int active, damaged, close_failed;
};

size_t lo_process_es_storage(uint32_t tokens) {
    return tokens ? sizeof(struct lo_es_owner) : 0;
}
static LoProcessExecutionToken lo_es_token(audit_token_t source) {
    LoProcessExecutionToken token;
    _Static_assert(sizeof(token.word) == sizeof(source.val), "audit token size");
    memcpy(token.word, source.val, sizeof(token.word));
    return token;
}
static LoProcessFileIdentity lo_es_file(const es_file_t *file) {
    return (LoProcessFileIdentity){(uint64_t)file->stat.st_dev, (uint64_t)file->stat.st_ino};
}
/* Called under serial. Damage is sticky and does not consume the ES client. */
static void lo_es_failure(struct lo_es_owner *owner, int reason) {
    owner->damaged = 1;
    LoProcessOwnershipEvent event = {0};
    event.kind = LO_PROCESS_OWNER_FAILURE;
    event.generation = owner->configuration.generation;
    event.status = reason;
    owner->emit(owner->destination, &event);
}
static int lo_es_exec_file(LoProcessOwnershipEvent *event, const es_process_t *process) {
    const es_file_t *file = process->executable;
    if (file->path_truncated || file->path.length == 0 || file->path.length > LO_PROCESS_OWNER_PATH_MAX) return 0;
    event->file = lo_es_file(file);
    event->path = (const unsigned char *)file->path.data;
    event->path_size = file->path.length;
    return 1;
}
/* The serial callback keeps one message borrowed until this handoff ends.
 * The SDK deadline bounds this wait. Timeout and a late owner reply cannot
 * issue two ES responses because pending_lock protects the single transition. */
static void lo_es_wait_reply(struct lo_es_owner *owner, es_client_t *client, const es_message_t *message) {
    pthread_mutex_lock(&owner->pending_lock);
    int timed_out = 0;
    while (!owner->pending.replied) {
        uint64_t now = mach_absolute_time();
        if (now >= message->deadline) { timed_out = 1; break; }
        uint64_t ticks = message->deadline - now;
        uint64_t seconds = ticks / owner->timebase.denom;
        uint64_t remainder = ticks % owner->timebase.denom;
        if (seconds > UINT64_MAX / owner->timebase.numer) { timed_out = 1; break; }
        uint64_t nanoseconds = seconds * owner->timebase.numer;
        uint64_t fraction = remainder * owner->timebase.numer / owner->timebase.denom;
        if (nanoseconds > UINT64_MAX - fraction) { timed_out = 1; break; }
        nanoseconds += fraction;
        struct timespec interval = {(time_t)(nanoseconds / 1000000000), (long)(nanoseconds % 1000000000)};
        int result = pthread_cond_timedwait_relative_np(&owner->replied, &owner->pending_lock, &interval);
        if (result && result != ETIMEDOUT && result != EINTR) { timed_out = 1; break; }
    }
    int failed = owner->pending.error;
    if (timed_out && !owner->pending.replied) {
        owner->pending.replied = 1;
        owner->pending.error = EIO;
        (void)owner->api.es_respond_auth_result(client, message, ES_AUTH_RESULT_DENY, false);
        failed = 1;
    }
    owner->pending = (struct lo_es_pending){0};
    pthread_mutex_unlock(&owner->pending_lock);
    if (failed) lo_es_failure(owner, LO_PROCESS_OWNER_INVALID);
}
static void lo_es_message(struct lo_es_owner *owner, es_client_t *client, const es_message_t *message) {
    pthread_mutex_lock(&owner->serial);
    if (!owner->active) {
        lo_es_failure(owner, LO_PROCESS_OWNER_CLOSED);
        if (message && message->action_type == ES_ACTION_TYPE_AUTH
            && owner->api.es_respond_auth_result(client, message, ES_AUTH_RESULT_DENY, false) != ES_RESPOND_RESULT_SUCCESS)
            lo_es_failure(owner, LO_PROCESS_OWNER_INVALID);
        pthread_mutex_unlock(&owner->serial);
        return;
    }
    LoProcessOwnershipEvent event = {0};
    event.generation = owner->configuration.generation;
    if (message->version < 4) goto invalid;
    event.sequence = message->global_seq_num;
    event.actor = lo_es_token(message->process->audit_token);
    switch (message->event_type) {
    case ES_EVENT_TYPE_NOTIFY_FORK:
        if (message->action_type != ES_ACTION_TYPE_NOTIFY) goto invalid;
        event.kind = LO_PROCESS_OWNER_FORK;
        event.target = lo_es_token(message->event.fork.child->audit_token);
        break;
    case ES_EVENT_TYPE_AUTH_EXEC: {
        if (message->action_type != ES_ACTION_TYPE_AUTH) goto invalid;
        event.kind = LO_PROCESS_OWNER_AUTH_EXEC;
        event.target = lo_es_token(message->event.exec.target->audit_token);
        if (!lo_es_exec_file(&event, message->event.exec.target) || owner->next_request == UINT64_MAX) goto invalid;
        pthread_mutex_lock(&owner->pending_lock);
        if (owner->pending.message) {
            pthread_mutex_unlock(&owner->pending_lock);
            goto invalid;
        }
        event.request = ++owner->next_request;
        owner->pending = (struct lo_es_pending){event.request, message, 0, 0};
        pthread_mutex_unlock(&owner->pending_lock);
        break;
    }
    case ES_EVENT_TYPE_NOTIFY_EXEC:
        if (message->action_type != ES_ACTION_TYPE_NOTIFY) goto invalid;
        event.kind = LO_PROCESS_OWNER_EXEC;
        event.actor = lo_es_token(message->event.exec.target->audit_token);
        if (!lo_es_exec_file(&event, message->event.exec.target)) goto invalid;
        break;
    case ES_EVENT_TYPE_NOTIFY_EXIT:
        if (message->action_type != ES_ACTION_TYPE_NOTIFY) goto invalid;
        event.kind = LO_PROCESS_OWNER_EXIT;
        break;
    case ES_EVENT_TYPE_NOTIFY_OPEN:
        if (message->action_type != ES_ACTION_TYPE_NOTIFY) goto invalid;
        event.kind = LO_PROCESS_OWNER_OPEN;
        event.file = lo_es_file(message->event.open.file);
        if (message->event.open.fflag & FREAD) event.flags |= LO_PROCESS_OWNER_OPEN_READ;
        if (message->event.open.fflag & FWRITE) event.flags |= LO_PROCESS_OWNER_OPEN_WRITE;
        /* OPEN does not need a path: the reserved marker uses retained identity. */
        break;
    default:
        goto invalid;
    }
    if (!owner->emit(owner->destination, &event)) {
        owner->damaged = 1;
        if (event.kind == LO_PROCESS_OWNER_AUTH_EXEC) {
            pthread_mutex_lock(&owner->pending_lock);
            owner->pending.replied = 1;
            es_respond_result_t result = owner->api.es_respond_auth_result(client, message, ES_AUTH_RESULT_DENY, false);
            owner->pending.error = result == ES_RESPOND_RESULT_SUCCESS ? 0 : EIO;
            pthread_mutex_unlock(&owner->pending_lock);
        }
    }
    if (event.kind == LO_PROCESS_OWNER_AUTH_EXEC) lo_es_wait_reply(owner, client, message);
    pthread_mutex_unlock(&owner->serial);
    return;
invalid:
    lo_es_failure(owner, LO_PROCESS_OWNER_INVALID);
    if (message->action_type == ES_ACTION_TYPE_AUTH) {
        /* This message did not enter the retained-request table. */
        if (owner->api.es_respond_auth_result(client, message, ES_AUTH_RESULT_DENY, false) != ES_RESPOND_RESULT_SUCCESS)
            lo_es_failure(owner, LO_PROCESS_OWNER_INVALID);
    }
    pthread_mutex_unlock(&owner->serial);
}
static int lo_es_marker(uintptr_t handle, uint64_t request) {
    struct lo_es_owner *owner = (void *)handle;
    pthread_mutex_lock(&owner->operations);
    int descriptor = -1;
    if (owner->active && owner->client && request != 0)
        descriptor = open("/dev/null", O_RDONLY | O_NOFOLLOW | O_CLOEXEC);
    pthread_mutex_unlock(&owner->operations);
    return descriptor;
}
static int lo_es_synchronize(uintptr_t handle, uint64_t request) {
    struct lo_es_owner *owner = (void *)handle;
    pthread_mutex_lock(&owner->operations);
    if (!owner->active || !owner->client || request == 0) {
        pthread_mutex_unlock(&owner->operations);
        return EINVAL;
    }
    es_return_t result = owner->api.es_sync_client(owner->client, ^{
        pthread_mutex_lock(&owner->serial);
        if (!owner->active || !owner->client) {
            lo_es_failure(owner, LO_PROCESS_OWNER_CLOSED);
        } else {
            LoProcessOwnershipEvent event = {0};
            event.kind = LO_PROCESS_OWNER_SYNC;
            event.generation = owner->configuration.generation;
            event.request = request;
            if (!owner->emit(owner->destination, &event)) owner->damaged = 1;
        }
        pthread_mutex_unlock(&owner->serial);
    });
    pthread_mutex_unlock(&owner->operations);
    return result == ES_RETURN_SUCCESS ? 0 : EIO;
}
static int lo_es_respond(uintptr_t handle, uint64_t request, uint32_t decision) {
    struct lo_es_owner *owner = (void *)handle;
    if (request == 0 || decision > LO_PROCESS_OWNER_ALLOW) return EINVAL;
    pthread_mutex_lock(&owner->operations);
    pthread_mutex_lock(&owner->pending_lock);
    if (!owner->active || !owner->client || !owner->pending.message
        || owner->pending.request != request || owner->pending.replied) {
        pthread_mutex_unlock(&owner->pending_lock);
        pthread_mutex_unlock(&owner->operations);
        return EINVAL;
    }
    const es_message_t *message = owner->pending.message;
    owner->pending.replied = 1;
    es_respond_result_t result = owner->api.es_respond_auth_result(owner->client, message,
        decision == LO_PROCESS_OWNER_ALLOW ? ES_AUTH_RESULT_ALLOW : ES_AUTH_RESULT_DENY, false);
    owner->pending.error = result == ES_RESPOND_RESULT_SUCCESS ? 0 : EIO;
    pthread_cond_signal(&owner->replied);
    pthread_mutex_unlock(&owner->pending_lock);
    pthread_mutex_unlock(&owner->operations);
    return result == ES_RESPOND_RESULT_SUCCESS ? 0 : EIO;
}
static int lo_es_signal(uintptr_t handle, const LoProcessExecutionToken *token, int32_t number) {
    struct lo_es_owner *owner = (void *)handle;
    if (!token || token->word[5] == 0 || token->word[5] > INT32_MAX) return EINVAL;
    audit_token_t native;
    memcpy(native.val, token->word, sizeof(native.val));
    return owner->api.signal(&native, number);
}
static int lo_es_close(uintptr_t handle) {
    struct lo_es_owner *owner = (void *)handle;
    for (;;) {
        pthread_mutex_lock(&owner->operations);
        if (owner->close_failed) { pthread_mutex_unlock(&owner->operations); return EIO; }
        if (!pthread_mutex_trylock(&owner->serial)) break;
        pthread_mutex_unlock(&owner->operations);
        pthread_mutex_lock(&owner->pending_lock);
        int unanswered = owner->pending.message && !owner->pending.replied;
        pthread_mutex_unlock(&owner->pending_lock);
        // Never hold operations or wait behind an unanswered AUTH. The owner
        // can return to its controller and deny the pending request.
        if (unanswered) return EBUSY;
        // A completed synchronization callback can still be returning through
        // emit. Join that bounded callback without declaring a close failure.
        sched_yield();
    }
    pthread_mutex_lock(&owner->pending_lock);
    int pending = owner->pending.message != NULL;
    pthread_mutex_unlock(&owner->pending_lock);
    if (pending) {
        pthread_mutex_unlock(&owner->serial);
        pthread_mutex_unlock(&owner->operations);
        return EBUSY;
    }
    owner->active = 0;
    pthread_mutex_unlock(&owner->serial);
    /* No callback can invoke an ES operation after active becomes zero. */
    if (owner->client) {
        es_return_t result = owner->api.es_delete_client(owner->client);
        // The SDK invalidates this pointer after deletion, including a shutdown
        // error. Keep the wrapper and callback destination alive on failure.
        owner->client = NULL;
        if (result != ES_RETURN_SUCCESS) {
            owner->close_failed = 1;
            pthread_mutex_unlock(&owner->operations);
            return EIO;
        }
    }
    if (owner->library && dlclose(owner->library)) {
        owner->close_failed = 1;
        pthread_mutex_unlock(&owner->operations);
        return EIO;
    }
    owner->library = NULL;
    pthread_mutex_unlock(&owner->operations);
    pthread_mutex_destroy(&owner->pending_lock);
    pthread_cond_destroy(&owner->replied);
    pthread_mutex_destroy(&owner->serial);
    pthread_mutex_destroy(&owner->operations);
    free(owner);
    return 0;
}
static int lo_es_setup(struct lo_es_owner *owner) {
    es_deadline_miss_mode_t mode;
    if (owner->api.es_set_deadline_miss_mode(owner->client, ES_DEADLINE_MISS_MODE_FAIL_CLOSED) != ES_RETURN_SUCCESS
        || owner->api.es_get_deadline_miss_mode(owner->client, &mode) != ES_RETURN_SUCCESS
        || mode != ES_DEADLINE_MISS_MODE_FAIL_CLOSED
        || owner->api.es_unmute_all_paths(owner->client) != ES_RETURN_SUCCESS
        || owner->api.es_unmute_all_target_paths(owner->client) != ES_RETURN_SUCCESS) return 0;
    for (es_mute_inversion_type_t kind = ES_MUTE_INVERSION_TYPE_PROCESS; kind < ES_MUTE_INVERSION_TYPE_LAST; ++kind)
        if (owner->api.es_muting_inverted(owner->client, kind) != ES_MUTE_NOT_INVERTED) return 0;
    es_muted_processes_t *muted = NULL;
    if (owner->api.es_muted_processes_events(owner->client, &muted) != ES_RETURN_SUCCESS || !muted) return 0;
    int valid = muted->count <= (uint64_t)owner->configuration.kernel_processes + 1;
    if (valid) {
        for (size_t index = 0; index < muted->count; ++index) {
            if (owner->api.es_unmute_process(owner->client, &muted->processes[index].audit_token) != ES_RETURN_SUCCESS)
                valid = 0;
        }
    }
    owner->api.es_release_muted_processes(muted);
    if (!valid) return 0;
    const es_event_type_t events[] = {ES_EVENT_TYPE_NOTIFY_FORK, ES_EVENT_TYPE_AUTH_EXEC,
        ES_EVENT_TYPE_NOTIFY_EXEC, ES_EVENT_TYPE_NOTIFY_EXIT, ES_EVENT_TYPE_NOTIFY_OPEN};
    return owner->api.es_subscribe(owner->client, events, sizeof(events) / sizeof(events[0])) == ES_RETURN_SUCCESS;
}
uint32_t lo_process_es_open(const LoProcessOwnershipConfiguration *configuration,
    LoProcessOwnershipEmit emit, void *destination, LoProcessOwnershipClient *client) {
    if (!configuration || !emit || !destination || !client || configuration->generation == 0
        || !configuration->max_processes || !configuration->max_tokens
        || !configuration->max_events || !configuration->kernel_processes) return LO_PROCESS_OWNER_FAILED;
    *client = (LoProcessOwnershipClient){0};
    if (!lo_process_es_storage(configuration->max_tokens)) return LO_PROCESS_OWNER_FAILED;
    struct lo_es_owner *owner = calloc(1, sizeof(*owner));
    if (!owner) return LO_PROCESS_OWNER_FAILED;
    owner->configuration = *configuration;
    owner->emit = emit;
    owner->destination = destination;
    if (mach_timebase_info(&owner->timebase) != KERN_SUCCESS || !owner->timebase.numer || !owner->timebase.denom)
        goto allocation_failed;
    if (pthread_mutex_init(&owner->serial, NULL)) goto allocation_failed;
    if (pthread_mutex_init(&owner->operations, NULL)) { pthread_mutex_destroy(&owner->serial); goto allocation_failed; }
    if (pthread_mutex_init(&owner->pending_lock, NULL)) {
        pthread_mutex_destroy(&owner->operations); pthread_mutex_destroy(&owner->serial); goto allocation_failed;
    }
    if (pthread_cond_init(&owner->replied, NULL)) {
        pthread_mutex_destroy(&owner->pending_lock); pthread_mutex_destroy(&owner->operations);
        pthread_mutex_destroy(&owner->serial); goto allocation_failed;
    }
    owner->library = dlopen("/usr/lib/libEndpointSecurity.dylib", RTLD_NOW | RTLD_LOCAL);
    if (!owner->library) goto unavailable;
#define LO_ES_RESOLVE(name) owner->api.name = (__typeof__(owner->api.name))dlsym(owner->library, #name); \
    if (!owner->api.name) goto unavailable;
    LO_ES_APIS(LO_ES_RESOLVE)
#undef LO_ES_RESOLVE
    owner->api.signal = (__typeof__(owner->api.signal))dlsym(RTLD_DEFAULT, "proc_signal_with_audittoken");
    if (!owner->api.signal) goto unavailable;
    owner->active = 1;
    es_new_client_result_t result = owner->api.es_new_descendants_client(&owner->client,
        ^(es_client_t *created, const es_message_t *message) { lo_es_message(owner, created, message); });
    if (result != ES_NEW_CLIENT_RESULT_SUCCESS || !owner->client) {
        if (!owner->client) goto unavailable;
        goto setup_failed;
    }
    *client = (LoProcessOwnershipClient){(uintptr_t)owner, lo_es_marker, lo_es_synchronize,
        lo_es_respond, lo_es_signal, lo_es_close};
    if (!lo_es_setup(owner)) goto setup_failed;
    return LO_PROCESS_OWNER_READY;
setup_failed:
    *client = (LoProcessOwnershipClient){(uintptr_t)owner, lo_es_marker, lo_es_synchronize,
        lo_es_respond, lo_es_signal, lo_es_close};
    if (lo_es_close((uintptr_t)owner) == 0) {
        *client = (LoProcessOwnershipClient){0};
        return LO_PROCESS_OWNER_UNAVAILABLE;
    }
    return LO_PROCESS_OWNER_FAILED;
unavailable:
    /* No command exists. A live partial client is returned for owned cleanup. */
    if (owner->client) goto setup_failed;
    owner->active = 0;
    if (owner->library && dlclose(owner->library)) goto setup_failed;
    pthread_mutex_destroy(&owner->pending_lock);
    pthread_cond_destroy(&owner->replied);
    pthread_mutex_destroy(&owner->operations);
    pthread_mutex_destroy(&owner->serial);
    free(owner);
    return LO_PROCESS_OWNER_UNAVAILABLE;
allocation_failed:
    free(owner);
    return LO_PROCESS_OWNER_FAILED;
}
