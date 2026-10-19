#include "ipc.h"
#include <mach/exception_types.h>
#include <mach/thread_status.h>
#include <servers/bootstrap.h>
#include <limits.h>
#include <stdio.h>
#include <string.h>

static int drop(mach_port_t port) {
    return !MACH_PORT_VALID(port) || mach_port_deallocate(mach_task_self(), port) == KERN_SUCCESS ? 0 : 1;
}

static int drop_array(mach_port_array_t ports, mach_msg_type_number_t count) {
    int failures = 0;
    for (mach_msg_type_number_t index = 0; index < count; index++) failures += drop(ports[index]);
    if (count != 0 && vm_deallocate(mach_task_self(), (vm_address_t)ports, count * sizeof(mach_port_t)) != KERN_SUCCESS) failures++;
    return failures;
}

int lo_ipc_variant(const char *variant) {
    if (strcmp(variant, "inheritance-control") == 0) return 0;
    if (strcmp(variant, "normalized-control") == 0) return 1;
    if (strcmp(variant, "normalized-confined") == 0) return 2;
    return -1;
}

enum { MAX_REGISTERED_OBSERVATIONS = 1024, MAX_SELECTED_RIGHTS = MAX_REGISTERED_OBSERVATIONS + 2 * EXC_TYPES_COUNT + 2 };

struct selected_right { mach_port_t port; unsigned int copies; };

static void acquired(mach_port_t port, mach_port_t selected[MAX_SELECTED_RIGHTS], unsigned int *count) {
    if (MACH_PORT_VALID(port)) selected[(*count)++] = port;
}

static int collect_clear_exceptions(mach_port_t subject, int thread, mach_port_t selected[MAX_SELECTED_RIGHTS], unsigned int *selected_count) {
    exception_mask_t masks[EXC_TYPES_COUNT];
    mach_port_t ports[EXC_TYPES_COUNT];
    exception_behavior_t behaviors[EXC_TYPES_COUNT];
    thread_state_flavor_t flavors[EXC_TYPES_COUNT];
    mach_msg_type_number_t count = EXC_TYPES_COUNT;
    kern_return_t observed = thread ? thread_get_exception_ports(subject, EXC_MASK_ALL, masks, &count, ports, behaviors, flavors) : task_get_exception_ports(subject, EXC_MASK_ALL, masks, &count, ports, behaviors, flavors);
    int failures = observed == KERN_SUCCESS ? 0 : 1;
    kern_return_t cleared = thread ? thread_set_exception_ports(subject, EXC_MASK_ALL, MACH_PORT_NULL, EXCEPTION_DEFAULT, THREAD_STATE_NONE) : task_set_exception_ports(subject, EXC_MASK_ALL, MACH_PORT_NULL, EXCEPTION_DEFAULT, THREAD_STATE_NONE);
    if (cleared != KERN_SUCCESS) failures++;
    if (observed == KERN_SUCCESS) for (mach_msg_type_number_t index = 0; index < count; index++) acquired(ports[index], selected, selected_count);
    return failures;
}

