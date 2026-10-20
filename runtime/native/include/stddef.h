#ifndef LO_STDDEF_H
#define LO_STDDEF_H
typedef __SIZE_TYPE__ size_t;
typedef __PTRDIFF_TYPE__ ptrdiff_t;
#define NULL ((void *)0)
#define offsetof(type, field) __builtin_offsetof(type, field)
#endif
