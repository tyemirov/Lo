#ifndef LO_METAL_DECLARATIONS_H
#define LO_METAL_DECLARATIONS_H
#import <Foundation/Foundation.h>
#include <dispatch/dispatch.h>
typedef struct { NSUInteger width, height, depth; } MTLSize;
static inline MTLSize MTLSizeMake(NSUInteger width, NSUInteger height, NSUInteger depth) {
    return (MTLSize){width,height,depth};
}
typedef NSUInteger MTLResourceOptions;
typedef NSInteger MTLGPUFamily;
typedef NSUInteger MTLCommandBufferStatus;
enum { MTLGPUFamilyApple7 = 1007 };
enum { MTLResourceStorageModeShared = 0 };
enum { MTLCommandBufferStatusCompleted = 4 };
@protocol MTLBuffer, MTLFunction, MTLLibrary, MTLComputePipelineState;
@protocol MTLCommandBuffer, MTLCommandQueue, MTLComputeCommandEncoder;
@protocol MTLDevice <NSObject>
- (BOOL)supportsFamily:(MTLGPUFamily)family;
@property(readonly) BOOL hasUnifiedMemory;
@property(readonly) NSUInteger maxBufferLength;
@property(readonly) NSString *name;
@property(readonly) uint64_t registryID;
- (id<MTLCommandQueue>)newCommandQueue;
- (id<MTLLibrary>)newLibraryWithData:(dispatch_data_t)data error:(NSError **)error;
- (id<MTLComputePipelineState>)newComputePipelineStateWithFunction:(id<MTLFunction>)function error:(NSError **)error;
- (id<MTLBuffer>)newBufferWithLength:(NSUInteger)length options:(MTLResourceOptions)options;
@end
@protocol MTLBuffer <NSObject>
@property(readonly) NSUInteger allocatedSize;
@property(readonly) void *contents;
@end
@protocol MTLLibrary <NSObject>
- (id<MTLFunction>)newFunctionWithName:(NSString *)name;
@end
@protocol MTLFunction <NSObject>
@end
@protocol MTLComputePipelineState <NSObject>
@property(readonly) NSUInteger maxTotalThreadsPerThreadgroup;
@end
@protocol MTLCommandQueue <NSObject>
- (id<MTLCommandBuffer>)commandBuffer;
@end
@protocol MTLCommandBuffer <NSObject>
- (id<MTLComputeCommandEncoder>)computeCommandEncoder;
- (void)commit;
- (void)waitUntilCompleted;
@property(readonly) MTLCommandBufferStatus status;
@property(readonly) NSError *error;
@end
@protocol MTLComputeCommandEncoder <NSObject>
- (void)setComputePipelineState:(id<MTLComputePipelineState>)state;
- (void)setBuffer:(id<MTLBuffer>)buffer offset:(NSUInteger)offset atIndex:(NSUInteger)index;
- (void)setBytes:(const void *)bytes length:(NSUInteger)length atIndex:(NSUInteger)index;
- (void)dispatchThreads:(MTLSize)threads threadsPerThreadgroup:(MTLSize)group;
- (void)endEncoding;
@end
extern id<MTLDevice> MTLCreateSystemDefaultDevice(void);
_Static_assert(sizeof(MTLSize)==24,"Darwin arm64 MTLSize ABI");
_Static_assert(sizeof(NSUInteger)==8,"Darwin arm64 NSUInteger ABI");
#endif
