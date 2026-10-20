#ifndef LO_STDINT_H
#define LO_STDINT_H
typedef __INT8_TYPE__ int8_t;
typedef __UINT8_TYPE__ uint8_t;
typedef __INT16_TYPE__ int16_t;
typedef __UINT16_TYPE__ uint16_t;
typedef __INT32_TYPE__ int32_t;
typedef __UINT32_TYPE__ uint32_t;
typedef __INT64_TYPE__ int64_t;
typedef __UINT64_TYPE__ uint64_t;
typedef __INTPTR_TYPE__ intptr_t;
typedef __UINTPTR_TYPE__ uintptr_t;
#define UINT8_MAX __UINT8_MAX__
#define UINT16_MAX __UINT16_MAX__
#define UINT32_MAX __UINT32_MAX__
#define UINT64_MAX __UINT64_MAX__
#define SIZE_MAX __SIZE_MAX__
#define INT32_MAX __INT32_MAX__
#define INT64_MAX __INT64_MAX__
#define UINT32_C(x) x##U
#define UINT8_C(x) x
#define UINT64_C(x) x##ULL
#define INT32_C(x) x
#define INT64_C(x) x##LL
#endif
