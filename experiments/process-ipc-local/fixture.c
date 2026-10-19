#include "ipc.h"
#include <mach/exception_types.h>
#include <mach/thread_status.h>
#include <servers/bootstrap.h>
#include <stdio.h>

int lo_ipc_local_fixture(int variant) {
    if (variant < 0 || variant > 2) return 1;
    int inherited_inventory = lo_ipc_inventory("inherited-prestate-no-send", variant, MACH_PORT_NULL, 0, 0);
    int inherited_normalized = lo_normalize_inherited_rights();
    int inherited_empty = lo_ipc_inventory("inherited-normalized", variant, MACH_PORT_NULL, 0, 1);
    if (inherited_inventory != 0 || inherited_normalized != 0 || inherited_empty != 0) return 1;
    mach_port_t local = MACH_PORT_NULL;
    int setup_failed = mach_port_allocate(mach_task_self(), MACH_PORT_RIGHT_RECEIVE, &local) != KERN_SUCCESS;
    if (!setup_failed && mach_port_insert_right(mach_task_self(), local, local, MACH_MSG_TYPE_MAKE_SEND) != KERN_SUCCESS) setup_failed = 1;
    /* Two extra aliases verify the generic routine drains known selected SEND
       user references rather than merely its temporary getter copies. */
    if (!setup_failed && mach_port_mod_refs(mach_task_self(), local, MACH_PORT_RIGHT_SEND, 2) != KERN_SUCCESS) setup_failed = 1;
    mach_port_t aliases[3] = {local, local, local};
    if (!setup_failed && mach_ports_register(mach_task_self(), aliases, 3) != KERN_SUCCESS) setup_failed = 1;
    if (!setup_failed && task_set_special_port(mach_task_self(), TASK_BOOTSTRAP_PORT, local) != KERN_SUCCESS) setup_failed = 1;
    if (!setup_failed) {
        if (mach_port_mod_refs(mach_task_self(), local, MACH_PORT_RIGHT_SEND, 1) != KERN_SUCCESS) setup_failed = 1;
        else bootstrap_port = local;
    }
    if (!setup_failed && task_set_exception_ports(mach_task_self(), EXC_MASK_ALL, local, EXCEPTION_DEFAULT, THREAD_STATE_NONE) != KERN_SUCCESS) setup_failed = 1;
    thread_t initial = mach_thread_self();
    if (!MACH_PORT_VALID(initial)) setup_failed = 1;
    else {
        if (!setup_failed && thread_set_exception_ports(initial, EXC_MASK_ALL, local, EXCEPTION_DEFAULT, THREAD_STATE_NONE) != KERN_SUCCESS) setup_failed = 1;
        if (mach_port_deallocate(mach_task_self(), initial) != KERN_SUCCESS) setup_failed = 1;
    }
    int positive = setup_failed ? 1 : lo_ipc_inventory("local-positive-control", variant, local, 1, 0);
    int normalized = lo_normalize_inherited_rights();
    mach_port_type_t remaining = 0;
    int send_removed = MACH_PORT_VALID(local) && mach_port_type(mach_task_self(), local, &remaining) == KERN_SUCCESS && (remaining & MACH_PORT_TYPE_RECEIVE) != 0 && (remaining & MACH_PORT_TYPE_SEND) == 0;
    int normalized_empty = lo_ipc_inventory("local-normalized", variant, MACH_PORT_NULL, 0, 1);
    int destroyed = MACH_PORT_VALID(local) && mach_port_destroy(mach_task_self(), local) == KERN_SUCCESS;
    /* There are no name queries after the continuously held RECEIVE is gone. */
    int cleanup = normalized == 0 && normalized_empty == 0 && (!MACH_PORT_VALID(local) || (send_removed && destroyed));
    int complete = !setup_failed && positive == 0 && send_removed && destroyed && cleanup;
    printf("{\"kind\":\"local_fixture\",\"variant\":\"%s\",\"inherited_normalized\":true,\"setup_complete\":%s,\"positive_controls\":%s,\"normalization_complete\":%s,\"send_removed_through_live_receive\":%s,\"receive_destroyed\":%s,\"cleanup_complete\":%s}\n", lo_ipc_variant_name(variant), setup_failed ? "false" : "true", positive == 0 ? "true" : "false", normalized == 0 ? "true" : "false", send_removed ? "true" : "false", destroyed ? "true" : "false", cleanup ? "true" : "false");
    if (fflush(stdout) != 0) return 1;
    return complete ? 0 : 1;
}
