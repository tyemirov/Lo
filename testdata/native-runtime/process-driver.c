#include "lo_context_options.h"
#define LO_OPTIONS(budget) (&(const ContextOptions){(budget),16ULL<<20,64ULL<<20,0,0,0,128})
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef __SIZE_TYPE__ Size;
typedef struct Context Context;
typedef struct Value Value;
typedef struct {U32 status,code;const Value *value;} ResultSlot;
typedef struct {int stage,class_id,started,cleanup_complete,termination_kind,termination_value;Size stdin_written,stdout_size,stderr_size;unsigned int cleanup_failures;Size ownership;} NativeResult;
typedef int (*Cancel)(const void *);
typedef void (*Driver)(Size,const unsigned char*,Size,const unsigned char*,Size,unsigned char*,unsigned char*,Size,Cancel,const void*,U64,NativeResult*);
extern Context *lo_context_new(const ContextOptions *);
extern void lo_context_run(Context*,ResultSlot*);
extern void lo_context_free(Context*);
extern void lo_value_release(const Value*);
extern int lo_context_process_driver(Context*,Driver,Size);
extern int lo_context_process_driver_limits(Context*,U64,U64,Size);
extern int lo_context_process_close(Context*);
extern Context *lo_context_process_peer(Context*);
extern int lo_context_process_wait_retained(Context*);
extern U32 lo_context_process_ownership(Context*);
extern U64 lo_runtime_live_contexts(void);
extern U64 lo_runtime_live_values(void);
struct timespec {long seconds,nanoseconds;};
extern int nanosleep(const struct timespec*,struct timespec*);
#if LO_CASE==5
extern int *__error(void);
extern int *__errno_location(void);
int pthread_create(__UINTPTR_TYPE__ *thread,const void *attributes,void *(*start)(void*),void *argument){(void)thread;(void)attributes;(void)start;(void)argument;
#ifdef __APPLE__
 *__error()=35;return 35;
#else
 *__errno_location()=11;return 11;
#endif
}
#endif
static unsigned calls;
static unsigned call_count(void){return __atomic_load_n(&calls,__ATOMIC_SEQ_CST);}
static void execute(Size token,const unsigned char*frame,Size frame_size,const unsigned char*input,Size input_size,unsigned char*out,unsigned char*err,Size capacity,Cancel cancelled,const void*cancel,U64 timeout,NativeResult*r){
 (void)token;(void)frame;(void)frame_size;(void)input;(void)input_size;(void)out;(void)err;(void)capacity;(void)cancelled;(void)cancel;(void)timeout;
 __atomic_fetch_add(&calls,1,__ATOMIC_SEQ_CST);
 if(LO_CASE==1){const struct timespec delay={0,200000000};nanosleep(&delay,0);}
 if(LO_CASE==2)return; /* nil termination and nil error is unresolved ownership. */
 r->started=1;r->termination_kind=1;r->termination_value=7;r->cleanup_complete=1;
 if(LO_CASE==3){r->stage=6;r->class_id=2;r->cleanup_complete=0;}
 if(LO_CASE==4)r->termination_value=-1;
 if(LO_CASE==6)r->class_id=2;
 if(LO_CASE==7){r->stage=1;r->class_id=2;r->termination_kind=0;}
}
int main(void){
 Context *context=lo_context_new(LO_OPTIONS(100000));if(!context)return 10;
 if(!lo_context_process_driver(context,execute,0))return 11;
 if(!lo_context_process_driver_limits(context,20,20,1024))return 12;
 ResultSlot result={0,0,0};lo_context_run(context,&result);
 if(LO_CASE==0){if(result.status!=0||!result.value||call_count()!=1)return 20;lo_value_release(result.value);if(!lo_context_process_close(context)||!lo_context_process_wait_retained(context))return 21;}
 else if(LO_CASE==5){if(result.status!=2||result.value||call_count()!=0||lo_context_process_ownership(context)!=0)return 60;if(!lo_context_process_close(context)||!lo_context_process_wait_retained(context))return 61;}
 else if(LO_CASE==1){
  if(result.status!=2||result.value||call_count()!=1||!(lo_context_process_ownership(context)&1))return 30;
  Context *peer=lo_context_process_peer(context);if(!peer)return 35;lo_context_run(peer,&result);lo_context_free(peer);if(result.status!=2||call_count()!=1)return 31; /* Busy never invokes another owner. */
  if(lo_context_process_close(context))return 32;
  if(lo_context_process_wait_retained(context))return 33; /* Historical timeout remains reported. */
  if(lo_context_process_ownership(context)!=4||call_count()!=1)return 34;
 }else{
  if(result.status!=2||result.value||call_count()!=1)return 40;
  if(lo_context_process_ownership(context)!=6)return 41;
  if(lo_context_process_close(context))return 42;
  lo_context_run(context,&result);if(result.status!=2||call_count()!=1)return 43;
  /* Unknown ownership has no release handle; do not wait for fictitious release. */
 }
 lo_context_free(context);
 return lo_runtime_live_contexts()||lo_runtime_live_values()?50:0;
}
