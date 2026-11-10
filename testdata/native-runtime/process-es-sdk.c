#include <EndpointSecurity/EndpointSecurity.h>
#include <Block.h>
#include <dlfcn.h>
#include <mach/mach_time.h>
#include <sched.h>
#include "lo_process_ownership.h"

static uint32_t sdk_native_open(uintptr_t, const LoProcessOwnershipConfiguration *,
    LoProcessOwnershipEmit, void *, LoProcessOwnershipClient *);

/* The controlled process fixture supplies SDK events. The production adapter
 * translates them and the production controller makes every ownership choice. */
@F036_EVENT_SOURCE@

enum {
    SDK_SUCCESS, SDK_SUBSCRIBE_FAILURE, SDK_DEADLINE_FAILURE, SDK_NEW_FAILURE,
    SDK_MISSING_API, SDK_MUTING_FAILURE, SDK_AUTH_DEADLINE, SDK_CLOSE_CALLBACK,
    SDK_DELETE_FAILURE, SDK_RESPONSE_FAILURE, SDK_SYNC_FAILURE,
    SDK_DEADLINE_READBACK, SDK_MUTED_FAILURE
};
#define SDK_CASE @F036_SDK_CASE@
struct sdk_client {
    es_handler_block_t callback;
    LoProcessOwnershipClient events;
    uint64_t auth_count;
    unsigned syncs;
};
struct sdk_delivery {
    es_process_t actor, target;
    es_file_t file;
    uint64_t provider_request, adapter_request;
    unsigned replies;
    int borrowed, deadline_probe;
    es_message_t message;
};
static struct sdk_client *sdk_current;
static LoProcessOwnershipConfiguration sdk_configuration;
static LoProcessOwnershipClient sdk_adapter;
static _Thread_local int sdk_deadline_callback;
static _Thread_local uint64_t sdk_message_deadline;
static int sdk_wait_entered, sdk_expire_deadline, sdk_expired_clock_seen, sdk_deadline_injected;
static int sdk_event(void *, const LoProcessOwnershipEvent *);
extern uint32_t lo_process_es_open(const LoProcessOwnershipConfiguration *,
    LoProcessOwnershipEmit, void *, LoProcessOwnershipClient *);

