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
extern Context *lo_context_new(U64);
extern void lo_context_run(Context*,Result*),lo_context_free(Context*),lo_value_release(const Value*);
extern void lo_context_cancel_after_polls(Context*,U64);
extern int lo_context_process_driver(Context*,Driver,Size),lo_context_process_driver_limits(Context*,U64,U64,Size),lo_context_process_close(Context*),lo_context_process_wait_retained(Context*);
extern U32 lo_context_process_ownership(Context*);
extern void lo_process_lifecycle_entry(Context*,const Value**,U64,U64,Result*);
extern int lo_cli_main(int,const char**,const char**);
extern U64 lo_runtime_live_values(void),lo_runtime_live_contexts(void);
extern int dup2(int,int),fcntl(int,int,...),close(int);
extern long read(int,void*,Size),write(int,const void*,Size);
extern void *memcpy(void*,const void*,Size);
struct timespec {long sec,nsec;};
extern int nanosleep(const struct timespec*,struct timespec*);
static unsigned entered,finished,joined,released,embedding,slot_status;
static int invalid;
static int contains(const char *text,const char *needle){for(;*text;++text){Size i=0;while(needle[i]&&text[i]==needle[i])++i;if(!needle[i])return 1;}return 0;}
static U32 number(const unsigned char *p){return ((U32)p[0]<<24)|((U32)p[1]<<16)|((U32)p[2]<<8)|p[3];}
static int equal(const unsigned char *bytes,Size size,const char*text){Size n=0;while(text[n])++n;if(n!=size)return 0;for(Size i=0;i<size;++i)if(bytes[i]!=(unsigned char)text[i])return 0;return 1;}
static int invocation(const unsigned char*frame,Size size,const unsigned char*input,Size input_size){
 if(size<16 || !equal(frame,4,"LOP1") || number(frame+4)!=size-16 || number(frame+8)!=3 || number(frame+12)!=1 || !equal(input,input_size,"input"))return 1;
 const char *expected[]={"/command","/work","/command","one two","$VALUE","ONLY=explicit"};Size offset=16;
 for(unsigned i=0;i<6;++i){if(offset+4>size)return 2;Size count=number(frame+offset);offset+=4;if(offset+count>size||!equal(frame+offset,count,expected[i]))return 3;offset+=count;}
 return offset!=size;
}
static void execute(Size token,const unsigned char*frame,Size frame_size,const unsigned char*input,Size input_size,unsigned char*out,unsigned char*err,Size capacity,Cancel cancelled,const void*cancel,U64 timeout,NativeResult*r){
 (void)token;(void)timeout;__atomic_fetch_add(&entered,1,__ATOMIC_SEQ_CST);
 if(invocation(frame,frame_size,input,input_size))invalid=1;
 r->started=1;r->cleanup_complete=1;r->termination_kind=LO_CASE==1||LO_CASE==9?2:1;r->termination_value=LO_CASE==1||LO_CASE==9?15:73;r->stdin_written=input_size;
 if(LO_CASE==9){
  const struct timespec pause={0,1000000};unsigned polls=0;
  while(!cancelled(cancel) && polls++<5000)nanosleep(&pause,0);
  if(!cancelled(cancel))invalid=2;
  r->stage=7;r->class_id=1;r->termination_kind=2;r->termination_value=15;r->stdout_size=7;
  memcpy(out,"partial",7);
 }else if(LO_CASE==2||LO_CASE==7||LO_CASE==8){
  if(capacity!=4)invalid=3;
  memcpy(out,"four",4);memcpy(err,"four",4);r->stdout_size=4;r->stderr_size=4;
  if(LO_CASE==7||LO_CASE==8){r->stage=LO_CASE==7?3:4;r->class_id=3;}
 }else if(LO_CASE>=3&&LO_CASE<=6){
  r->stage=LO_CASE==3?3:LO_CASE==4?2:LO_CASE==5?1:6;
  r->class_id=LO_CASE==6?2:0;r->stdin_written=LO_CASE==4?2:LO_CASE==5?0:input_size;
  static const char secret[]="process-argument-secret-sentinel";
  memcpy(out,secret,sizeof(secret)-1);r->stdout_size=sizeof(secret)-1;
  if(LO_CASE==6)r->cleanup_failures=4;
 }else {
  if(capacity<12)invalid=4;
  const unsigned char binary[]={0,255,'o'};memcpy(out,binary,3);memcpy(err,"error-output",12);r->stdout_size=3;r->stderr_size=12;
 }
 // The synchronous driver owns all endpoints until its one wait and release finish.
 __atomic_fetch_add(&joined,1,__ATOMIC_SEQ_CST);__atomic_fetch_add(&released,1,__ATOMIC_SEQ_CST);__atomic_store_n(&finished,1,__ATOMIC_SEQ_CST);
}
static Context *context(void){
 Context*c=lo_context_new(1000000);if(!c)return 0;
 if(!lo_context_process_driver(c,execute,0)||!lo_context_process_driver_limits(c,5000,1000,LO_CASE==2||LO_CASE==7||LO_CASE==8?4:1024)){invalid=5;return c;}
 if(LO_CASE==9)lo_context_cancel_after_polls(c,128);
 return c;
}
void lo_entry(Context*c,const Value**a,U64 n,U64 s,Result*out){
 if(embedding){lo_process_lifecycle_entry(c,a,n,s,out);return;}
 Context*owned=context();if(!owned){invalid=6;return;}
 embedding=1;lo_context_run(owned,out);embedding=0;slot_status=out->status;
 if(!__atomic_load_n(&finished,__ATOMIC_SEQ_CST)||!lo_context_process_close(owned)||!lo_context_process_wait_retained(owned)||lo_context_process_ownership(owned))invalid=7;
 lo_context_free(owned);
 if(out->status==1){out->status=0;out->code=0;}
}
int main(void){
 int host=LO_CASE>=6;
 if(host){
  Context*c=context();if(!c)return 10;Result result={99,99,0};embedding=1;lo_context_run(c,&result);embedding=0;
  if(result.status!=2||result.value||!__atomic_load_n(&finished,__ATOMIC_SEQ_CST))return 11;
  if(!lo_context_process_close(c)||!lo_context_process_wait_retained(c)||lo_context_process_ownership(c))return 12;
  lo_context_free(c);
 }else {
  int capture_error=lo_capture_start();if(capture_error)return capture_error;
  const char*args[]={"process-lifecycle","run","--format","json"};int status=lo_cli_main(4,args,0);
  capture_error=lo_capture_finish();if(capture_error)return capture_error;char *output=lo_capture_bytes;long count=(long)lo_capture_length;
  if(status)return 24;
  if(LO_CASE<=2){
   const char *termination=LO_CASE==1?"\"kind\":{\"type\":\"String\",\"value\":\"signaled\"}":"\"kind\":{\"type\":\"String\",\"value\":\"exited\"}";
   if(slot_status || !contains(output,termination) || !contains(output,LO_CASE==2?"Zm91cg==":"AP9v") || !contains(output,LO_CASE==2?"Zm91cg==":"ZXJyb3Itb3V0cHV0")){write(2,output,(Size)count);return 25;}
  }else {
   const char *stage=LO_CASE==3?"\"value\":\"stdout\"":LO_CASE==4?"\"value\":\"stdin\"":"\"value\":\"start\"";
   if(slot_status!=1 || !contains(output,"Process.ExecutionFailed") || !contains(output,stage) || !contains(output,"cleanup_complete") || !contains(output,"\"value\":true") || contains(output,"process-argument-secret-sentinel") || contains(output,"cHJvY2Vzcy1hcmd1bWVudC1zZWNyZXQtc2VudGluZWw=")){write(2,output,(Size)count);return 26;}
  }
 }
 if(invalid||__atomic_load_n(&entered,__ATOMIC_SEQ_CST)!=1||__atomic_load_n(&joined,__ATOMIC_SEQ_CST)!=1||__atomic_load_n(&released,__ATOMIC_SEQ_CST)!=1)return 30;
 return lo_runtime_live_values()||lo_runtime_live_contexts()?31:0;
}