int lo_normalize_inherited_rights(void) {
    mach_port_t self = mach_task_self(), initial = mach_thread_self(), host = mach_host_self();
    int self_held = mach_port_mod_refs(self, self, MACH_PORT_RIGHT_SEND, 1) == KERN_SUCCESS;
    int failures = self_held && MACH_PORT_VALID(initial) && MACH_PORT_VALID(host) ? 0 : 1;
    mach_port_t selected[MAX_SELECTED_RIGHTS];
    unsigned int selected_count = 0;
    mach_port_array_t ports = NULL;
    mach_msg_type_number_t count = 0;
    kern_return_t observed = mach_ports_lookup(mach_task_self(), &ports, &count);
    if (observed != KERN_SUCCESS) failures++;
    if (mach_ports_register(mach_task_self(), NULL, 0) != KERN_SUCCESS) failures++;
    if (observed == KERN_SUCCESS) {
        if (count > MAX_REGISTERED_OBSERVATIONS) {
            /* Reject an out-of-bound SDK response, release its acquired data,
               and fail closed rather than treating partial inventory as clear. */
            failures += 1 + drop_array(ports, count);
            ports = NULL;
            count = 0;
        } else for (mach_msg_type_number_t index = 0; index < count; index++) acquired(ports[index], selected, &selected_count);
    }
    mach_port_t bootstrap = MACH_PORT_NULL;
    observed = task_get_special_port(mach_task_self(), TASK_BOOTSTRAP_PORT, &bootstrap);
    if (observed != KERN_SUCCESS) failures++;
    if (task_set_special_port(mach_task_self(), TASK_BOOTSTRAP_PORT, MACH_PORT_NULL) != KERN_SUCCESS) failures++;
    if (observed == KERN_SUCCESS) acquired(bootstrap, selected, &selected_count);
    /* The public cache owns a reference distinct from the getter copy. Hold
       both until all selected aliases have been accounted for. */
    mach_port_t cached = bootstrap_port;
    bootstrap_port = MACH_PORT_NULL;
    acquired(cached, selected, &selected_count);
    failures += collect_clear_exceptions(self, 0, selected, &selected_count);
    if (MACH_PORT_VALID(initial)) failures += collect_clear_exceptions(initial, 1, selected, &selected_count);
    /* Deduplicate while every SDK reference is still live. Never query a name
       again after its final reference has been dropped. */
    struct selected_right unique[MAX_SELECTED_RIGHTS];
    unsigned int unique_count = 0;
    for (unsigned int index = 0; index < selected_count; index++) {
        unsigned int candidate;
        for (candidate = 0; candidate < unique_count; candidate++) if (unique[candidate].port == selected[index]) break;
        if (candidate == unique_count) { unique[candidate].port = selected[index]; unique[candidate].copies = 0; unique_count++; }
        unique[candidate].copies++;
    }
    for (unsigned int index = 0; index < unique_count; index++) {
        struct selected_right right = unique[index];
        int protected = right.port == self || right.port == initial || right.port == host;
        mach_port_urefs_t references = 0;
        if (!protected && mach_port_get_refs(self, right.port, MACH_PORT_RIGHT_SEND, &references) == KERN_SUCCESS && references >= right.copies && references <= INT_MAX) {
            if (mach_port_mod_refs(self, right.port, MACH_PORT_RIGHT_SEND, -(mach_port_delta_t)references) == KERN_SUCCESS) continue;
        }
        /* Selected endpoints may not consume outside runtime authority. On
           this failure path release only copies actually acquired above. */
        failures++;
        for (unsigned int copy = 0; copy < right.copies; copy++) failures += drop(right.port);
    }
    if (count != 0 && vm_deallocate(self, (vm_address_t)ports, count * sizeof(mach_port_t)) != KERN_SUCCESS) failures++;
    if (self_held) failures += drop(self);
    failures += drop(initial);
    failures += drop(host);
    return failures == 0 ? 0 : 1;
}

struct exception_observation {
    kern_return_t status;
    unsigned int nonempty;
    int matches;
    int cleanup_failed;
};

static struct exception_observation exceptions(mach_port_t subject, int thread, mach_port_t reference) {
    struct exception_observation observation = {0, 0, 0, 0};
    exception_mask_t masks[EXC_TYPES_COUNT];
    mach_port_t ports[EXC_TYPES_COUNT];
    exception_behavior_t behaviors[EXC_TYPES_COUNT];
    thread_state_flavor_t flavors[EXC_TYPES_COUNT];
    mach_msg_type_number_t count = EXC_TYPES_COUNT;
    observation.status = thread ? thread_get_exception_ports(subject, EXC_MASK_ALL, masks, &count, ports, behaviors, flavors) : task_get_exception_ports(subject, EXC_MASK_ALL, masks, &count, ports, behaviors, flavors);
    if (observation.status == KERN_SUCCESS) {
        for (mach_msg_type_number_t index = 0; index < count; index++) {
            if (MACH_PORT_VALID(ports[index])) observation.nonempty++;
            if (MACH_PORT_VALID(reference) && ports[index] == reference) observation.matches = 1;
            observation.cleanup_failed += drop(ports[index]);
        }
    }
    return observation;
}

