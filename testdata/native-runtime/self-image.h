#ifndef LO_TEST_SELF_IMAGE_H
#define LO_TEST_SELF_IMAGE_H

enum { LO_TEST_SELF_IMAGE_CAPACITY = 4096 };
#ifndef __APPLE__
extern long readlink(const char *, char *, __SIZE_TYPE__);
#endif

static const char *lo_test_self_image(const char *argument, char *buffer) {
#ifdef __APPLE__
    (void)buffer;
    return argument;
#else
    (void)argument;
    long length = readlink("/proc/self/exe", buffer, LO_TEST_SELF_IMAGE_CAPACITY);
    if (length <= 0 || length >= LO_TEST_SELF_IMAGE_CAPACITY) return 0;
    buffer[length] = 0;
    return buffer;
#endif
}

#endif
