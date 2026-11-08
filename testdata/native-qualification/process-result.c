#include "lo_process.h"
#define LO_OPTIONS(budget) (&(const ContextOptions){(budget),16ULL<<20,64ULL<<20,0,0,0,128})
#include "capture.h"
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef __SIZE_TYPE__ Size;
extern void lo_process_result_entry(Context*,const Value**,U64,U64,ResultSlot*);
extern U64 lo_runtime_live_contexts(void),lo_runtime_live_values(void);
extern int lo_cli_main(int,const char**,const char**);
extern int dup2(int,int),close(int),fcntl(int,int,...);
extern long read(int,void*,Size),write(int,const void*,Size);
static unsigned calls,embedding;
static int hook_error;
static int contains(const char *text,const char *needle){for(;*text;++text){Size i=0;while(needle[i]&&text[i]==needle[i])++i;if(!needle[i])return 1;}return 0;}
static void execute(Size token,const LoProcessRequest *request,LoProcessResult*r){
 (void)token;
 static const char stdout_bytes[]="process-argument-secret-sentinel",stderr_bytes[]="process-environment-secret-sentinel";
 if(request->output_bytes<sizeof(stderr_bytes)){hook_error=1;return;}
 ++calls;
 r->started=1;r->cleanup_complete=1;r->termination_kind=(LO_CASE==1||LO_CASE==2||LO_CASE==6)?2:1;r->termination_value=7;
 uintptr_t confirmed=0;
 int status=request->output(request->output_token,1,(const unsigned char *)stdout_bytes,sizeof(stdout_bytes)-1,&confirmed);
 r->stdout_size=confirmed;
 if(status||confirmed!=sizeof(stdout_bytes)-1){hook_error=2;r->stage=3;r->class_id=status?status:LO_PROCESS_HOST;return;}
 status=request->output(request->output_token,2,(const unsigned char *)stderr_bytes,sizeof(stderr_bytes)-1,&confirmed);
 r->stderr_size=confirmed;
 if(status||confirmed!=sizeof(stderr_bytes)-1){hook_error=3;r->stage=4;r->class_id=status?status:LO_PROCESS_HOST;}
}
void lo_entry(Context*c,const Value**a,U64 n,U64 s,ResultSlot*out){
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
