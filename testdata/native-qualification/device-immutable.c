#include "lo_runtime.h"
#define LO_OPTIONS(budget) (&(const ContextOptions){(budget),16ULL<<20,64ULL<<20,0,0,0,128})
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;

extern void lo_flow_2(Context*,const Value**,U64,U64,ResultSlot*);
extern int lo_boolean_value(const Value*);
extern U64 lo_runtime_live_values(void),lo_runtime_live_contexts(void);
static int verify(Context*context,const Value*value){
 const Value*args[]={value};ResultSlot checked={99,99,0};
 lo_flow_2(context,args,1,0,&checked);
 if(checked.status||!checked.value)return 1;
 int valid=lo_boolean_value(checked.value);lo_value_release(checked.value);
 return !valid;
}
int main(void){
 Context*context=lo_context_new(LO_OPTIONS(1000000));if(!context)return 10;
 const Value*retained=0;
 for(unsigned iteration=0;iteration<16;++iteration){
  ResultSlot result={99,99,0};lo_context_run(context,&result);
  if(result.status||!result.value)return 11;
  if(!iteration){retained=result.value;lo_value_retain(retained);}
  lo_value_release(result.value);
  // New evaluations and derived records must not change the retained record.
  if(verify(context,retained))return 12;
 }
 lo_context_free(context);
 if(lo_runtime_live_contexts()||!lo_runtime_live_values())return 13;
 context=lo_context_new(LO_OPTIONS(1000000));if(!context||verify(context,retained))return 14;
 lo_context_free(context);lo_value_release(retained);
 return lo_runtime_live_contexts()||lo_runtime_live_values()?15:0;
}
