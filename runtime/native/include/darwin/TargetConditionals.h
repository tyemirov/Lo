#ifndef LO_TARGET_CONDITIONALS_H
#define LO_TARGET_CONDITIONALS_H
#if !defined(__APPLE__) || !defined(__aarch64__)
#error Lo Darwin headers require the selected Darwin arm64 target
#endif
#define TARGET_OS_OSX 1
#define TARGET_OS_IPHONE 0
#define TARGET_OS_SIMULATOR 0
#endif
