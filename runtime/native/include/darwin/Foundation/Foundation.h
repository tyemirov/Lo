#ifndef LO_FOUNDATION_DECLARATIONS_H
#define LO_FOUNDATION_DECLARATIONS_H
/* The Darwin arm64 declarations used by Lo's fixed Metal bridge. */
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
extern int snprintf(char *, size_t, const char *, ...);
typedef unsigned long NSUInteger;
typedef long NSInteger;
typedef _Bool BOOL;
#define nil ((id)0)
#define MIN(a,b) ((a)<(b)?(a):(b))
@protocol NSObject
- (oneway void)release;
- (id)copy;
@end
__attribute__((objc_root_class))
@interface NSObject <NSObject>
@end
@interface NSString : NSObject
@property(readonly) const char *UTF8String;
+ (instancetype)stringWithFormat:(NSString *)format, ...;
@end
@interface NSError : NSObject
@end
@interface NSProcessInfo : NSObject
+ (NSProcessInfo *)processInfo;
@property(readonly) NSString *operatingSystemVersionString;
@end
#endif
