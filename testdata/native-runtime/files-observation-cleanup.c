typedef __SIZE_TYPE__ Size;
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef struct Context Context;
typedef struct Value Value;
typedef struct { U32 status, code; const Value *value; } Result;
extern void lo_op_File_2Edirectory(Context *, const Value **, U64, U64, Result *);
extern int lo_cli_main(int, const char **, const char **);
extern U64 lo_runtime_live_contexts(void), lo_runtime_live_values(void);
extern int mkdir(const char *, unsigned), open(const char *, int, ...);
extern int rename(const char *, const char *), fcntl(int, int, ...), dup2(int, int);
extern long write(int, const void *, Size), read(int, void *, Size), lseek(int, long, int);
extern void *dlsym(void *, const char *);
#ifdef __APPLE__
extern int *__error(void);
#define ERROR_ADDRESS __error
#define CREATE (2|0x200|0x400)
#else
extern int *__errno_location(void);
#define ERROR_ADDRESS __errno_location
#define CREATE (2|0x40|0x200)
#endif
static int (*real_close)(int);
static int directory_calls, armed, close_failures;
int close(int descriptor) {
 int result = real_close(descriptor);
 if (armed) { armed = 0; ++close_failures; *ERROR_ADDRESS() = 5; return -1; }
 return result;
}
void lo_observation_directory_bridge(Context *context, const Value **arguments, U64 count, U64 site, Result *output) {
 ++directory_calls;
 lo_op_File_2Edirectory(context, arguments, count, site, output);
 if (directory_calls == 2 && !output->status) armed = 1;
}
int main(void) {
 real_close = (int (*)(int))dlsym((void *)-1, "close");
 if (!real_close) return 10;
 const char *roots[] = { "first", "second", "third" };
 for (int index = 0; index < 3; ++index) {
  if (mkdir(roots[index], 0777)) return 11;
 }
 int output = open("diagnostic", CREATE, 0666);
 int saved_out = fcntl(1, 0, 3), saved_err = fcntl(2, 0, 3);
 if (output < 0 || saved_out < 0 || saved_err < 0 || dup2(output, 1) < 0 || dup2(output, 2) < 0) return 12;
 const char *arguments[] = { "fixture", "run", "--format", "json", "--list-root", "first", "--list-root", "second", "--list-root", "third" };
 int status = lo_cli_main(10, arguments, 0);
 armed = 0;
 if (dup2(saved_out, 1) < 0 || dup2(saved_err, 2) < 0 || lseek(output, 0, 0) < 0) return 13;
 char diagnostic[65536];
 long count = read(output, diagnostic, sizeof(diagnostic) - 1);
 if (count <= 0 || count == (long)sizeof(diagnostic) - 1) return 14;
 if (status != 2 || directory_calls != 2 || close_failures != 1) { write(2, diagnostic, (Size)count); return 20; }
 if (lo_runtime_live_values() || lo_runtime_live_contexts()) return 21;
 if (real_close(output) || real_close(saved_out) || real_close(saved_err)) return 22;
 return write(1, diagnostic, (Size)count) == count ? 0 : 23;
}
