#ifndef LO_IPC_H
#define LO_IPC_H

#include <mach/mach.h>
#include <stdint.h>

enum { LO_IPC_CHANNELS = 4, LO_IPC_MESSAGE_BASE = 0x4c4f4900 };

/* All name comparisons occur in the observing task, before copies are dropped. */
int lo_ipc_snapshot(const char *variant, const char *phase, int send_canary_messages);
int lo_normalize_inherited_rights(void);
int lo_ipc_variant(const char *variant);

#endif
