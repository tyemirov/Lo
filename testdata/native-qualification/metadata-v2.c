typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef struct Program Program;
typedef struct Context Context;
typedef struct Value Value;
typedef struct { U32 status,code;const Value *value; } Result;
extern const Program *lo_program_new(void);
extern void lo_program_free(const Program *);
extern Context *lo_context_from_program(const Program *,U64);
extern void lo_context_free(Context *),lo_context_run(Context *,Result *);
extern void lo_value_release(const Value *);
extern U64 lo_context_remaining_steps(const Context *),lo_context_scope_count(const Context *),lo_context_depth(const Context *);
extern U64 lo_runtime_live_values(void),lo_runtime_live_programs(void),lo_runtime_live_contexts(void);
extern int lo_cli_main(int,const char *const *,const char *const *);
static int empty(void){return !lo_runtime_live_values() && !lo_runtime_live_programs() && !lo_runtime_live_contexts();}
int main(void){
 const Program *program=lo_program_new();
 if(LO_CASE==0 || LO_CASE==2){
  if(program){lo_program_free(program);return 10;}
  if(!empty())return 11;
  if(LO_CASE==2){const char *args[]={"metadata-v2","metadata",0};return lo_cli_main(2,args,0);}
  return 0;
 }
 if(!program){const char *args[]={"metadata-v2","metadata",0};return lo_cli_main(2,args,0);}
 Context *context=lo_context_from_program(program,100000);if(!context){lo_program_free(program);return 12;}
 Result result={0,0,0};lo_context_run(context,&result);
 if(result.status!=(LO_CASE==3?1:0) || result.code!=result.status || !result.value || lo_context_remaining_steps(context)>=100000 || lo_context_scope_count(context) || lo_context_depth(context))return 13;
 lo_program_free(program);lo_context_free(context);
 if(lo_runtime_live_programs() || lo_runtime_live_contexts() || !lo_runtime_live_values())return 14;
 lo_value_release(result.value);if(!empty())return 15;
 const char *args[]={"metadata-v2","run","--format","json",0};int status=lo_cli_main(4,args,0);
 return empty()?status:16;
}
