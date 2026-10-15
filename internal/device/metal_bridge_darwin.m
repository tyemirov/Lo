//go:build metal && darwin && arm64 && cgo

#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#include "metal_bridge.h"

struct LoMetalSession {
    id<MTLDevice> device;
    id<MTLCommandQueue> queue;
    id<MTLComputePipelineState> copy;
    int lost;
};
struct LoMetalBuffer {
    LoMetalSession *session;
    id<MTLBuffer> storage;
    uint64_t length;
};

static void fail(LoMetalError *error, int code, NSString *message) {
    error->code = code;
    snprintf(error->message, sizeof(error->message), "%s", message.UTF8String);
}

void lo_metal_close(LoMetalSession *session) {
    if (!session) return;
    [session->copy release];
    [session->queue release];
    [session->device release];
    free(session);
}

LoMetalSession *lo_metal_open(const void *bytes, size_t length, LoMetalError *error) {
    @autoreleasepool {
        LoMetalSession *session = calloc(1, sizeof(*session));
        if (!session) { fail(error, LO_METAL_UNAVAILABLE, @"allocate session"); return NULL; }
        session->device = MTLCreateSystemDefaultDevice();
        if (!session->device) { fail(error, LO_METAL_UNAVAILABLE, @"no system Metal device"); lo_metal_close(session); return NULL; }
        if (![session->device supportsFamily:MTLGPUFamilyApple7] || !session->device.hasUnifiedMemory) {
            fail(error, LO_METAL_PROFILE, @"require Apple GPU family 7 and unified memory"); lo_metal_close(session); return NULL;
        }
        session->queue = [session->device newCommandQueue];
        if (!session->queue) { fail(error, LO_METAL_UNAVAILABLE, @"create command queue"); lo_metal_close(session); return NULL; }
        dispatch_data_t data = dispatch_data_create(bytes, length, NULL, DISPATCH_DATA_DESTRUCTOR_DEFAULT);
        if (!data) { fail(error, LO_METAL_LIBRARY, @"copy embedded library data"); lo_metal_close(session); return NULL; }
        NSError *cause = nil;
        id<MTLLibrary> library = [session->device newLibraryWithData:data error:&cause];
        dispatch_release(data);
        if (!library) { fail(error, LO_METAL_LIBRARY, [NSString stringWithFormat:@"load embedded library: %@", cause]); lo_metal_close(session); return NULL; }
        id<MTLFunction> function = [library newFunctionWithName:@"lo_copy_words"];
        [library release];
        if (!function) { fail(error, LO_METAL_LIBRARY, @"missing lo_copy_words kernel"); lo_metal_close(session); return NULL; }
        session->copy = [session->device newComputePipelineStateWithFunction:function error:&cause];
        [function release];
        if (!session->copy) { fail(error, LO_METAL_LIBRARY, [NSString stringWithFormat:@"create copy pipeline: %@", cause]); lo_metal_close(session); return NULL; }
        return session;
    }
}

LoMetalBuffer *lo_metal_allocate(LoMetalSession *session, uint64_t bytes, LoMetalError *error) {
    @autoreleasepool {
        if (session->lost) { fail(error, LO_METAL_LOST, @"device session is unusable"); return NULL; }
        if (bytes % 4 || bytes > UINT32_MAX * (uint64_t)4 || bytes > session->device.maxBufferLength / 2) {
            fail(error, LO_METAL_ALLOCATION, @"invalid transfer buffer length"); return NULL;
        }
        LoMetalBuffer *buffer = calloc(1, sizeof(*buffer));
        if (!buffer) { fail(error, LO_METAL_ALLOCATION, @"allocate buffer handle"); return NULL; }
        buffer->session = session;
        buffer->length = bytes;
        if (bytes) {
            // The second half is transfer staging. Both halves count in Bytes().
            buffer->storage = [session->device newBufferWithLength:bytes * 2 options:MTLResourceStorageModeShared];
            if (!buffer->storage) { fail(error, LO_METAL_ALLOCATION, @"allocate shared Metal storage"); free(buffer); return NULL; }
        }
        return buffer;
    }
}

uint64_t lo_metal_reserved(LoMetalBuffer *buffer) { return buffer->storage ? buffer->storage.allocatedSize : 0; }
const char *lo_metal_name(LoMetalSession *session) { return session->device.name.UTF8String; }
uint64_t lo_metal_registry(LoMetalSession *session) { return session->device.registryID; }
void lo_metal_release(LoMetalBuffer *buffer) {
    if (!buffer) return;
    [buffer->storage release];
    free(buffer);
}

static int copy_words(LoMetalBuffer *buffer, uint64_t source, uint64_t target, LoMetalError *error) {
    LoMetalSession *session = buffer->session;
    if (session->lost) { fail(error, LO_METAL_LOST, @"device session is unusable"); return 0; }
    if (!buffer->length) return 1;
    id<MTLCommandBuffer> command = [session->queue commandBuffer];
    id<MTLComputeCommandEncoder> encoder = [command computeCommandEncoder];
    if (!command || !encoder) { fail(error, LO_METAL_TRANSFER, @"create transfer command"); return 0; }
    [encoder setComputePipelineState:session->copy];
    [encoder setBuffer:buffer->storage offset:source atIndex:0];
    [encoder setBuffer:buffer->storage offset:target atIndex:1];
    NSUInteger count = buffer->length / 4;
    NSUInteger width = MIN(count, session->copy.maxTotalThreadsPerThreadgroup);
    [encoder dispatchThreads:MTLSizeMake(count, 1, 1) threadsPerThreadgroup:MTLSizeMake(width, 1, 1)];
    [encoder endEncoding];
    [command commit];
    [command waitUntilCompleted];
    if (command.status != MTLCommandBufferStatusCompleted) {
        // A failed submitted command invalidates this invocation's device session.
        session->lost = 1;
        fail(error, LO_METAL_LOST, [NSString stringWithFormat:@"transfer command failed: %@", command.error]);
        return 0;
    }
    return 1;
}

int lo_metal_upload(LoMetalBuffer *buffer, const void *values, uint64_t bytes, LoMetalError *error) {
    @autoreleasepool {
        if (bytes != buffer->length) { fail(error, LO_METAL_TRANSFER, @"upload length differs from allocation"); return 0; }
        if (buffer->session->lost) { fail(error, LO_METAL_LOST, @"device session is unusable"); return 0; }
        if (bytes) memcpy((char *)buffer->storage.contents + bytes, values, bytes);
        return copy_words(buffer, bytes, 0, error);
    }
}

int lo_metal_download(LoMetalBuffer *buffer, void *values, uint64_t bytes, LoMetalError *error) {
    @autoreleasepool {
        if (bytes != buffer->length) { fail(error, LO_METAL_TRANSFER, @"download length differs from allocation"); return 0; }
        if (!copy_words(buffer, 0, bytes, error)) return 0;
        if (bytes) memcpy(values, (char *)buffer->storage.contents + bytes, bytes);
        return 1;
    }
}
