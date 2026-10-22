typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef struct Program Program;
typedef struct Context Context;
typedef struct Value Value;
typedef struct { U32 status; U32 code; const Value *value; } ResultSlot;
extern const Program *lo_program_new(void);
extern void lo_program_free(const Program *);
extern Context *lo_context_from_program(const Program *,U64);
extern void lo_context_free(Context *);
extern void lo_context_run(Context *,ResultSlot *);
extern void lo_context_cancel(Context *);
extern U64 lo_context_remaining_steps(const Context *);
extern U64 lo_runtime_live_programs(void);
extern U64 lo_runtime_live_contexts(void);
extern U64 lo_runtime_live_values(void);
extern void lo_step(Context *,const Value *const *,U64,U64,ResultSlot *);
extern void lo_value_retain(const Value *);
extern void lo_value_release(const Value *);
extern const U64 lo_test_step_site;
int main(void){
 for(unsigned iteration=0;iteration<32;++iteration){
  const Program *program=lo_program_new();
  if(LO_CASE==1){if(program || lo_runtime_live_programs() || lo_runtime_live_values())return 1;continue;}
  if(!program)return 2;
  Context *first=lo_context_from_program(program,100000),*second=lo_context_from_program(program,100000);
  if(!first || !second)return 3;
  ResultSlot first_value={99,99,0},second_value={99,99,0};
  lo_step(first,0,0,lo_test_step_site,&first_value);
  lo_step(second,0,0,lo_test_step_site,&second_value);
  if(first_value.status || first_value.code || !first_value.value || second_value.status || second_value.code || !second_value.value)return 4;
  if(lo_context_remaining_steps(first)!=99999 || lo_context_remaining_steps(second)!=99999)return 5;
  lo_context_cancel(first);ResultSlot cancelled={99,99,0};lo_context_run(first,&cancelled);
  if(cancelled.status!=2 || cancelled.code!=2 || cancelled.value || lo_context_remaining_steps(first)!=99999)return 6;
  lo_program_free(program);lo_context_free(first);
  if(lo_runtime_live_programs()!=1 || lo_runtime_live_contexts()!=1)return 7;
  ResultSlot result={99,99,0};lo_context_run(second,&result);
  if(result.status || result.code || !result.value || lo_context_remaining_steps(second)>=99999)return 8;
  lo_value_release(result.value);lo_context_free(second);
  if(lo_runtime_live_programs() || lo_runtime_live_contexts() || !lo_runtime_live_values())return 9;
  /* Escaped immutable control values remain owned after both sessions die. */
  lo_value_retain(first_value.value);lo_value_release(first_value.value);lo_value_release(first_value.value);
  if(!lo_runtime_live_values())return 10;
  lo_value_release(second_value.value);
  if(lo_runtime_live_programs() || lo_runtime_live_contexts() || lo_runtime_live_values())return 11;
 }
 return 0;
}
