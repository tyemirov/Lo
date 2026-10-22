typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef __SIZE_TYPE__ Size;
typedef struct Context Context;
typedef struct Program Program;
typedef struct Value Value;
typedef struct {U32 status,code;const Value *value;} Result;
extern const Program *lo_program_new(void);
extern void lo_program_free(const Program*);
extern Context *lo_context_from_program(const Program*,U64);
extern void lo_context_free(Context*),lo_context_run(Context*,Result*),lo_context_cancel_after_polls(Context*,U64);
extern U64 lo_context_scope_count(const Context*),lo_context_depth(const Context*),lo_context_tape_count(const Context*),lo_context_remaining_steps(const Context*),lo_context_resource_bytes(const Context*,U32),lo_context_peak_resource_bytes(const Context*,U32);
extern U64 lo_runtime_live_contexts(void),lo_runtime_live_values(void),lo_runtime_live_storages(void),lo_runtime_live_storage_bytes(void);
extern void lo_value_retain(const Value*),lo_value_release(const Value*);
extern long write(int,const void*,Size);
static void number(U64 value){char digits[32];unsigned index=sizeof(digits);do{digits[--index]='0'+value%10;value/=10;}while(value);write(1,digits+index,sizeof(digits)-index);}
int main(void){
 const Program *program=lo_program_new();if(!program)return 10;
 U64 admitted=lo_runtime_live_values(),used=0,peak=0;
 for(unsigned iteration=0;iteration<32;++iteration){
  Context *context=lo_context_from_program(program,10000000);if(!context)return 11;
  if(LO_CASE==3)lo_context_cancel_after_polls(context,128);
  Result result={99,99,0};lo_context_run(context,&result);
  U32 expected=LO_CASE==2?1:LO_CASE==3?2:0;
  if(result.status!=expected||result.code!=expected||((result.value!=0)!=(expected!=2)))return 12;
  if(lo_context_scope_count(context)||lo_context_depth(context)||lo_context_tape_count(context)||lo_context_resource_bytes(context,1)||lo_context_resource_bytes(context,3))return 13;
  U64 spent=10000000-lo_context_remaining_steps(context),reserved=lo_context_peak_resource_bytes(context,0);
  if(!iteration){used=spent;peak=reserved;}
  if(!spent||spent!=used||reserved!=peak)return 14;
  if(LO_CASE&& !reserved)return 15;
  if(result.value)lo_value_retain(result.value);
  lo_context_free(context);
  if(lo_runtime_live_contexts())return 16;
  if(LO_CASE==1&&(lo_runtime_live_storages()!=1||lo_runtime_live_storage_bytes()!=4))return 17;
  if(result.value){lo_value_release(result.value);lo_value_release(result.value);}
  if(lo_runtime_live_values()!=admitted||lo_runtime_live_storages()||lo_runtime_live_storage_bytes())return 18;
 }
 lo_program_free(program);if(lo_runtime_live_values())return 19;
 write(1,"{\"steps\":",9);number(used);write(1,",\"peak\":",8);number(peak);write(1,"}\n",2);return 0;
}
