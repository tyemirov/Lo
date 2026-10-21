#ifndef LO_DEVICE_DRIVER_H
#define LO_DEVICE_DRIVER_H
#include "stddef.h"
#include "stdint.h"

enum { LO_DEVICE_UNAVAILABLE=1, LO_DEVICE_ALLOCATION, LO_DEVICE_TRANSFER,
       LO_DEVICE_UNSUPPORTED_OPERATION, LO_DEVICE_UNSUPPORTED_PROFILE,
       LO_DEVICE_LIBRARY, LO_DEVICE_LOST, LO_DEVICE_MISMATCH, LO_DEVICE_EXECUTION };
enum { LO_DEVICE_OPEN=1, LO_DEVICE_ALLOCATE, LO_DEVICE_TRANSFER_EVENT,
       LO_DEVICE_RELEASE, LO_DEVICE_CLOSE, LO_DEVICE_COMPUTE };
enum { LO_DEVICE_ADD=1, LO_DEVICE_SUB, LO_DEVICE_MUL, LO_DEVICE_DIV,
       LO_DEVICE_SQRT, LO_DEVICE_RELU, LO_DEVICE_BROADCAST, LO_DEVICE_TRANSPOSE,
       LO_DEVICE_SUM, LO_DEVICE_MEAN, LO_DEVICE_MATMUL, LO_DEVICE_FILL,
       LO_DEVICE_RELU_GRAD, LO_DEVICE_SQRT_GRAD };

typedef struct { uint32_t kind; char message[512]; } LoDeviceError;
typedef struct {
    char name[256], profile[64], registry_id[64], system[128], kernel_sha256[65];
} LoDeviceIdentity;
typedef struct {
    uint32_t kind, count, input_count, region, a_rank, b_rank, rank, scalar;
    uint32_t a[8], b[8], output[8], axes[8];
} LoDeviceOperation;
typedef struct {
    uint32_t event;
    uint64_t allocation, bytes, live_bytes;
    const char *device;
    const LoDeviceIdentity *backend;
} LoDeviceReceipt;
typedef void (*LoDeviceObserver)(uintptr_t, const LoDeviceReceipt *);
typedef struct {
    uintptr_t user;
    int (*open)(uintptr_t, const char *, uintptr_t *, LoDeviceIdentity *, LoDeviceError *);
    int (*close)(uintptr_t, uintptr_t, LoDeviceError *);
    int (*allocate)(uintptr_t, uintptr_t, uint64_t, uintptr_t *, uint64_t *, LoDeviceError *);
    int (*upload)(uintptr_t, uintptr_t, const float *, uint64_t, LoDeviceError *);
    int (*download)(uintptr_t, uintptr_t, float *, uint64_t, LoDeviceError *);
    int (*release)(uintptr_t, uintptr_t, LoDeviceError *);
    int (*supports)(uintptr_t, uintptr_t, const LoDeviceOperation *, LoDeviceError *);
    int (*compute)(uintptr_t, uintptr_t, const LoDeviceOperation *, const uintptr_t *,
                   size_t, uintptr_t, uintptr_t, uint32_t *, LoDeviceError *);
} LoDeviceDriver;
int lo_device_install_driver(const LoDeviceDriver *);
int lo_context_set_device_grant(void *, const char *);
void lo_device_set_observer(uintptr_t, LoDeviceObserver);
#endif
