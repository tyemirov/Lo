#ifndef LO_DISPATCH_DECLARATIONS_H
#define LO_DISPATCH_DECLARATIONS_H
#include <stddef.h>
typedef struct dispatch_data_s *dispatch_data_t;
typedef struct dispatch_queue_s *dispatch_queue_t;
typedef void (^dispatch_block_t)(void);
#define DISPATCH_DATA_DESTRUCTOR_DEFAULT ((dispatch_block_t)0)
extern dispatch_data_t dispatch_data_create(const void *, size_t, dispatch_queue_t, dispatch_block_t);
extern void dispatch_release(void *);
#endif
