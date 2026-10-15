#include <metal_stdlib>
using namespace metal;

kernel void lo_copy_words(device const uint *source [[buffer(0)]],
                          device uint *target [[buffer(1)]],
                          uint index [[thread_position_in_grid]]) {
    target[index] = source[index];
}
