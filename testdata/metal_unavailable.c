// Controlled framework boundary for native artifact integration tests.
extern void *MTLCreateSystemDefaultDevice(void);

static void *unavailableDevice(void) { return 0; }

// dyld replaces the imported framework function with this test dependency.
__attribute__((used, section("__DATA,__interpose")))
static const void *const deviceReplacement[2] = {
    (const void *)&unavailableDevice,
    (const void *)&MTLCreateSystemDefaultDevice,
};
