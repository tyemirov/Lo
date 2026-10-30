#include "lo_context_options.h"
#define LO_OPTIONS(budget) (&(const ContextOptions){(budget),16ULL<<20,64ULL<<20,0,0,0,128})
#include "capture.h"
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef __SIZE_TYPE__ Size;
typedef struct Context Context;
typedef struct Value Value;
typedef struct {U32 status,code;const Value *value;} Result;
typedef struct {int stage,class_id,started,cleanup_complete,termination_kind,termination_value;Size stdin_written,stdout_size,stderr_size;unsigned int cleanup_failures;Size ownership;} NativeResult;
typedef int (*Cancel)(const void*);
typedef void (*Driver)(Size,const unsigned char*,Size,const unsigned char*,Size,unsigned char*,unsigned char*,Size,Cancel,const void*,U64,NativeResult*);
extern Context *lo_context_new(const ContextOptions *);
extern void lo_context_run(Context*,Result*),lo_context_free(Context*);
extern int lo_context_process_driver(Context*,Driver,Size),lo_context_process_close(Context*),lo_context_process_wait_retained(Context*);
extern void lo_process_result_entry(Context*,const Value**,U64,U64,Result*);
extern U64 lo_runtime_live_contexts(void),lo_runtime_live_values(void);
extern int lo_cli_main(int,const char**,const char**);
extern int dup2(int,int),close(int),fcntl(int,int,...);
extern long read(int,void*,Size),write(int,const void*,Size);
extern void *memcpy(void*,const void*,Size);
static unsigned calls,embedding;
static int hook_error;
static int contains(const char *text,const char *needle){for(;*text;++text){Size i=0;while(needle[i]&&text[i]==needle[i])++i;if(!needle[i])return 1;}return 0;}
static void execute(Size token,const unsigned char*frame,Size frame_size,const unsigned char*input,Size input_size,unsigned char*out,unsigned char*err,Size capacity,Cancel cancelled,const void*cancel,U64 timeout,NativeResult*r){
 (void)token;(void)frame;(void)frame_size;(void)input;(void)input_size;(void)cancelled;(void)cancel;(void)timeout;
 static const char stdout_bytes[]="process-argument-secret-sentinel",stderr_bytes[]="process-environment-secret-sentinel";
 if(capacity<sizeof(stderr_bytes)){hook_error=1;return;}
 ++calls;memcpy(out,stdout_bytes,sizeof(stdout_bytes)-1);memcpy(err,stderr_bytes,sizeof(stderr_bytes)-1);
 r->started=1;r->cleanup_complete=1;r->termination_kind=(LO_CASE==1||LO_CASE==2||LO_CASE==6)?2:1;r->termination_value=7;
 r->stdout_size=sizeof(stdout_bytes)-1;r->stderr_size=sizeof(stderr_bytes)-1;
}
void lo_entry(Context*c,const Value**a,U64 n,U64 s,Result*out){
 if(embedding){lo_process_result_entry(c,a,n,s,out);return;}
 Context*context=lo_context_new(LO_OPTIONS(100000));
 if(!context||!lo_context_process_driver(context,execute,0)){hook_error=1;return;}
 embedding=1;lo_context_run(context,out);embedding=0;
 if(!lo_context_process_close(context)||!lo_context_process_wait_retained(context))hook_error=1;
 lo_context_free(context);
 // The embedding contract publishes recoverable errors as Error values.
 if(out->status==1){out->status=0;out->code=0;}
}
int main(void){
 if(LO_CASE==2||LO_CASE==3){int capture_error=lo_capture_start();if(capture_error)return capture_error;}
 const char*args[]={"process-result","run","--format","json"};
 int status=lo_cli_main(4,args,0);
 if(LO_CASE==2||LO_CASE==3){
  int capture_error=lo_capture_finish();if(capture_error)return capture_error;char *output=lo_capture_bytes;long count=(long)lo_capture_length;
  if(!contains(output,"\"kind\":\"Process.TerminationKind\"") || !contains(output,"\"flow\":{\"type\":\"String\",\"value\":\"Main\"}")){write(2,output,(Size)count);return 20;}
 }
 if(status||hook_error||calls!=1)return 21;
 if(lo_runtime_live_contexts()||lo_runtime_live_values())return 22;
 return 0;
}
