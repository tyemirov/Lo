#ifndef LO_HOST_OBSERVER_H
#define LO_HOST_OBSERVER_H
#include "lo_runtime.h"

typedef struct { int64_t seconds, nanoseconds; } LoClockMetadata;
typedef struct {
    uint64_t fragment_bytes, total_blocks, free_blocks, available_blocks;
} LoVolumeMetadata;
typedef int (*LoClockObserver)(uintptr_t, LoClockMetadata *);
typedef int (*LoVolumeObserver)(uintptr_t, const unsigned char *, uint64_t, LoVolumeMetadata *);
/* Install trusted synchronous providers before session evaluation on the owner
 * thread. The provider and userdata must outlive the session and its requests.
 * Return zero for a valid observation, positive errno for an observation failure,
 * or a negative result for terminal provider failure. Root bytes are borrowed.
 * Volume providers run after exact directory authority and native measurement.
 * Clock providers run after the native clock read. They do not change invocation
 * capture. No provider may reenter or destroy the context. Cancellation is valid. */
int lo_context_clock_observer(Context *, LoClockObserver, uintptr_t);
int lo_context_volume_observer(Context *, LoVolumeObserver, uintptr_t);
#endif
