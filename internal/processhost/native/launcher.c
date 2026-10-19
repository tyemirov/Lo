#include <errno.h>
#include <fcntl.h>
#include <sandbox.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <unistd.h>
#include "protocol.h"

/* Supplied by the separately reviewed IPC normalizer; no stub is linked. */
extern int lo_normalize_inherited_rights(void);

/* Trusted configuration is private and separate from untrusted command input. */

static int exact_read(int fd, void *destination, size_t size) {
    unsigned char *bytes = destination;
    while (size != 0) {
        ssize_t count = read(fd, bytes, size);
        if (count < 0 && errno == EINTR) continue;
        if (count <= 0) return -1;
        bytes += count;
        size -= (size_t)count;
    }
    return 0;
}

static void packet(unsigned char kind, unsigned char stage, int error) {
    unsigned char bytes[6] = {kind, stage, (unsigned char)((unsigned int)error >> 24), (unsigned char)((unsigned int)error >> 16), (unsigned char)((unsigned int)error >> 8), (unsigned char)error};
    size_t offset = 0;
    while (offset != sizeof(bytes)) {
        ssize_t count = write(STATUS, bytes + offset, sizeof(bytes) - offset);
        if (count < 0 && errno == EINTR) continue;
        if (count <= 0) _exit(90);
        offset += (size_t)count;
    }
}

static void fail(unsigned char stage, int error) {
    packet('F', stage, error);
    _exit(64 + stage);
}

static uint32_t number(const unsigned char *bytes) {
    return ((uint32_t)bytes[0] << 24) | ((uint32_t)bytes[1] << 16) | ((uint32_t)bytes[2] << 8) | bytes[3];
}

static int valid_utf8(const unsigned char *bytes, size_t size) {
    size_t offset = 0;
    while (offset < size) {
        unsigned char first = bytes[offset++];
        if (first == 0) return 0;
        if (first < 0x80) continue;
        unsigned int count, scalar;
        if (first >= 0xc2 && first <= 0xdf) { count = 1; scalar = first & 0x1f; }
        else if (first >= 0xe0 && first <= 0xef) { count = 2; scalar = first & 0x0f; }
        else if (first >= 0xf0 && first <= 0xf4) { count = 3; scalar = first & 7; }
        else return 0;
        if (size - offset < count) return 0;
        for (unsigned int index = 0; index < count; index++) {
            unsigned char next = bytes[offset++];
            if ((next & 0xc0) != 0x80) return 0;
            scalar = (scalar << 6) | (next & 0x3f);
        }
        if ((count == 1 && scalar < 0x80) || (count == 2 && scalar < 0x800) || (count == 3 && scalar < 0x10000) || (scalar >= 0xd800 && scalar <= 0xdfff) || scalar > 0x10ffff) return 0;
    }
    return 1;
}

static char *entry(const unsigned char *body, size_t size, size_t *offset, char storage[MAX_ENTRY + 1]) {
    if (size - *offset < 4) fail(2, EINVAL);
    uint32_t length = number(body + *offset);
    *offset += 4;
    if (length > MAX_ENTRY || size - *offset < length || !valid_utf8(body + *offset, length)) fail(2, EINVAL);
    memcpy(storage, body + *offset, length);
    storage[length] = '\0';
    *offset += length;
    return storage;
}

static void configuration_entry(const unsigned char *body, size_t size, size_t *offset, char *storage, size_t maximum) {
    if (size - *offset < 4) fail(1, EINVAL);
    uint32_t length = number(body + *offset);
    *offset += 4;
    if (length == 0 || length > maximum || size - *offset < length || !valid_utf8(body + *offset, length)) fail(1, EINVAL);
    memcpy(storage, body + *offset, length);
    storage[length] = '\0';
    *offset += length;
}

static int authorized_directory(const char *directory, char roots[MAX_COUNT][MAX_ENTRY + 1], uint32_t count) {
    for (uint32_t index = 0; index < count; index++) {
        size_t size = strlen(roots[index]);
        if (strcmp(roots[index], "/") == 0 && directory[0] == '/') return 1;
        if (strncmp(directory, roots[index], size) == 0 && (directory[size] == '\0' || directory[size] == '/')) return 1;
    }
    return 0;
}

