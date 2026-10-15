#include <metal_stdlib>
using namespace metal;

kernel void lo_copy_words(device const uint *source [[buffer(0)]],
                          device uint *target [[buffer(1)]],
                          uint index [[thread_position_in_grid]]) {
    target[index] = source[index];
}

#include "metal_types.h"

// Bitwise classification avoids implementation-dependent subnormal comparisons.
float lo_normal(float value) {
    return (as_type<uint>(value) & 0x7f800000u) == 0 ? 0.0f : value;
}
bool lo_finite(float value) { return (as_type<uint>(value) & 0x7f800000u) != 0x7f800000u; }
void lo_failure(device atomic_uint *status, uint index, uint kind) {
    atomic_fetch_min_explicit(status, (index << 3) | kind, memory_order_relaxed);
}
uint lo_broadcast_index(uint index, constant LoMetalOperation &op, constant uint *dims, uint rank) {
    uint result=0, stride=1;
    for (int i=int(op.rank)-1, j=int(rank)-1; j>=0; --i,--j) {
        uint coordinate=index%op.output[i]; index/=op.output[i];
        if(dims[j]!=1) result+=coordinate*stride;
        stride*=dims[j];
    }
    return result;
}

kernel void lo_compute(device const float *a [[buffer(0)]], device const float *b [[buffer(1)]],
                       device float *out [[buffer(2)]], device atomic_uint *status [[buffer(3)]],
                       constant LoMetalOperation &op [[buffer(4)]], uint index [[thread_position_in_grid]]) {
    float result=0.0f;
    switch(op.kind) {
    case LO_FILL: reinterpret_cast<device uint *>(out)[index]=op.scalar; return;
    case LO_BROADCAST: reinterpret_cast<device uint *>(out)[index]=reinterpret_cast<device const uint *>(a)[lo_broadcast_index(index,op,op.a,op.aRank)]; return;
    case LO_TRANSPOSE: {
        uint strides[8],stride=1,source=0,rest=index;
        for(int axis=int(op.aRank)-1;axis>=0;--axis){strides[axis]=stride;stride*=op.a[axis];}
        for(int axis=int(op.rank)-1;axis>=0;--axis){source+=(rest%op.output[axis])*strides[op.axes[axis]];rest/=op.output[axis];}
        reinterpret_cast<device uint *>(out)[index]=reinterpret_cast<device const uint *>(a)[source];return;
    }
    case LO_SQRT_GRAD:
        if(lo_normal(a[index])==0.0f)lo_failure(status,index,LO_NUMERIC_DERIVATIVE);
        return;
    case LO_RELU_GRAD:
        if(lo_normal(a[index])>0.0f)result=lo_normal(b[index]);
        break;
    case LO_SUM: case LO_MEAN: {
        // Each output traverses its source coordinates in row-major order.
        for(uint r=0;r<op.region;++r){
            uint selected=r,kept=index,source=0,stride=1;
            for(int axis=int(op.aRank)-1;axis>=0;--axis){
                uint coordinate;
                if(op.axes[axis]){coordinate=selected%op.a[axis];selected/=op.a[axis];}
                else{coordinate=kept%op.a[axis];kept/=op.a[axis];}
                source+=coordinate*stride;stride*=op.a[axis];
            }
            result=lo_normal(result+lo_normal(a[source]));
            if(!lo_finite(result)){lo_failure(status,index,LO_NUMERIC_NONFINITE);return;}
        }
        if(op.kind==LO_MEAN)result=lo_normal(result/float(op.region));
        break;
    }
    case LO_MATMUL:
        for(uint k=0;k<op.a[1];++k){
            float product=lo_normal(lo_normal(a[(index/op.b[1])*op.a[1]+k])*lo_normal(b[k*op.b[1]+index%op.b[1]]));
            if(!lo_finite(product)){lo_failure(status,index,LO_NUMERIC_NONFINITE);return;}
            result=lo_normal(result+product);
            if(!lo_finite(result)){lo_failure(status,index,LO_NUMERIC_NONFINITE);return;}
        }
        break;
    default: {
        float x=lo_normal(a[lo_broadcast_index(index,op,op.a,op.aRank)]),y=0.0f;
        if(op.kind<=LO_DIV)y=lo_normal(b[lo_broadcast_index(index,op,op.b,op.bRank)]);
        switch(op.kind){
        case LO_ADD:result=lo_normal(x+y);break;
        case LO_SUB:result=lo_normal(x-y);break;
        case LO_MUL:result=lo_normal(x*y);break;
        case LO_DIV:
            if(y==0.0f){lo_failure(status,index,LO_NUMERIC_ZERO);return;}
            result=lo_normal(x/y);break;
        case LO_SQRT:
            if(x<0.0f){lo_failure(status,index,LO_NUMERIC_DOMAIN);return;}
            result=lo_normal(sqrt(x));break;
        case LO_RELU:if(x>0.0f)result=x;break;
        }
    }
    }
    if(!lo_finite(result)){lo_failure(status,index,LO_NUMERIC_NONFINITE);return;}
    out[index]=result;
}
