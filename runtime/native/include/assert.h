#undef assert
#ifdef NDEBUG
#define assert(expression) ((void)0)
#else
#include <stdlib.h>
#define assert(expression) ((expression) ? (void)0 : abort())
#endif
