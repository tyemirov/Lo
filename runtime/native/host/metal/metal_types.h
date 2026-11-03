#ifndef LO_METAL_TYPES_H
#define LO_METAL_TYPES_H
#ifdef __METAL_VERSION__
typedef uint LoMetalWord;
#else
#include <stdint.h>
typedef uint32_t LoMetalWord;
#endif

enum { LO_ADD=1, LO_SUB, LO_MUL, LO_DIV, LO_SQRT, LO_RELU, LO_BROADCAST,
       LO_TRANSPOSE, LO_SUM, LO_MEAN, LO_MATMUL, LO_FILL, LO_RELU_GRAD, LO_SQRT_GRAD };
enum { LO_NUMERIC_NONFINITE=1, LO_NUMERIC_ZERO, LO_NUMERIC_DOMAIN, LO_NUMERIC_DERIVATIVE };
typedef struct {
    LoMetalWord kind, count, inputCount, region, aRank, bRank, rank, scalar;
    LoMetalWord a[8], b[8], output[8], axes[8];
} LoMetalOperation;
#endif
