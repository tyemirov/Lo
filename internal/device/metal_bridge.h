#ifndef LO_METAL_BRIDGE_H
#define LO_METAL_BRIDGE_H
#include <stddef.h>
#include <stdint.h>

typedef struct LoMetalSession LoMetalSession;
typedef struct LoMetalBuffer LoMetalBuffer;
enum { LO_METAL_UNAVAILABLE = 1, LO_METAL_PROFILE, LO_METAL_LIBRARY,
       LO_METAL_ALLOCATION, LO_METAL_TRANSFER, LO_METAL_LOST };
typedef struct { int code; char message[512]; } LoMetalError;

LoMetalSession *lo_metal_open(const void *library, size_t length, LoMetalError *error);
void lo_metal_close(LoMetalSession *session);
const char *lo_metal_name(LoMetalSession *session);
uint64_t lo_metal_registry(LoMetalSession *session);
LoMetalBuffer *lo_metal_allocate(LoMetalSession *session, uint64_t bytes, LoMetalError *error);
uint64_t lo_metal_reserved(LoMetalBuffer *buffer);
int lo_metal_upload(LoMetalBuffer *buffer, const void *values, uint64_t bytes, LoMetalError *error);
int lo_metal_download(LoMetalBuffer *buffer, void *values, uint64_t bytes, LoMetalError *error);
void lo_metal_release(LoMetalBuffer *buffer);
#endif
