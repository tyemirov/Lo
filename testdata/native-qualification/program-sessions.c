#include "lo_context_options.h"
#define LO_OPTIONS(budget) (&(const ContextOptions){(budget),16ULL<<20,64ULL<<20,0,0,0,128})
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef struct Program Program;
typedef struct Context Context;
typedef struct Value Value;
typedef struct { U32 status; U32 code; const Value *value; } ResultSlot;
extern const Program *lo_program_new(void);
extern void lo_program_free(const Program *);
extern Context *lo_context_from_program(const Program *, const ContextOptions *);
extern void lo_context_free(Context *);
extern void lo_context_run(Context *, ResultSlot *);
extern void lo_context_cancel(Context *);
extern U64 lo_context_remaining_steps(const Context *);
extern U64 lo_runtime_live_programs(void);
extern U64 lo_runtime_live_contexts(void);
extern U64 lo_runtime_live_values(void);
extern void lo_value_release(const Value *);
static int run(Context *context, U32 status) {
 ResultSlot result = {99, 99, 0};
 lo_context_run(context, &result);
 if (result.status != status || result.code != status || (result.value != 0) != (status != 2)) return 0;
 if (result.value) lo_value_release(result.value);
 return 1;
}
int main(void) {
 if (lo_runtime_live_programs() || lo_runtime_live_contexts() || lo_runtime_live_values()) return 1;
 if (lo_context_from_program(0,LO_OPTIONS(100000))) return 2;
 lo_program_free(0);
 for (unsigned iteration = 0; iteration < 64; iteration++) {
  const Program *program = lo_program_new();
  if (LO_CASE == 1) {
   if (program || lo_runtime_live_programs()) return 3;
   continue;
  }
  if (!program || lo_runtime_live_programs() != 1 || lo_context_from_program(program,LO_OPTIONS(0))) return 4;
  Context *cancelled = lo_context_from_program(program,LO_OPTIONS(100000));
  Context *first = lo_context_from_program(program,LO_OPTIONS(100000));
  Context *limited = lo_context_from_program(program,LO_OPTIONS(1));
  if (!cancelled || !first || !limited || lo_runtime_live_contexts() != 3) return 5;
  lo_context_cancel(cancelled);
  if (!run(cancelled, 2) || !run(first, 0) || !run(limited, 2)) return 6;
  U64 remaining = lo_context_remaining_steps(first);
  if (remaining >= 100000 || lo_context_remaining_steps(cancelled) != 100000) return 7;
  lo_context_free(cancelled);
  lo_context_free(limited);
  Context *second = lo_context_from_program(program,LO_OPTIONS(100000));
  if (!second) return 8;
  lo_program_free(program);
  if (lo_runtime_live_programs() != 1 || !run(second, 0) || lo_context_remaining_steps(second) != remaining) return 9;
  lo_context_free(first);
  if (lo_runtime_live_programs() != 1) return 10;
  lo_context_free(second);
  if (lo_runtime_live_programs() || lo_runtime_live_contexts() || lo_runtime_live_values()) return 11;
 }
 return 0;
}
