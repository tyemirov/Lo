typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef __UINTPTR_TYPE__ Thread;
typedef struct Context Context;
typedef struct Value Value;
typedef struct { U32 status; U32 code; const Value *value; } ResultSlot;
extern Context *lo_context_new(U64);
extern void lo_context_free(Context *);
extern void lo_context_run(Context *,ResultSlot *);
extern void lo_context_cancel_after_polls(Context *,U64);
extern U64 lo_context_remaining_steps(const Context *);
extern U64 lo_context_scope_count(const Context *);
extern U64 lo_context_depth(const Context *);
extern U64 lo_context_tape_count(const Context *);
extern U64 lo_context_resource_bytes(const Context *,U32);
extern U64 lo_runtime_live_contexts(void);
extern U64 lo_runtime_live_values(void);
extern void lo_value_release(const Value *);
extern void lo_step(Context *,const Value *const *,U64,U64,ResultSlot *);
extern void lo_op_Math_2Eadd(Context *,const Value *const *,U64,U64,ResultSlot *);
extern const U64 lo_test_step_site;
extern int pthread_create(Thread *,const void *,void *(*)(void *),void *);
extern int pthread_join(Thread,void **);

static void *foreign_call(void *pointer) {
 ResultSlot out={99,99,(const Value *)1};
 /* These deliberately invalid arguments must never be inspected off-owner. */
 if (LO_CASE%2) lo_op_Math_2Eadd(pointer,(const Value *const *)1,~(U64)0,~(U64)0,&out);
 else lo_step(pointer,(const Value *const *)1,~(U64)0,~(U64)0,&out);
 return out.status==2 && out.code==3 && !out.value ? 0:(void *)1;
}
static int foreign_rejected_unchanged(Context *context) {
 U64 steps=lo_context_remaining_steps(context), values=lo_runtime_live_values();
 Thread thread;void *thread_result=(void *)1;
 if (pthread_create(&thread,0,foreign_call,context) || pthread_join(thread,&thread_result) || thread_result) return 0;
 if (lo_context_remaining_steps(context)!=steps || lo_runtime_live_values()!=values ||
     lo_context_scope_count(context) || lo_context_depth(context) || lo_context_tape_count(context)) return 0;
 for(U32 resource_class=0;resource_class<4;++resource_class)
  if(lo_context_resource_bytes(context,resource_class))return 0;
 return 1;
}
static int owner_step(Context *context,U32 status) {
 ResultSlot out={99,99,0};
 lo_step(context,0,0,lo_test_step_site,&out);
 int valid=out.status==status && out.code==(status?2:0) && (out.value!=0)==(status==0);
 if(out.value)lo_value_release(out.value);
 return valid;
}
int main(void) {
 for(unsigned iteration=0;iteration<32;++iteration) {
  Context *context=lo_context_new(100000);
  if(!context || lo_runtime_live_contexts()!=1)return 1;
  if(LO_CASE<2) {
   if(!foreign_rejected_unchanged(context))return 2;
   ResultSlot out={99,99,0};lo_context_run(context,&out);
   if(out.status || out.code || !out.value || lo_context_remaining_steps(context)>=100000)return 3;
   lo_value_release(out.value);
  } else {
   lo_context_cancel_after_polls(context,2);
   /* Foreign calls must not advance polling: the first owner step succeeds. */
   if(!foreign_rejected_unchanged(context) || !owner_step(context,0))return 4;
   if(lo_context_remaining_steps(context)!=99999)return 5;
   if(!foreign_rejected_unchanged(context) || !owner_step(context,2))return 6;
   if(lo_context_remaining_steps(context)!=99999)return 7;
   /* A stored cancellation fault must not outrank wrong-thread rejection. */
   if(!foreign_rejected_unchanged(context) || !owner_step(context,2))return 8;
   ResultSlot out={99,99,0};lo_context_run(context,&out);
   if(out.status!=2 || out.code!=2 || out.value || lo_context_remaining_steps(context)!=99999)return 9;
  }
  if(lo_context_scope_count(context) || lo_context_depth(context))return 10;
  lo_context_free(context);
  if(lo_runtime_live_contexts() || lo_runtime_live_values())return 11;
 }
 return 0;
}
