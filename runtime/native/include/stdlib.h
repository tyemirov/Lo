#ifndef LO_STDLIB_H
#define LO_STDLIB_H
#include <stddef.h>
extern void abort(void) __attribute__((noreturn));
extern void *malloc(size_t);
extern void *calloc(size_t, size_t);
extern void *realloc(void *, size_t);
extern void free(void *);
#endif
