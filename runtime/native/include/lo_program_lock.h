#ifndef LO_PROGRAM_LOCK_H
#define LO_PROGRAM_LOCK_H
#include <stddef.h>
#include <stdint.h>
typedef struct Context Context;
/* Register once on the owner thread before evaluation or session sharing. */
/* Paths require UTF-8. Input storage is borrowed only during registration. */
/* Return 1 on success and 0 on rejection. Entry paths require absolute spelling. */
int lo_context_set_program_entry(Context *, const uint8_t *, size_t);
/* Grant existing files only. The final argument selects entry lock authority. */
/* Grant paths require clean absolute spelling. Count cannot exceed 128. */
int lo_context_set_lock_files(Context *, const char *const *, size_t, int);
/* Close after failed or cancelled evaluation, or without terminal output. */
/* Return 1 only when cleanup succeeds. Retained children join before lease release. */
/* Terminal Program.Exit output requires lo_context_complete instead. */
int lo_context_close(Context *);
#endif