static int send_message(mach_port_t destination, int channel) {
    mach_msg_header_t message = {0};
    message.msgh_bits = MACH_MSGH_BITS(MACH_MSG_TYPE_COPY_SEND, 0);
    message.msgh_size = sizeof(message);
    message.msgh_remote_port = destination;
    message.msgh_id = LO_IPC_MESSAGE_BASE + channel;
    return mach_msg(&message, MACH_SEND_MSG | MACH_SEND_TIMEOUT, sizeof(message), 0, MACH_PORT_NULL, 0, MACH_PORT_NULL);
}

int lo_ipc_snapshot(const char *variant, const char *phase, int send_canary_messages) {
    mach_port_array_t ports = NULL;
    mach_msg_type_number_t count = 0;
    kern_return_t registered_status = mach_ports_lookup(mach_task_self(), &ports, &count);
    unsigned int registered_nonempty = 0;
    if (registered_status == KERN_SUCCESS) for (mach_msg_type_number_t index = 0; index < count; index++) if (MACH_PORT_VALID(ports[index])) registered_nonempty++;
    mach_port_t reference = registered_status == KERN_SUCCESS && count > 0 && registered_nonempty == 1 && MACH_PORT_VALID(ports[0]) ? ports[0] : MACH_PORT_NULL;
    mach_port_t bootstrap = MACH_PORT_NULL;
    kern_return_t bootstrap_status = task_get_special_port(mach_task_self(), TASK_BOOTSTRAP_PORT, &bootstrap);
    thread_t initial = mach_thread_self();
    struct exception_observation task = exceptions(mach_task_self(), 0, reference);
    struct exception_observation thread = exceptions(initial, 1, reference);
    mach_port_type_t task_type = 0, thread_type = 0;
    kern_return_t self_task_status = mach_port_type(mach_task_self(), mach_task_self(), &task_type);
    kern_return_t self_thread_status = mach_port_type(mach_task_self(), initial, &thread_type);
    int self_task_valid = self_task_status == KERN_SUCCESS && (task_type & MACH_PORT_TYPE_SEND) != 0;
    int self_thread_valid = self_thread_status == KERN_SUCCESS && (thread_type & MACH_PORT_TYPE_SEND) != 0;
    int bootstrap_nonempty = MACH_PORT_VALID(bootstrap);
    int matched[LO_IPC_CHANNELS] = {MACH_PORT_VALID(reference), MACH_PORT_VALID(reference) && bootstrap_status == KERN_SUCCESS && bootstrap == reference, task.matches, thread.matches};
    int sends[LO_IPC_CHANNELS] = {-1, -1, -1, -1};
    if (send_canary_messages) for (int channel = 0; channel < LO_IPC_CHANNELS; channel++) if (matched[channel]) sends[channel] = send_message(reference, channel);
    int failures = task.cleanup_failed + thread.cleanup_failed;
    if (bootstrap_status == KERN_SUCCESS) failures += drop(bootstrap);
    if (registered_status == KERN_SUCCESS) failures += drop_array(ports, count);
    failures += drop(initial);
    printf("{\"kind\":\"snapshot\",\"variant\":\"%s\",\"phase\":\"%s\",\"api_status\":[%d,%d,%d,%d],\"nonempty_slots\":[%u,%d,%u,%u],\"matches_reference\":[%s,%s,%s,%s],\"canary_message_status\":[%d,%d,%d,%d],\"bootstrap_cache_nonempty\":%s,\"self_task_valid\":%s,\"self_thread_valid\":%s,\"copy_cleanup_complete\":%s}\n", variant, phase, registered_status, bootstrap_status, task.status, thread.status, registered_nonempty, bootstrap_nonempty, task.nonempty, thread.nonempty, matched[0] ? "true" : "false", matched[1] ? "true" : "false", matched[2] ? "true" : "false", matched[3] ? "true" : "false", sends[0], sends[1], sends[2], sends[3], MACH_PORT_VALID(bootstrap_port) ? "true" : "false", self_task_valid ? "true" : "false", self_thread_valid ? "true" : "false", failures == 0 ? "true" : "false");
    if (fflush(stdout) != 0) return 1;
    return failures == 0 && self_task_valid && self_thread_valid ? 0 : 1;
}
