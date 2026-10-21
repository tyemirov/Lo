#ifndef LO_TEST_METAL_EXECUTOR_H
#define LO_TEST_METAL_EXECUTOR_H
typedef unsigned uint;
#define __METAL_VERSION__ 1
#include "../../internal/device/metal_types.h"
#undef __METAL_VERSION__
_Static_assert(sizeof(LoMetalOperation)==160,"descriptor size");
static float model_float(unsigned bits) {
    union { unsigned bits; float value; } word = { .bits = bits };
    return word.value;
}
static unsigned model_bits(float value) {
    union { unsigned bits; float value; } word = { .value = value };
    return word.bits;
}
static float model_round(double value) {
    float result = (float)value;
    return (model_bits(result) & 0x7f800000u) == 0 ? 0.0f : result;
}
static int model_finite(float value) {
    return (model_bits(value) & 0x7f800000u) != 0x7f800000u;
}
static void model_coordinates(unsigned index, const unsigned *dims, unsigned rank, unsigned *out) {
    for (unsigned axis = rank; axis; --axis) {
        out[axis-1] = index % dims[axis-1];
        index /= dims[axis-1];
    }
}
static unsigned model_index(const unsigned *coordinates, unsigned rank, const unsigned *dims, unsigned input_rank) {
    unsigned result = 0, shift = rank-input_rank;
    for (unsigned axis = 0; axis < input_rank; ++axis) {
        unsigned coordinate = dims[axis] == 1 ? 0 : coordinates[axis+shift];
        result = result*dims[axis]+coordinate;
    }
    return result;
}
/* Independent descriptor interpreter. It never calls the Lo tensor evaluator. */
static int model_execute(const LoMetalOperation *op, const float *a, const float *b, float *out, unsigned *status) {
    *status = ~0u;
    if (op->kind < LO_ADD || op->kind > LO_SQRT_GRAD) return 0;
    for (unsigned index=0; index<op->count; ++index) {
        unsigned coordinates[8]={0};
        model_coordinates(index,op->output,op->rank,coordinates);
        float result=0.0f;
        unsigned failure=0;
        switch (op->kind) {
        case LO_FILL:
            out[index]=model_float(op->scalar); continue;
        case LO_BROADCAST:
            out[index]=a[model_index(coordinates,op->rank,op->a,op->aRank)]; continue;
        case LO_TRANSPOSE: {
            unsigned original[8]={0};
            for(unsigned axis=0;axis<op->rank;++axis) original[op->axes[axis]]=coordinates[axis];
            out[index]=a[model_index(original,op->aRank,op->a,op->aRank)]; continue;
        }
        case LO_SQRT_GRAD:
            if(model_round((double)a[index])==0.0f) failure=LO_NUMERIC_DERIVATIVE;
            break;
        case LO_RELU_GRAD:
            if(model_round((double)a[index])>0.0f) result=model_round((double)b[index]);
            break;
        case LO_SUM: case LO_MEAN: {
            unsigned reduced[8]={0}, reduced_rank=0;
            for(unsigned axis=0;axis<op->aRank;++axis) if(op->axes[axis]) reduced[reduced_rank++]=op->a[axis];
            for(unsigned r=0;r<op->region;++r) {
                unsigned rc[8]={0}, source[8]={0}, kept=0, removed=0;
                model_coordinates(r,reduced,reduced_rank,rc);
                for(unsigned axis=0;axis<op->aRank;++axis) source[axis]=op->axes[axis]?rc[removed++]:coordinates[kept++];
                float value=model_round((double)a[model_index(source,op->aRank,op->a,op->aRank)]);
                result=model_round((double)result+(double)value);
                if(!model_finite(result)){failure=LO_NUMERIC_NONFINITE;break;}
            }
            if(!failure && op->kind==LO_MEAN) result=model_round((double)result/(double)(float)op->region);
            break;
        }
        case LO_MATMUL:
            for(unsigned k=0;k<op->a[1];++k) {
                float left=model_round((double)a[coordinates[0]*op->a[1]+k]);
                float right=model_round((double)b[k*op->b[1]+coordinates[1]]);
                float product=model_round((double)left*(double)right);
                if(!model_finite(product)){failure=LO_NUMERIC_NONFINITE;break;}
                result=model_round((double)result+(double)product);
                if(!model_finite(result)){failure=LO_NUMERIC_NONFINITE;break;}
            }
            break;
        default: {
            double x=(double)model_round((double)a[model_index(coordinates,op->rank,op->a,op->aRank)]), y=0.0;
            if(op->kind<=LO_DIV) y=(double)model_round((double)b[model_index(coordinates,op->rank,op->b,op->bRank)]);
            switch(op->kind) {
            case LO_ADD: result=model_round(x+y);break;
            case LO_SUB: result=model_round(x-y);break;
            case LO_MUL: result=model_round(x*y);break;
            case LO_DIV:
                if(y==0.0) failure=LO_NUMERIC_ZERO;
                else result=model_round(x/y);
                break;
            case LO_SQRT:
                if(x<0.0) failure=LO_NUMERIC_DOMAIN;
                else result=model_round(__builtin_sqrt(x));
                break;
            case LO_RELU: if(x>0.0) result=(float)x;break;
            }
        }
        }
        if(!failure && !model_finite(result)) failure=LO_NUMERIC_NONFINITE;
        if(failure){*status=(index<<3)|failure;return 1;}
        if(op->kind!=LO_SQRT_GRAD) out[index]=result;
    }
    return 1;
}
#endif
