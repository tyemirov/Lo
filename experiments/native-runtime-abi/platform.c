/* Public C entry points; declarations are authored here without SDK headers. */
extern int printf(const char *, ...);
extern double NSFoundationVersionNumber;
extern void *objc_getClass(const char *);
extern void *MTLCreateSystemDefaultDevice(void);
extern int sandbox_init(const char *, unsigned long long, char **);

int main(void) {
    void *(*volatile metal_entry)(void) = MTLCreateSystemDefaultDevice;
    int (*volatile process_entry)(const char *, unsigned long long, char **) = sandbox_init;
    int foundation = NSFoundationVersionNumber > 0;
    int objects = objc_getClass("NSObject") != 0;
    int metal = metal_entry != 0;
    int process = process_entry != 0;
    printf("platform foundation %d objc %d metal %d process %d\n", foundation, objects, metal, process);
    return foundation && objects && metal && process ? 0 : 92;
}