static void sdk_require(int condition, const char *message) {
    if (!condition) { fixture_log(message); _exit(110); }
}
static uint64_t sdk_deadline(unsigned milliseconds) {
    mach_timebase_info_data_t timebase;
    sdk_require(mach_timebase_info(&timebase) == KERN_SUCCESS, "sdk-timebase-invalid\n");
    return mach_absolute_time() + (uint64_t)milliseconds * 1000000 * timebase.denom / timebase.numer;
}
static uint64_t sdk_absolute_time(void) {
    if (sdk_deadline_callback) {
        __atomic_store_n(&sdk_wait_entered, 1, __ATOMIC_SEQ_CST);
        if (__atomic_load_n(&sdk_expire_deadline, __ATOMIC_SEQ_CST)) {
            __atomic_store_n(&sdk_expired_clock_seen, 1, __ATOMIC_SEQ_CST);
            return sdk_message_deadline;
        }
        /* A one-tick timed wait releases pending_lock. Hold the injected clock
         * below the deadline until the concurrent public close has returned. */
        return sdk_message_deadline - 1;
    }
    return mach_absolute_time();
}
static es_new_client_result_t sdk_new(es_client_t **client, es_handler_block_t callback) {
    fixture_log("sdk-new\n");
    if (SDK_CASE == SDK_NEW_FAILURE) return ES_NEW_CLIENT_RESULT_ERR_NOT_ENTITLED;
    struct sdk_client *created = calloc(1, sizeof(*created));
    sdk_require(created != NULL, "sdk-allocation-failed\n");
    created->callback = Block_copy(callback);
    sdk_current = created;
    sdk_require(fixture_open(36, &sdk_configuration, sdk_event, created, &created->events)
        == LO_PROCESS_OWNER_READY, "sdk-event-source-failed\n");
    *client = (es_client_t *)created;
    return ES_NEW_CLIENT_RESULT_SUCCESS;
}
static es_return_t sdk_subscribe(es_client_t *client, const es_event_type_t *events, uint32_t count) {
    (void)client;
    const es_event_type_t expected[] = {ES_EVENT_TYPE_NOTIFY_FORK, ES_EVENT_TYPE_AUTH_EXEC,
        ES_EVENT_TYPE_NOTIFY_EXEC, ES_EVENT_TYPE_NOTIFY_EXIT, ES_EVENT_TYPE_NOTIFY_OPEN};
    sdk_require(count == sizeof(expected) / sizeof(expected[0])
        && !memcmp(events, expected, sizeof(expected)), "sdk-subscriptions-differ\n");
    fixture_log("sdk-subscribe\n");
    return SDK_CASE == SDK_SUBSCRIBE_FAILURE ? ES_RETURN_ERROR : ES_RETURN_SUCCESS;
}
static es_return_t sdk_set_deadline(es_client_t *client, es_deadline_miss_mode_t selected) {
    (void)client;
    sdk_require(selected == ES_DEADLINE_MISS_MODE_FAIL_CLOSED, "sdk-deadline-not-fail-closed\n");
    fixture_log("sdk-set-deadline\n");
    return SDK_CASE == SDK_DEADLINE_FAILURE ? ES_RETURN_ERROR : ES_RETURN_SUCCESS;
}
static es_return_t sdk_get_deadline(es_client_t *client, es_deadline_miss_mode_t *selected) {
    (void)client;
    *selected = SDK_CASE == SDK_DEADLINE_READBACK ? (es_deadline_miss_mode_t)-1 : ES_DEADLINE_MISS_MODE_FAIL_CLOSED;
    return ES_RETURN_SUCCESS;
}
static es_return_t sdk_unmute_paths(es_client_t *client) {
    (void)client; fixture_log("sdk-unmute-paths\n"); return ES_RETURN_SUCCESS;
}
static es_return_t sdk_unmute_targets(es_client_t *client) {
    (void)client; fixture_log("sdk-unmute-targets\n"); return ES_RETURN_SUCCESS;
}
static es_mute_inverted_return_t sdk_inverted(es_client_t *client, es_mute_inversion_type_t kind) {
    (void)client; (void)kind;
    return SDK_CASE == SDK_MUTING_FAILURE ? ES_MUTE_INVERTED : ES_MUTE_NOT_INVERTED;
}
static es_return_t sdk_muted(es_client_t *client, es_muted_processes_t **muted) {
    (void)client;
    if (SDK_CASE == SDK_MUTED_FAILURE) return ES_RETURN_ERROR;
    *muted = calloc(1, sizeof(**muted));
    sdk_require(*muted != NULL, "sdk-muted-allocation-failed\n");
    return ES_RETURN_SUCCESS;
}
static es_return_t sdk_unmute_process(es_client_t *client, const audit_token_t *token) {
    (void)client; (void)token; return ES_RETURN_SUCCESS;
}
static void sdk_release_muted(es_muted_processes_t *muted) {
    free(muted); fixture_log("sdk-release-muted\n");
}
static es_respond_result_t sdk_respond(es_client_t *client, const es_message_t *message,
    es_auth_result_t decision, bool cache) {
    struct sdk_client *owner = (void *)client;
    struct sdk_delivery *delivery = (void *)((unsigned char *)message - offsetof(struct sdk_delivery, message));
    sdk_require(delivery->borrowed && !cache && delivery->replies++ == 0,
        "sdk-response-lifetime-or-count-invalid\n");
    sdk_require(decision == ES_AUTH_RESULT_ALLOW || decision == ES_AUTH_RESULT_DENY,
        "sdk-response-decision-invalid\n");
    fixture_log(decision == ES_AUTH_RESULT_ALLOW ? "sdk-allow-uncached\n" : "sdk-deny-uncached\n");
    if (delivery->deadline_probe) {
        sdk_require(decision == ES_AUTH_RESULT_DENY
            && __atomic_load_n(&sdk_expired_clock_seen, __ATOMIC_SEQ_CST),
            "sdk-auth-did-not-expire\n");
        fixture_log("sdk-deadline-denial\n");
    }
    if (delivery->provider_request) {
        uint32_t selected = decision == ES_AUTH_RESULT_ALLOW ? LO_PROCESS_OWNER_ALLOW : LO_PROCESS_OWNER_DENY;
        if (SDK_CASE == SDK_RESPONSE_FAILURE) selected = LO_PROCESS_OWNER_DENY;
        sdk_require(owner->events.respond(owner->events.handle, delivery->provider_request, selected) == 0,
            "sdk-event-response-failed\n");
    }
    return SDK_CASE == SDK_RESPONSE_FAILURE ? ES_RESPOND_RESULT_ERR_INTERNAL : ES_RESPOND_RESULT_SUCCESS;
}
static void sdk_deliver(struct sdk_client *owner, const LoProcessOwnershipEvent *event, int deadline_probe) {
    struct sdk_delivery delivery = {0};
    delivery.message.version = 4;
    delivery.message.global_seq_num = event->sequence;
    delivery.message.process = &delivery.actor;
    delivery.message.action_type = ES_ACTION_TYPE_NOTIFY;
    delivery.actor.audit_token = fixture_native_token(event->actor);
    delivery.target.audit_token = fixture_native_token(event->target);
    delivery.file.stat.st_dev = (dev_t)event->file.device;
    delivery.file.stat.st_ino = (ino_t)event->file.inode;
    delivery.file.path.data = (const char *)event->path;
    delivery.file.path.length = event->path_size;
    delivery.target.executable = &delivery.file;
    switch (event->kind) {
    case LO_PROCESS_OWNER_FORK:
        delivery.message.event_type = ES_EVENT_TYPE_NOTIFY_FORK;
        delivery.message.event.fork.child = &delivery.target;
        break;
    case LO_PROCESS_OWNER_AUTH_EXEC:
        delivery.message.event_type = ES_EVENT_TYPE_AUTH_EXEC;
        delivery.message.action_type = ES_ACTION_TYPE_AUTH;
        delivery.message.event.exec.target = &delivery.target;
        delivery.message.deadline = sdk_deadline(deadline_probe ? 50 : 1000);
        delivery.provider_request = event->request;
        delivery.adapter_request = ++owner->auth_count;
        break;
    case LO_PROCESS_OWNER_EXEC:
        delivery.message.event_type = ES_EVENT_TYPE_NOTIFY_EXEC;
        delivery.target.audit_token = delivery.actor.audit_token;
        delivery.message.event.exec.target = &delivery.target;
        break;
    case LO_PROCESS_OWNER_EXIT:
        delivery.message.event_type = ES_EVENT_TYPE_NOTIFY_EXIT;
        break;
    case LO_PROCESS_OWNER_OPEN:
        delivery.message.event_type = ES_EVENT_TYPE_NOTIFY_OPEN;
        delivery.message.event.open.file = &delivery.file;
        delivery.message.event.open.fflag = FREAD;
        break;
    default: sdk_require(0, "sdk-unexpected-event\n");
    }
    delivery.borrowed = 1;
    delivery.deadline_probe = deadline_probe;
    sdk_deadline_callback = deadline_probe;
    sdk_message_deadline = delivery.message.deadline;
    owner->callback((es_client_t *)owner, &delivery.message);
    sdk_deadline_callback = 0;
    delivery.borrowed = 0;
    if (event->kind == LO_PROCESS_OWNER_AUTH_EXEC) {
        sdk_require(delivery.replies == 1, "sdk-auth-returned-unanswered\n");
        /* The borrowed SDK message has ended. The public client must reject a
         * duplicate or late controller answer without another SDK response. */
        if (sdk_adapter.handle) {
            sdk_require(sdk_adapter.respond(sdk_adapter.handle, delivery.adapter_request,
                LO_PROCESS_OWNER_ALLOW) == EINVAL && delivery.replies == 1,
                "sdk-late-response-accepted\n");
            fixture_log("sdk-late-response-rejected\n");
        }
    }
}
static int sdk_event(void *destination, const LoProcessOwnershipEvent *event) {
    sdk_deliver(destination, event, 0);
    return 1;
}
static es_return_t sdk_sync(es_client_t *client, void (^completion)(void)) {
    struct sdk_client *owner = (void *)client;
    if (SDK_CASE == SDK_SYNC_FAILURE) return ES_RETURN_ERROR;
    int descriptor = owner->events.open_marker(owner->events.handle, ++owner->syncs);
    sdk_require(descriptor >= 0 && close(descriptor) == 0, "sdk-marker-failed\n");
    struct fixture_owner *events = (void *)owner->events.handle;
    pthread_mutex_lock(&events->serial);
    completion();
    pthread_mutex_unlock(&events->serial);
    fixture_log("sdk-sync\n");
    return ES_RETURN_SUCCESS;
}
struct sdk_probe { struct sdk_client *owner; LoProcessOwnershipEvent event; };
static void *sdk_probe_auth(void *argument) {
    struct sdk_probe *probe = argument;
    struct fixture_owner *events = (void *)probe->owner->events.handle;
    pthread_mutex_lock(&events->serial);
    probe->event.sequence = ++events->sequence;
    sdk_deliver(probe->owner, &probe->event, 1);
    pthread_mutex_unlock(&events->serial);
    return NULL;
}
static int sdk_signal(const audit_token_t *token, int number) {
    if (SDK_CASE == SDK_AUTH_DEADLINE && !sdk_deadline_injected++) {
        struct sdk_probe probe = {0}; probe.owner = sdk_current;
        probe.event.kind = LO_PROCESS_OWNER_AUTH_EXEC;
        probe.event.actor = fixture_owned_token(*token);
        probe.event.target = probe.event.actor; probe.event.target.word[7] ^= 0x40000000U;
        probe.event.path = (const unsigned char *)@F036_COMMAND@;
        probe.event.path_size = strlen(@F036_COMMAND@);
        struct stat information;
        sdk_require(stat(@F036_COMMAND@, &information) == 0, "sdk-probe-image-failed\n");
        probe.event.file = (LoProcessFileIdentity){(uint64_t)information.st_dev, (uint64_t)information.st_ino};
        pthread_t thread;
        sdk_require(pthread_create(&thread, NULL, sdk_probe_auth, &probe) == 0, "sdk-probe-thread-failed\n");
        uint64_t deadline = fixture_now() + 1000;
        while (!__atomic_load_n(&sdk_wait_entered, __ATOMIC_SEQ_CST)) {
            sdk_require(fixture_now() < deadline, "sdk-auth-wait-not-entered\n");
            sched_yield();
        }
        sdk_require(sdk_adapter.close(sdk_adapter.handle) == EBUSY, "sdk-close-consumed-pending-auth\n");
        fixture_log("sdk-close-auth-busy\n");
        __atomic_store_n(&sdk_expire_deadline, 1, __ATOMIC_SEQ_CST);
        sdk_require(pthread_join(thread, NULL) == 0, "sdk-probe-thread-not-joined\n");
        fixture_log("sdk-borrow-ended\n");
    }
    return proc_signal_with_audittoken(token, number);
}
static void *sdk_closed_callback(void *argument) {
    struct sdk_client *owner = argument;
    struct sdk_delivery delivery = {0};
    delivery.message.action_type = ES_ACTION_TYPE_AUTH;
    delivery.borrowed = 1;
    owner->callback((es_client_t *)owner, &delivery.message);
    delivery.borrowed = 0;
    sdk_require(delivery.replies == 1, "sdk-close-auth-unanswered\n");
    fixture_log("sdk-close-callback-joined\n");
    return NULL;
}
static es_return_t sdk_delete(es_client_t *client) {
    struct sdk_client *owner = (void *)client;
    fixture_log("sdk-delete\n");
    if (SDK_CASE == SDK_CLOSE_CALLBACK) {
        pthread_t callback;
        sdk_require(pthread_create(&callback, NULL, sdk_closed_callback, owner) == 0
            && pthread_join(callback, NULL) == 0, "sdk-delete-callback-failed\n");
    }
    sdk_require(owner->events.close(owner->events.handle) == 0, "sdk-event-source-not-closed\n");
    Block_release(owner->callback);
    free(owner); sdk_current = NULL;
    return SDK_CASE == SDK_DELETE_FAILURE ? ES_RETURN_ERROR : ES_RETURN_SUCCESS;
}
static void *sdk_dlopen(const char *path, int flags) {
    sdk_require(!strcmp(path, "/usr/lib/libEndpointSecurity.dylib")
        && flags == (RTLD_NOW | RTLD_LOCAL), "sdk-library-selection-invalid\n");
    return (void *)(uintptr_t)36;
}
static int sdk_dlclose(void *library) {
    sdk_require(library == (void *)(uintptr_t)36, "sdk-library-close-invalid\n");
    fixture_log("sdk-library-closed\n"); return 0;
}
static void *sdk_dlsym(void *library, const char *name) {
    if (!strcmp(name, "proc_signal_with_audittoken")) {
        sdk_require(library == RTLD_DEFAULT, "sdk-signal-library-invalid\n");
        return (void *)sdk_signal;
    }
    sdk_require(library == (void *)(uintptr_t)36, "sdk-symbol-library-invalid\n");
#define SDK_BIND(symbol, implementation) if (!strcmp(name, #symbol)) return (void *)implementation
    SDK_BIND(es_new_descendants_client, sdk_new);
    SDK_BIND(es_delete_client, sdk_delete);
    SDK_BIND(es_subscribe, sdk_subscribe);
    SDK_BIND(es_respond_auth_result, sdk_respond);
    SDK_BIND(es_unmute_all_paths, sdk_unmute_paths);
    SDK_BIND(es_unmute_all_target_paths, sdk_unmute_targets);
    SDK_BIND(es_muted_processes_events, sdk_muted);
    SDK_BIND(es_unmute_process, sdk_unmute_process);
    SDK_BIND(es_release_muted_processes, sdk_release_muted);
    SDK_BIND(es_muting_inverted, sdk_inverted);
    SDK_BIND(es_set_deadline_miss_mode, sdk_set_deadline);
    SDK_BIND(es_get_deadline_miss_mode, sdk_get_deadline);
    if (!strcmp(name, "es_sync_client")) return SDK_CASE == SDK_MISSING_API ? NULL : (void *)sdk_sync;
#undef SDK_BIND
    sdk_require(0, "sdk-unexpected-symbol\n"); return NULL;
}
#define dlopen sdk_dlopen
#define dlsym sdk_dlsym
#define dlclose sdk_dlclose
#define mach_absolute_time sdk_absolute_time
#include @F036_ADAPTER_SOURCE@
#undef mach_absolute_time
#undef dlclose
#undef dlsym
#undef dlopen

static uint32_t sdk_native_open(uintptr_t user, const LoProcessOwnershipConfiguration *configuration,
    LoProcessOwnershipEmit emit, void *destination, LoProcessOwnershipClient *client) {
    sdk_require(user == 36, "sdk-factory-user-invalid\n");
    sdk_configuration = *configuration;
    uint32_t result = lo_process_es_open(configuration, emit, destination, client);
    sdk_adapter = *client;
    if (result == LO_PROCESS_OWNER_READY) fixture_log("sdk-admitted\n");
    return result;
}