int main(int argc, char **argv, char **host_environment) {
    (void)argv;
    if (argc != 1) fail(2, EINVAL);
    struct rlimit core = {0, 0}, descriptors = {64, 64};
    if (setrlimit(RLIMIT_CORE, &core) != 0 || setrlimit(RLIMIT_NOFILE, &descriptors) != 0) fail(1, errno);
    if (fcntl(STATUS, F_SETFD, FD_CLOEXEC) != 0 || fcntl(PERMIT, F_SETFD, FD_CLOEXEC) != 0) fail(5, errno);
    if (lo_normalize_inherited_rights() != 0) fail(1, EPERM);
    unsigned char configuration_header[16], configuration[MAX_CONFIGURATION], extra;
    if (exact_read(CONFIGURATION, configuration_header, sizeof(configuration_header)) != 0 || memcmp(configuration_header, "LOC1", 4) != 0) fail(1, EINVAL);
    uint32_t configuration_size = number(configuration_header + 4), command_count = number(configuration_header + 8), root_count = number(configuration_header + 12);
    if (configuration_size > MAX_CONFIGURATION || command_count == 0 || command_count > MAX_COUNT || root_count == 0 || root_count > MAX_COUNT || exact_read(CONFIGURATION, configuration, configuration_size) != 0) fail(1, EINVAL);
    ssize_t tail;
    do { tail = read(CONFIGURATION, &extra, 1); } while (tail < 0 && errno == EINTR);
    if (tail != 0 || close(CONFIGURATION) != 0) fail(1, EINVAL);
    size_t configuration_offset = 0;
    char profile[MAX_PROFILE + 1], host_directory[MAX_ENTRY + 1], commands[MAX_COUNT][MAX_ENTRY + 1], roots[MAX_COUNT][MAX_ENTRY + 1];
    configuration_entry(configuration, configuration_size, &configuration_offset, profile, MAX_PROFILE);
    configuration_entry(configuration, configuration_size, &configuration_offset, host_directory, MAX_ENTRY);
    for (uint32_t index = 0; index < command_count; index++) configuration_entry(configuration, configuration_size, &configuration_offset, commands[index], MAX_ENTRY);
    for (uint32_t index = 0; index < root_count; index++) configuration_entry(configuration, configuration_size, &configuration_offset, roots[index], MAX_ENTRY);
    if (configuration_offset != configuration_size || host_environment[0] == NULL || host_environment[1] == NULL || host_environment[2] != NULL || strcmp(host_environment[0], "CI=1") != 0 || strncmp(host_environment[1], "TMPDIR=", 7) != 0 || strcmp(host_environment[1] + 7, host_directory) != 0) fail(2, EINVAL);
    char *sandbox_error = NULL;
    if (sandbox_init(profile, 0, &sandbox_error) != 0) {
        if (sandbox_error != NULL) sandbox_free_error(sandbox_error);
        fail(1, EPERM);
    }
    unsigned char header[16], body[MAX_BODY];
    if (exact_read(FRAME, header, sizeof(header)) != 0 || memcmp(header, "LOP1", 4) != 0) fail(2, EINVAL);
    uint32_t size = number(header + 4), argument_count = number(header + 8), environment_count = number(header + 12);
    if (size > MAX_BODY || argument_count == 0 || argument_count > MAX_COUNT || environment_count > MAX_COUNT || exact_read(FRAME, body, size) != 0) fail(2, EINVAL);
    do { tail = read(FRAME, &extra, 1); } while (tail < 0 && errno == EINTR);
    if (tail != 0 || close(FRAME) != 0) fail(2, EINVAL);
    size_t offset = 0;
    char executable[MAX_ENTRY + 1], directory[MAX_ENTRY + 1];
    entry(body, size, &offset, executable);
    entry(body, size, &offset, directory);
    int command_allowed = 0;
    for (uint32_t index = 0; index < command_count; index++) if (strcmp(executable, commands[index]) == 0) command_allowed = 1;
    if (!command_allowed || !authorized_directory(directory, roots, root_count)) fail(2, EINVAL);
    char argument_storage[MAX_COUNT][MAX_ENTRY + 1], environment_storage[MAX_COUNT][MAX_ENTRY + 1];
    char *arguments[MAX_COUNT + 1], *environment[MAX_COUNT + 1];
    for (uint32_t index = 0; index < argument_count; index++) arguments[index] = entry(body, size, &offset, argument_storage[index]);
    arguments[argument_count] = NULL;
    for (uint32_t index = 0; index < environment_count; index++) {
        environment[index] = entry(body, size, &offset, environment_storage[index]);
        char *equal = strchr(environment[index], '=');
        if (equal == NULL || equal == environment[index]) fail(2, EINVAL);
        size_t name_length = (size_t)(equal - environment[index]);
        for (uint32_t prior = 0; prior < index; prior++) if (strncmp(environment[prior], environment[index], name_length) == 0 && environment[prior][name_length] == '=') fail(2, EINVAL);
    }
    environment[environment_count] = NULL;
    if (offset != size || strcmp(arguments[0], executable) != 0 || chdir(directory) != 0) fail(2, EINVAL);
    packet('R', 0, 0);
    unsigned char permit;
    if (exact_read(PERMIT, &permit, 1) != 0 || permit != 'P') fail(3, EINVAL);
    do { tail = read(PERMIT, &extra, 1); } while (tail < 0 && errno == EINTR);
    if (tail != 0 || close(PERMIT) != 0) fail(3, EINVAL);
    execve(executable, arguments, environment);
    fail(4, errno);
}
