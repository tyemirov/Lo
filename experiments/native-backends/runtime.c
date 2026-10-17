#include <stdint.h>
#include <stdio.h>
extern int64_t lo_probe(int64_t, int32_t *);
int main(void) {
    int32_t status = -1;
    int64_t value = lo_probe(7, &status);
    printf("%lld %d\n", (long long)value, status);
    value = lo_probe(-1, &status);
    printf("%lld %d\n", (long long)value, status);
    return 0;
}
