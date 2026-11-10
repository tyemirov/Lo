#ifndef LO_PROCESS_OWNERSHIP_H
#define LO_PROCESS_OWNERSHIP_H
#include <stddef.h>
#include <stdint.h>

/* Host-only descendant authority. These limits do not change ContextOptions
 * or the Process driver ABI. Zero and unlimited values are not accepted. */
typedef struct {
    uint32_t max_processes, max_tokens;
    uint64_t max_events;
    uint32_t kernel_processes;
} LoProcessDescendantLimits;
typedef struct { uint32_t word[8]; } LoProcessExecutionToken;
typedef struct { uint64_t device, inode; } LoProcessFileIdentity;
typedef struct {
    uint64_t generation;
    LoProcessExecutionToken supervisor;
    LoProcessFileIdentity marker;
    uint64_t max_events;
    uint32_t max_processes, max_tokens, kernel_processes;
} LoProcessOwnershipConfiguration;

enum {
    LO_PROCESS_OWNER_FORK = 1,
    LO_PROCESS_OWNER_AUTH_EXEC = 2,
    LO_PROCESS_OWNER_EXEC = 3,
    LO_PROCESS_OWNER_EXIT = 4,
    LO_PROCESS_OWNER_OPEN = 5,
    LO_PROCESS_OWNER_SYNC = 6,
    LO_PROCESS_OWNER_FAILURE = 7
};
enum { LO_PROCESS_OWNER_OPEN_READ = 1, LO_PROCESS_OWNER_OPEN_WRITE = 2 };
enum { LO_PROCESS_OWNER_LOST = 1, LO_PROCESS_OWNER_CLOSED = 2,
       LO_PROCESS_OWNER_INVALID = 3 };
#define LO_PROCESS_OWNER_PATH_MAX 1024U
typedef struct {
    uint32_t kind;
    uint64_t generation, sequence, request;
    LoProcessExecutionToken actor, target;
    LoProcessFileIdentity file;
    const unsigned char *path;
    size_t path_size;
    uint32_t flags;
    int32_t status;
} LoProcessOwnershipEvent;

/* Event pointers are borrowed until emit returns. The controller copies them.
 * The provider serializes events and synchronization callbacks in one order.
 * Its operations can emit synchronous callbacks. The controller does not call
 * provider operations while it holds the event-state mutex.
 * For AUTH_EXEC, one means exactly one reply obligation was retained, including
 * a denied obligation in quarantine. It does not authorize execution.
 * For AUTH_EXEC, zero means no reply obligation was retained. The provider must
 * immediately deny without caching and release that request exactly once.
 * The controller does not later respond to an AUTH request it did not ingest.
 * Other event return values never authorize stopping lifecycle delivery.
 * Later events remain required for fixed-storage quarantine cleanup.
 * FORK uses actor=parent and target=child. AUTH_EXEC uses actor=old token and
 * target=proposed token. EXEC uses actor=confirmed current token.
 * EXIT and OPEN use actor=current token. The file and path identify exec/open.
 * Lifecycle and OPEN events carry the full global sequence number.
 * SYNC carries its request and active generation. It consumes no sequence.
 * FAILURE records an observation failure. It cannot clear prior failures.
 * AUTH_EXEC request identifies one retained request within this client.
 * The audit token supplies credentials. Unused fields must be zero.
 * OPEN flags contain READ and/or WRITE. Other events have zero flags.
 * FAILURE status is LOST, CLOSED, or INVALID. Other events have zero status.
 * AUTH_EXEC and EXEC paths are nonempty UTF-8 without NUL and have at most
 * PATH_MAX bytes. OPEN paths can be empty. Other events have no path.
 * A nonempty path has a nonnull pointer. Validate its bound before allocation.
 * Every event uses the active generation. Reject unknown kinds and flags.
 * The controller enforces sequence continuity and exact registry identity.
 * An exec cannot add another initial executable's filesystem or endpoint scope. */
typedef int (*LoProcessOwnershipEmit)(void *, const LoProcessOwnershipEvent *);
enum { LO_PROCESS_OWNER_DENY = 0, LO_PROCESS_OWNER_ALLOW = 1 };
enum { LO_PROCESS_OWNER_READY = 0, LO_PROCESS_OWNER_UNAVAILABLE = 1,
       LO_PROCESS_OWNER_FAILED = 2 };

typedef struct {
    uintptr_t handle;
    /* Return an owned marker descriptor, or -1 without a descriptor.
     * The controller verifies its identity and closes it after synchronization. */
    int (*open_marker)(uintptr_t, uint64_t request);
    /* Return zero only if the synchronization request was accepted. */
    int (*synchronize)(uintptr_t, uint64_t request);
    /* Respond once. Return zero only if the reply and request release succeed. */
    int (*respond)(uintptr_t, uint64_t request, uint32_t decision);
    /* Return zero or the platform error. ESRCH does not prove process exit. */
    int (*signal)(uintptr_t, const LoProcessExecutionToken *, int32_t signal);
    /* Zero joins callbacks and consumes the client. Failure retains the client. */
    int (*close)(uintptr_t);
} LoProcessOwnershipClient;

/* Open completes platform setup before READY. The controller still requires
 * marker and synchronization admission. Setup includes complete subscriptions,
 * no default muting, fail-closed deadlines, and disabled authorization caching.
 * Failure with a live client retains that client for controlled cleanup.
 * The native serial callback borrows one AUTH message until respond or its
 * bounded deadline supplies exactly one reply. A late respond must fail.
 * Code and userdata outlive the supervisor. No pointer crosses an exec boundary. */
typedef uint32_t (*LoProcessOwnershipOpen)(uintptr_t userdata,
    const LoProcessOwnershipConfiguration *, LoProcessOwnershipEmit, void *,
    LoProcessOwnershipClient *);

/* Install once before any CLI dispatch. Return one on success.
 * A compiled host repeats registration after re-exec. Arguments and environment
 * values cannot select this factory. Absence selects the native provider.
 * Failure of the selected provider cannot select another provider.
 * Registration is sealed before internal CLI dispatch and before activation. */
int lo_process_ownership_provider(LoProcessOwnershipOpen, uintptr_t userdata);

#endif
