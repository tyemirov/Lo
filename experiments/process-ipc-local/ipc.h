#ifndef LO_IPC_LOCAL_H
#define LO_IPC_LOCAL_H
#include <mach/mach.h>

int lo_normalize_inherited_rights(void);
int lo_ipc_local_fixture(int variant);
int lo_ipc_inventory(const char *phase, int variant, mach_port_t known, int positive, int empty);
const char *lo_ipc_variant_name(int variant);

#endif
