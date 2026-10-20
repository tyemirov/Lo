#include "contract.h"

/* Public C ABI declarations. No developer SDK headers are used. */
extern int printf(const char *, ...);
typedef unsigned long thread_word;
extern int pthread_create(thread_word *, const void *, void *(*)(void *), void *);
extern int pthread_join(thread_word, void **);

static lo_context *owner_context;
static const lo_value *shared;
static int callbacks;
static lo_result foreign, owned;

static void add_two(lo_context *context, const lo_value *input, lo_result *output) {
    (void)context;
    callbacks++;
    output->status = LO_OK;
    output->code = 0;
    output->value = lo_value_new(lo_value_read(input) + 2);
}

static void fail_callback(lo_context *context, const lo_value *input, lo_result *output) {
    (void)context;
    (void)input;
    callbacks++;
    output->status = LO_ERROR;
    output->code = 17;
    output->value = lo_value_new(-17);
}

static void *worker(void *argument) {
    (void)argument;
    lo_apply(owner_context, shared, add_two, &foreign);
    lo_context *own_context = lo_context_new(1);
    lo_apply(own_context, shared, add_two, &owned);
    lo_context_drop(own_context);
    lo_value_release(shared);
    return 0;
}

int main(void) {
    owner_context = lo_context_new(1);
    shared = lo_value_new(7);
    lo_value_retain(shared);
    thread_word thread;
    if (pthread_create(&thread, 0, worker, 0) != 0 || pthread_join(thread, 0) != 0) return 90;
    printf("thread %u %u %u %lld\n", foreign.status, foreign.code, owned.status, lo_value_read(owned.value));
    lo_value_release(owned.value);

    lo_result result;
    lo_apply(owner_context, shared, add_two, &result);
    printf("success %u %u %lld\n", result.status, result.code, lo_value_read(result.value));
    lo_value_release(result.value);
    lo_apply(owner_context, shared, add_two, &result);
    printf("limit %u %u %u\n", result.status, result.code, result.value == 0);
    lo_context_drop(owner_context);

    lo_context *negative_context = lo_context_new(1);
    const lo_value *negative = lo_value_new(-1);
    lo_apply(negative_context, negative, add_two, &result);
    printf("error %u %u %lld\n", result.status, result.code, lo_value_read(result.value));
    lo_value_release(result.value);
    lo_value_release(negative);
    lo_context_drop(negative_context);

    lo_context *callback_context = lo_context_new(1);
    lo_apply(callback_context, shared, fail_callback, &result);
    printf("callback %u %u %lld\n", result.status, result.code, lo_value_read(result.value));
    lo_value_release(result.value);
    lo_context_drop(callback_context);
    lo_value_release(shared);
    printf("cleanup %llu callbacks %d\n", lo_live_values(), callbacks);
    return lo_live_values() == 0 && callbacks == 3 ? 0 : 91;
}
