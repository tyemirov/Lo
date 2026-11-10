#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

static int write_all(int descriptor, const unsigned char *data, size_t length) {
    while (length != 0) {
        ssize_t count = write(descriptor, data, length);
        if (count < 0 && errno == EINTR) continue;
        if (count <= 0) return 1;
        data += count;
        length -= (size_t)count;
    }
    return 0;
}

static int mark(const char *path) {
    FILE *file = fopen(path, "wb");
    if (file == NULL) return 1;
    int written = fputs("started", file);
    int closed = fclose(file);
    return written < 0 || closed != 0;
}

int main(int argc, char **argv) {
    if (argc < 2) return 64;
    if (strcmp(argv[1], "output") == 0) {
        const unsigned char output[] = {'o', 'u', 't', 0, 255, '\n'};
        const unsigned char diagnostic[] = {'e', 'r', 'r', '\r', '\n'};
        return write_all(1, output, sizeof(output)) || write_all(2, diagnostic, sizeof(diagnostic));
    }
    if (strcmp(argv[1], "mark") == 0 && argc == 3) return mark(argv[2]);
    if (strcmp(argv[1], "sleep") == 0 && argc == 3) {
        unsigned long milliseconds = strtoul(argv[2], NULL, 10);
        struct timespec remaining = {(time_t)(milliseconds / 1000), (long)(milliseconds % 1000) * 1000000};
        while (nanosleep(&remaining, &remaining) != 0) {
            if (errno != EINTR) return 65;
        }
        return 0;
    }
    if (strcmp(argv[1], "repeat") == 0 && argc == 3) {
        unsigned long remaining = strtoul(argv[2], NULL, 10);
        unsigned char data[4096];
        memset(data, 'x', sizeof(data));
        while (remaining != 0) {
            size_t count = remaining < sizeof(data) ? (size_t)remaining : sizeof(data);
            if (write_all(1, data, count)) return 66;
            remaining -= count;
        }
        return 0;
    }
    return 64;
}
