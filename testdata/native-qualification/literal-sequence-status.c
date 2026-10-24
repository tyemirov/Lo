typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef struct Program Program;
typedef struct Context Context;
typedef struct Value Value;
typedef struct { U32 status, code; const Value *value; } Result;
#define ABI(name) void name(Context *context, const Value *const *arguments, U64 count, U64 site, Result *out)
extern const Program *lo_program_new(void);
extern void lo_program_free(const Program *);
extern Context *lo_context_from_program(const Program *, U64);
extern void lo_context_free(Context *);
extern ABI(lo_entry);
extern ABI(lo_scope_enter);
extern ABI(lo_step);
extern ABI(lo_literal);
extern ABI(lo_charge_value);
extern ABI(lo_op_List_2Enew);
extern ABI(lo_cause);
extern ABI(lo_invalid_status);
extern void lo_value_retain(const Value *), lo_value_release(const Value *);
extern void lo_scope_leave(Context *,const Value *,U64);
extern U64 lo_context_scope_count(const Context *), lo_context_depth(const Context *), lo_context_remaining_steps(const Context *);
extern U64 lo_runtime_live_values(void), lo_runtime_live_programs(void), lo_runtime_live_contexts(void);
extern const U64 lo_literal_sequence_sites[15], lo_literal_sequence_body_site, lo_literal_status_invalid_site;
extern long write(int,const void *,unsigned long);
static const unsigned kinds[15]={1,2,2,2,3,4,2,3,4,2,3,4,5,4,6};
static const unsigned boundaries[10]={4,5,6,7,8,9,10,11,12,13};
static const Value *const poison=(const Value *)1;
static const Value *owners[15];
static unsigned calls,owned,released,closed,invalid,error,target,mode,failed,retains,null_releases;
static int is_zero(const Result *out){return !out->status && !out->code && !out->value;}
static void before(unsigned kind,U64 site,Result *out){
 if(failed || closed || calls>=15 || kinds[calls]!=kind || lo_literal_sequence_sites[calls]!=site || !is_zero(out))error=1;
 ++calls;
}
static void after(Result *out){
 if(calls==target){
  /* These malformed results exist only at this trusted ABI boundary. Dispose of
     the real runtime result before replacing it with a non-owned poison. */
  if(out->status || !out->value){error=2;return;}
  lo_value_release(out->value);
  *out=(Result){0,0,0};
  if(mode==0)*out=(Result){1,1,poison};
  if(mode==1)*out=(Result){99,99,poison};
  if(mode==2)*out=(Result){(U32)-1,99,poison};
  if(mode==3)*out=(Result){2,77,poison};
  if(mode==4)out->status=1;
  if(mode==5)out->status=2;
 }
 if(out->status)failed=1;
 else if(out->value)owners[owned++]=out->value;
}
ABI(lo_literal_sequence_enter){before(1,site,out);lo_scope_enter(context,arguments,count,site,out);after(out);}
ABI(lo_literal_sequence_step){before(2,site,out);lo_step(context,arguments,count,site,out);after(out);}
ABI(lo_literal_sequence_literal){before(3,site,out);lo_literal(context,arguments,count,site,out);after(out);}
ABI(lo_literal_sequence_charge){before(4,site,out);lo_charge_value(context,arguments,count,site,out);after(out);}
ABI(lo_literal_sequence_list){before(5,site,out);lo_op_List_2Enew(context,arguments,count,site,out);after(out);}
ABI(lo_literal_sequence_cause){before(6,site,out);lo_cause(context,arguments,count,site,out);after(out);}
ABI(lo_literal_status_invalid){
 if(!failed || site!=lo_literal_status_invalid_site || !is_zero(out) || invalid++)error=3;
 lo_invalid_status(context,arguments,count,site,out);
}
void lo_literal_status_retain(const Value *value){if(value==poison)error=4;else {++retains;lo_value_retain(value);}}
void lo_literal_sequence_release(const Value *value){
 if(value==poison){error=5;return;}
 if(value){if(!closed || released>=owned || owners[released]!=value)error=6;else ++released;}
 else ++null_releases;
 lo_value_release(value);
}
void lo_literal_sequence_scope_leave(Context *context,const Value *value,U64 site){
 if(closed || released || site!=lo_literal_sequence_body_site || value==poison || (failed && value))error=7;
 lo_scope_leave(context,value,site);closed=1;
}
static int session(unsigned boundary,unsigned selected_mode){
 calls=owned=released=closed=invalid=error=failed=retains=null_releases=0;target=boundary;mode=selected_mode;
 const Program *program=lo_program_new();if(!program)return 10;
 Context *context=lo_context_from_program(program,100);if(!context)return 11;
 Result result={0,0,0};lo_entry(context,0,0,0,&result);
 unsigned rejects=mode<=2 || mode==4;
 unsigned null_fault=mode==6 && (kinds[target-1]==3 || kinds[target-1]==5);
 unsigned terminal=mode<6 || null_fault;
 if(error)return 20+error;
 if(!closed || released!=owned || !null_releases || lo_context_scope_count(context) || lo_context_depth(context))return 30;
 if(terminal){
  if(result.status!=2 || result.code!=(mode==3?77:mode==5?0:2) || result.value || retains || invalid!=rejects || calls!=target+null_fault)return 31;
 }else if(result.status || result.code || !result.value || calls!=15 || invalid || retains!=1)return 32;
 /* A real subsequent call distinguishes the sticky invalid/null-input fault
    from injected host status/code, which does not manufacture Context.fault. */
 U64 remaining=lo_context_remaining_steps(context);
 Result probe={0,0,0};lo_step(context,0,0,lo_literal_sequence_sites[1],&probe);
 if(rejects || null_fault){if(probe.status!=2 || probe.code!=2 || probe.value || lo_context_remaining_steps(context)!=remaining)return 33;}
 else {if(probe.status || probe.code || !probe.value || lo_context_remaining_steps(context)!=remaining-1)return 34;}
 lo_value_release(probe.value);lo_value_release(result.value);
 lo_context_free(context);lo_program_free(program);
 if(lo_runtime_live_values() || lo_runtime_live_contexts() || lo_runtime_live_programs())return 35;
 return 0;
}
int main(void){
 for(unsigned boundary=0;boundary<10;++boundary)for(unsigned selected_mode=0;selected_mode<7;++selected_mode){
  int status=session(boundaries[boundary],selected_mode);if(status)return status;
 }
 const char message[]="literal-sequence-status-ok\n";return write(1,message,sizeof(message)-1)==sizeof(message)-1?0:45;
}
