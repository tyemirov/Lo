#include <errno.h>
#include <fcntl.h>
#include <sandbox.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include "fixture_paths.h"

extern char **environ;
enum { FRAME_LIMIT = 4096, ENTRY_LIMIT = 1024, ENTRY_COUNT = 16, INVALID_FRAME = 65 };
#define MARKER(phase) do { \
    static const char marker[] = "LO_PHASE " phase "\n"; \
    if (write(STDERR_FILENO, marker, sizeof(marker) - 1) != sizeof(marker) - 1) _exit(90); \
} while (0)

static bool read_exact(int descriptor, unsigned char *bytes, size_t length) {
    while (length > 0) {
        ssize_t count = read(descriptor, bytes, length);
        if (count < 0 && errno == EINTR) continue;
        if (count <= 0) return false;
        bytes += count;
        length -= (size_t)count;
    }
    return true;
}

static uint32_t big_endian(const unsigned char *bytes) {
    return ((uint32_t)bytes[0] << 24) | ((uint32_t)bytes[1] << 16) |
           ((uint32_t)bytes[2] << 8) | (uint32_t)bytes[3];
}

static bool utf8(const unsigned char *bytes, size_t length) {
    size_t offset = 0;
    while (offset < length) {
        uint32_t code = bytes[offset++];
        if (code == 0) return false;
        if (code < 0x80) continue;
        unsigned continuation;
        uint32_t minimum;
        if (code >= 0xc2 && code <= 0xdf) { continuation = 1; minimum = 0x80; code &= 0x1f; }
        else if (code >= 0xe0 && code <= 0xef) { continuation = 2; minimum = 0x800; code &= 0x0f; }
        else if (code >= 0xf0 && code <= 0xf4) { continuation = 3; minimum = 0x10000; code &= 0x07; }
        else return false;
        if (length - offset < continuation) return false;
        for (unsigned index = 0; index < continuation; index++) {
            unsigned char next = bytes[offset++];
            if ((next & 0xc0) != 0x80) return false;
            code = (code << 6) | (next & 0x3f);
        }
        if (code < minimum || code > 0x10ffff || (code >= 0xd800 && code <= 0xdfff)) return false;
    }
    return true;
}

static bool receive_environment(int descriptor, char storage[ENTRY_COUNT][ENTRY_LIMIT + 1], char *entries[ENTRY_COUNT + 1]) {
    unsigned char header[12], body[FRAME_LIMIT];
    if (!read_exact(descriptor, header, sizeof(header)) || memcmp(header, "LOE1", 4) != 0) return false;
    uint32_t size = big_endian(header + 4), count = big_endian(header + 8);
    if (size > FRAME_LIMIT || count > ENTRY_COUNT || !read_exact(descriptor, body, size)) return false;
    MARKER("launcher.before-environment-eof");
    unsigned char extra;
    ssize_t end;
    do { end = read(descriptor, &extra, 1); } while (end < 0 && errno == EINTR);
    if (end != 0) return false;
    size_t offset = 0, names[ENTRY_COUNT];
    for (uint32_t index = 0; index < count; index++) {
        if (size - offset < 4) return false;
        uint32_t length = big_endian(body + offset);
        offset += 4;
        if (length == 0 || length > ENTRY_LIMIT || length > size - offset || !utf8(body + offset, length)) return false;
        const unsigned char *equals = memchr(body + offset, '=', length);
        if (equals == NULL || equals == body + offset) return false;
        size_t name = (size_t)(equals - (body + offset));
        for (uint32_t previous = 0; previous < index; previous++) {
            if (name == names[previous] && memcmp(storage[previous], body + offset, name) == 0) return false;
        }
        memcpy(storage[index], body + offset, length);
        storage[index][length] = '\0';
        names[index] = name;
        entries[index] = storage[index];
        offset += length;
    }
    entries[count] = NULL;
    return offset == size;
}

int main(int argc, char **argv) {
    MARKER("launcher.entered");
    if (argc != 3) return 64;
    char *end = NULL;
    errno = 0;
    long descriptor = strtol(argv[1], &end, 10);
    if (errno != 0 || end == argv[1] || *end != '\0' || descriptor < 3 || descriptor >= LO_FD_LIMIT) return 64;
    if (strcmp(argv[2], "loaded") != 0 && strcmp(argv[2], "empty") != 0 &&
        strcmp(argv[2], "values") != 0 && strcmp(argv[2], "invalid") != 0) return 64;
    int count = 0;
    for (char **entry = environ; *entry != NULL; entry++) count++;
    const char *ci = getenv("CI"), *temporary = getenv("TMPDIR");
    if (count != 2 || ci == NULL || strcmp(ci, "1") != 0 || temporary == NULL || strcmp(temporary, LO_ROOT) != 0) return 66;
    MARKER("launcher.environment-exact");
    char *sandbox_error = NULL;
    MARKER("launcher.before-sandbox-init");
    if (sandbox_init(LO_PROFILE, 0, &sandbox_error) != 0) {
        if (sandbox_error != NULL) sandbox_free_error(sandbox_error);
        if (close((int)descriptor) != 0) return 72;
        MARKER("launcher.sandbox-init-failed");
        return 70;
    }
    MARKER("launcher.after-sandbox-init");
    char storage[ENTRY_COUNT][ENTRY_LIMIT + 1];
    char *environment[ENTRY_COUNT + 1];
    if (!receive_environment((int)descriptor, storage, environment)) {
        if (close((int)descriptor) != 0) return 72;
        MARKER("launcher.invalid-environment");
        return INVALID_FRAME;
    }
    MARKER("launcher.environment-received");
    if (close((int)descriptor) != 0) return 72;
    MARKER("launcher.control-closed");
    char *arguments[] = {LO_TARGET, argv[2], "fixed-argument", NULL};
    MARKER("launcher.before-execve");
    execve(LO_TARGET, arguments, environment);
    MARKER("launcher.execve-returned");
    return 71;
}
