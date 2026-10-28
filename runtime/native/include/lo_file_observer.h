#ifndef LO_FILE_OBSERVER_H
#define LO_FILE_OBSERVER_H
#include "stdint.h"

typedef struct LoContext LoContext;
typedef struct {
    uint64_t owner_uid;
    int64_t accessed_sec, accessed_nsec;
    int64_t modified_sec, modified_nsec;
    int64_t changed_sec, changed_nsec;
    uint64_t link_count, size_bytes;
} LoFileMetadata;
enum { LO_FILE_DIRECTORY=1, LO_FILE_DISCOVERY, LO_FILE_PREFLIGHT, LO_FILE_REMOVE };
/* Trusted synchronous embedding hook. Callback and userdata must outlive every
 * context and request in the session. Install before any session evaluation.
 * Called after a successful native lookup; identity and file kind remain native.
 * Return 0 to accept metadata, positive errno for a lookup failure, negative for
 * a terminal observer failure. Root/name bytes are borrowed for this call only. */
typedef int (*LoFileMetadataObserver)(uintptr_t, uint32_t, const unsigned char *,
    uint64_t, const unsigned char *, uint64_t, LoFileMetadata *);
int lo_context_file_metadata_observer(LoContext *, LoFileMetadataObserver, uintptr_t);
#endif
