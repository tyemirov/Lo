#include "ipc.h"
#include <mach/exception_types.h>
#include <servers/bootstrap.h>
#include <stdio.h>
#include <string.h>

const char *lo_ipc_variant_name(int variant) {
    static const char *names[] = {"local-control", "normalized-control", "normalized-confined"};
    return variant >= 0 && variant < 3 ? names[variant] : "invalid";
}

static int dropped(mach_port_t port) {
    return !MACH_PORT_VALID(port) || mach_port_deallocate(mach_task_self(), port) == KERN_SUCCESS ? 0 : 1;
}

static int delivery(mach_port_t known, int channel) {
    /* Destination identity is the locally allocated, continuously held RECEIVE
       right. No SDK-enumerated or inherited endpoint is ever messaged. */
    mach_msg_header_t outgoing = {0};
    outgoing.msgh_bits = MACH_MSGH_BITS(MACH_MSG_TYPE_COPY_SEND, 0);
    outgoing.msgh_size = sizeof(outgoing);
    outgoing.msgh_remote_port = known;
    outgoing.msgh_id = 0x4c4f4c00 + channel;
    if (mach_msg(&outgoing, MACH_SEND_MSG | MACH_SEND_TIMEOUT, sizeof(outgoing), 0, MACH_PORT_NULL, 0, MACH_PORT_NULL) != KERN_SUCCESS) return 0;
    struct { mach_msg_header_t header; mach_msg_max_trailer_t trailer; } incoming;
    memset(&incoming, 0, sizeof(incoming));
    if (mach_msg(&incoming.header, MACH_RCV_MSG | MACH_RCV_TIMEOUT, 0, sizeof(incoming), known, 0, MACH_PORT_NULL) != KERN_SUCCESS) return 0;
    int valid = incoming.header.msgh_id == outgoing.msgh_id && incoming.header.msgh_size == sizeof(mach_msg_header_t) && (incoming.header.msgh_bits & MACH_MSGH_BITS_COMPLEX) == 0 && !MACH_PORT_VALID(incoming.header.msgh_remote_port);
    if (!valid) mach_msg_destroy(&incoming.header);
    return valid;
}

int lo_ipc_inventory(const char *phase, int variant, mach_port_t known, int positive, int empty) {
    int statuses[4] = {-1, -1, -1, -1}, matches[4] = {0, 0, 0, 0}, delivered[4] = {0, 0, 0, 0};
    unsigned int nonempty[4] = {0, 0, 0, 0};
    int cleanup_failed = 0;
    mach_port_array_t registered = NULL;
    mach_msg_type_number_t count = 0;
    statuses[0] = mach_ports_lookup(mach_task_self(), &registered, &count);
    if (statuses[0] == KERN_SUCCESS) {
        int every_match = count > 0;
        for (mach_msg_type_number_t index = 0; index < count; index++) {
            if (MACH_PORT_VALID(registered[index])) nonempty[0]++;
            if (registered[index] != known) every_match = 0;
            cleanup_failed += dropped(registered[index]);
        }
        matches[0] = MACH_PORT_VALID(known) && every_match && nonempty[0] == 3;
        if (count != 0 && vm_deallocate(mach_task_self(), (vm_address_t)registered, count * sizeof(mach_port_t)) != KERN_SUCCESS) cleanup_failed++;
    }
    mach_port_t bootstrap = MACH_PORT_NULL;
    statuses[1] = task_get_special_port(mach_task_self(), TASK_BOOTSTRAP_PORT, &bootstrap);
    if (statuses[1] == KERN_SUCCESS) {
        nonempty[1] = MACH_PORT_VALID(bootstrap);
        matches[1] = MACH_PORT_VALID(known) && bootstrap == known && bootstrap_port == known;
        cleanup_failed += dropped(bootstrap);
    }
    thread_t initial = mach_thread_self();
    for (int channel = 2; channel < 4; channel++) {
        exception_mask_t masks[EXC_TYPES_COUNT];
        mach_port_t ports[EXC_TYPES_COUNT];
        exception_behavior_t behaviors[EXC_TYPES_COUNT];
        thread_state_flavor_t flavors[EXC_TYPES_COUNT];
        mach_msg_type_number_t size = EXC_TYPES_COUNT;
        statuses[channel] = channel == 2 ? task_get_exception_ports(mach_task_self(), EXC_MASK_ALL, masks, &size, ports, behaviors, flavors) : thread_get_exception_ports(initial, EXC_MASK_ALL, masks, &size, ports, behaviors, flavors);
        if (statuses[channel] == KERN_SUCCESS) {
            int every_match = 1;
            for (mach_msg_type_number_t index = 0; index < size; index++) {
                if (MACH_PORT_VALID(ports[index])) { nonempty[channel]++; if (ports[index] != known) every_match = 0; }
                cleanup_failed += dropped(ports[index]);
            }
            matches[channel] = MACH_PORT_VALID(known) && every_match && nonempty[channel] > 0;
        }
    }
    mach_port_t runtime[3] = {mach_task_self(), initial, mach_host_self()};
    int runtime_valid[3] = {0, 0, 0};
    for (int index = 0; index < 3; index++) {
        mach_port_type_t type = 0;
        runtime_valid[index] = mach_port_type(mach_task_self(), runtime[index], &type) == KERN_SUCCESS && (type & MACH_PORT_TYPE_SEND) != 0;
    }
    cleanup_failed += dropped(runtime[1]) + dropped(runtime[2]);
    int success = cleanup_failed == 0 && runtime_valid[0] && runtime_valid[1] && runtime_valid[2];
    for (int channel = 0; channel < 4; channel++) {
        if (statuses[channel] != KERN_SUCCESS) success = 0;
        if (positive) { if (matches[channel]) delivered[channel] = delivery(known, channel); if (!matches[channel] || !delivered[channel]) success = 0; }
        if (empty && nonempty[channel] != 0) success = 0;
    }
    int cache_empty = !MACH_PORT_VALID(bootstrap_port);
    if (empty && !cache_empty) success = 0;
    printf("{\"kind\":\"inventory\",\"variant\":\"%s\",\"phase\":\"%s\",\"api_status\":[%d,%d,%d,%d],\"nonempty_slots\":[%u,%u,%u,%u],\"matches_known_local\":[%s,%s,%s,%s],\"local_deliveries\":[%s,%s,%s,%s],\"bootstrap_cache_empty\":%s,\"runtime_valid\":[%s,%s,%s],\"copy_cleanup_complete\":%s,\"requirements_met\":%s}\n", lo_ipc_variant_name(variant), phase, statuses[0], statuses[1], statuses[2], statuses[3], nonempty[0], nonempty[1], nonempty[2], nonempty[3], matches[0] ? "true" : "false", matches[1] ? "true" : "false", matches[2] ? "true" : "false", matches[3] ? "true" : "false", delivered[0] ? "true" : "false", delivered[1] ? "true" : "false", delivered[2] ? "true" : "false", delivered[3] ? "true" : "false", cache_empty ? "true" : "false", runtime_valid[0] ? "true" : "false", runtime_valid[1] ? "true" : "false", runtime_valid[2] ? "true" : "false", cleanup_failed ? "false" : "true", success ? "true" : "false");
    if (fflush(stdout) != 0) return 1;
    return success ? 0 : 1;
}
