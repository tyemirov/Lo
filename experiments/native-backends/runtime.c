typedef long long int64_t;
typedef int int32_t;
extern int printf(const char *, ...);
extern void *malloc(unsigned long);
extern void free(void *);
extern int64_t lo_probe(int64_t, int32_t *);
static int runtime_calls;
static int64_t callback(int64_t value) { return value + 2; }
int64_t lo_runtime_adjust(int64_t value, int32_t *status) {
    runtime_calls++;
    int64_t *owned = malloc(sizeof *owned);
    if (!owned) { *status = 9; return 0; }
    *owned = callback(value);
    int64_t result = *owned;
    free(owned);
    *status = 0;
    return result;
}
int main(void) {
    int32_t status = -1;
    int64_t value = lo_probe(7, &status);
    printf("%lld %d\n", value, status);
    value = lo_probe(-1, &status);
    printf("%lld %d\n", value, status);
    printf("runtime_calls %d\n", runtime_calls);
    return 0;
}
