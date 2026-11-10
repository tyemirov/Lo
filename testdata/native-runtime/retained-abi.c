#include "lo_runtime.h"
#include "lo_console.h"
typedef __UINT64_TYPE__ U64;
typedef __UINTPTR_TYPE__ Thread;
typedef __SIZE_TYPE__ Size;
_Static_assert(sizeof(ResultSlot)==16 && offsetof(ResultSlot,value)==8,"native result ABI");
_Static_assert(sizeof(ContextOptions)==64,"native context options ABI");
extern U64 lo_runtime_live_programs(void),lo_runtime_live_contexts(void),lo_runtime_live_values(void);
extern int lo_cli_main(int,const char *const *,const char *const *);
extern int pthread_create(Thread *,const void *,void *(*)(void *),void *),pthread_join(Thread,void **);
extern int poll(void *,unsigned long,int),getpid(void),kill(int,int);
extern unsigned alarm(unsigned);
#define OPTIONS (&(const ContextOptions){100000,16ULL<<20,64ULL<<20,0,0,0,128,256})
static int contains(const unsigned char *bytes,Size size,const char *text){for(Size i=0;i<size;++i){Size n=0;while(text[n] && i+n<size && bytes[i+n]==(unsigned char)text[n])++n;if(!text[n])return 1;}return 0;}
#if LO_CASE <= 1
extern int lo_context_set_host_invocation(Context *,const unsigned char *,Size);
extern char *getcwd(char *,Size);
static void append32(unsigned char *bytes,Size *offset,unsigned value){for(unsigned shift=4;shift;--shift)bytes[(*offset)++]=(unsigned char)(value>>(8*(shift-1)));}
static void append_text(unsigned char *bytes,Size *offset,const char *text){Size length=0;while(text[length])++length;append32(bytes,offset,(unsigned)length+1);for(Size n=0;n<=length;++n)bytes[(*offset)++]=(unsigned char)text[n];}
struct Peer { const Program *program;Context *context;unsigned index,calls,bytes;int failed; };
static unsigned entered;
static int output(void *opaque,unsigned stream,const unsigned char *bytes,Size size,Size *confirmed){
 struct Peer *peer=opaque;(void)stream;++peer->calls;
 const char *expected=peer->index==0?"left\n":"right\n";Size length=peer->index==0?5:6;
 if(size!=length)peer->failed=5;else for(Size n=0;n<size;++n)if(bytes[n]!=(unsigned char)expected[n])peer->failed=5;
 if(peer->calls==1){
  __atomic_fetch_or(&entered,1U<<peer->index,__ATOMIC_SEQ_CST);
  while(__atomic_load_n(&entered,__ATOMIC_SEQ_CST)!=3)poll(0,0,1);
 }
 if(peer->index==0){lo_context_cancel(peer->context);*confirmed=0;return LO_CONSOLE_WOULD_BLOCK;}
 *confirmed=size;peer->bytes+=(unsigned)size;return LO_CONSOLE_OK;
}
static int flush(void *opaque,unsigned stream){(void)opaque;(void)stream;return LO_CONSOLE_OK;}
static void *run_peer(void *opaque){
 struct Peer *peer=opaque;peer->context=lo_context_from_program(peer->program,OPTIONS);
 if(!peer->context){peer->failed=1;return 0;}
 char directory[4096];unsigned char frame[8400];Size offset=0;
 if(!getcwd(directory,sizeof(directory))){peer->failed=6;return 0;}
 append32(frame,&offset,1);append_text(frame,&offset,peer->index==0?"left":"right");
 append_text(frame,&offset,directory);
 append32(frame,&offset,0);append32(frame,&offset,0);append32(frame,&offset,0);append32(frame,&offset,0);
 if(!lo_context_set_host_invocation(peer->context,frame,offset)){peer->failed=7;return 0;}
 for(Size n=0;n<offset;++n)frame[n]='x';
 if(!lo_context_set_console(peer->context,3,peer,output,flush,1024)){peer->failed=2;return 0;}
 ResultSlot result={99,99,0};lo_context_run(peer->context,&result);
 if(result.status!=(peer->index==0?2U:0U)||(result.value!=0)!=(peer->index==1))peer->failed=3;
 if(result.value)lo_value_release(result.value);
 if(!lo_context_close(peer->context))peer->failed=4;
 lo_context_free(peer->context);return 0;
}
int main(void){
 alarm(10);
 if(LO_CASE==0){
  const char *args[]={"abi-host","run","--max-steps","1","--format","json"};
  if(lo_cli_main(6,args,0)!=2)return 10;
  if(kill(getpid(),2))return 11;
 }
 const Program *program=lo_program_new();if(!program)return 12;
 struct Peer peers[2]={{program,0,0,0,0,0},{program,0,1,0,0,0}};
 Thread threads[2];
 if(pthread_create(&threads[0],0,run_peer,&peers[0])||pthread_create(&threads[1],0,run_peer,&peers[1]))return 13;
 if(pthread_join(threads[0],0)||pthread_join(threads[1],0))return 14;
 lo_program_free(program);
 if(peers[0].failed||peers[1].failed)return 20+peers[0].failed+peers[1].failed;
 if(peers[0].calls!=1||peers[1].bytes!=6||entered!=3)return 30;
 return lo_runtime_live_programs()||lo_runtime_live_contexts()||lo_runtime_live_values()?31:0;
}
#else
static int diagnostic(Context *context,const ResultSlot *result,const char *expected){
 unsigned char bytes[4098];for(unsigned i=0;i<sizeof(bytes);++i)bytes[i]=0xa5;
 Size size=99;if(lo_context_diagnostic(context,result,0,0,&size)!=LO_DIAGNOSTIC_SHORT_BUFFER||size==0||size>4096)return 40;
 Size again=0;if(lo_context_diagnostic(context,result,bytes+1,size-1,&again)!=LO_DIAGNOSTIC_SHORT_BUFFER||again!=size)return 41;
 for(unsigned i=0;i<sizeof(bytes);++i)if(bytes[i]!=0xa5)return 42;
 if(lo_context_diagnostic(context,result,bytes+1,size,&again)!=LO_DIAGNOSTIC_OK||again!=size||bytes[0]!=0xa5||bytes[size+1]!=0xa5)return 43;
 if(!contains(bytes+1,size,expected)||bytes[1]!='{'||bytes[size]!='}')return 44;
 Size invalid=99;if(lo_context_diagnostic(0,result,0,0,&invalid)!=LO_DIAGNOSTIC_INVALID||invalid!=0)return 45;
 if(lo_context_diagnostic(context,result,bytes,1,0)!=LO_DIAGNOSTIC_INVALID)return 46;
 return 0;
}
static int fail_flush(void *opaque,unsigned stream){(void)opaque;(void)stream;return LO_CONSOLE_FAILED;}
static int accept(void *opaque,unsigned stream,const unsigned char *bytes,Size size,Size *confirmed){(void)opaque;(void)stream;(void)bytes;*confirmed=size;return LO_CONSOLE_OK;}
struct WrongThread {Context *context;int failed;};
static void *wrong_thread(void *opaque){
 struct WrongThread *state=opaque;Size required=99;unsigned char byte=0xa5;
 if(lo_context_diagnostic(state->context,0,&byte,1,&required)!=LO_DIAGNOSTIC_WRONG_THREAD||required||byte!=0xa5)state->failed=1;
 ResultSlot result={99,99,(const Value *)1};lo_context_run(state->context,&result);
 if(result.status!=2||result.code!=3||result.value)state->failed=2;
 return 0;
}
int main(void){
 const Program *program=lo_program_new();if(!program)return 1;
 ContextOptions options=*OPTIONS;if(LO_CASE==4)options.steps=1;
 Context *context=lo_context_from_program(program,&options);if(!context)return 2;
 lo_program_free(program);
 if(LO_CASE==5&&!lo_context_set_console(context,3,0,accept,fail_flush,1024))return 3;
 if(LO_CASE==6){
  struct WrongThread state={context,0};Thread thread;
  if(pthread_create(&thread,0,wrong_thread,&state)||pthread_join(thread,0)||state.failed)return 4;
 }
 ResultSlot result={99,99,0};lo_context_run(context,&result);
 int checked=0;
 if(LO_CASE==2||LO_CASE==6){
  if(result.status||!result.value)return 5;
  Size required=99;unsigned char byte=0xa5;
  if(lo_context_diagnostic(context,&result,&byte,1,&required)!=LO_DIAGNOSTIC_NONE||required||byte!=0xa5)return 6;
  lo_value_retain(result.value);
  int status=99;if(!lo_context_complete(context,result.value,&status)||status)return 7;
  if(lo_context_complete(context,result.value,&status))return 8;
  lo_context_free(context);context=0;
  lo_value_release(result.value);lo_value_release(result.value);result.value=0;
 }else if(LO_CASE==3||LO_CASE==7){
  if(result.status!=1||!result.value)return 9;
  checked=diagnostic(context,&result,"Math.DivideByZero");if(checked)return checked;
  if(LO_CASE==7){
   if(diagnostic(context,&result,"\"value\":\"Outer\"")||diagnostic(context,&result,"\"value\":\"Inner\""))return 50;
   lo_value_release(result.value);result.value=0;
   ResultSlot second={99,99,0};lo_context_run(context,&second);
   if(second.status!=1||!second.value)return 51;
   if(diagnostic(context,&second,"\"value\":\"Outer\""))return 52;
   lo_value_release(second.value);
  }
 }else if(LO_CASE==4){
  if(result.status!=2||result.value)return 10;
  checked=diagnostic(context,&result,"runtime.limit");if(checked)return checked;
 }else if(LO_CASE==5){
  if(result.status||!result.value)return 11;
  int status=99;if(lo_context_complete(context,result.value,&status)||status!=99)return 12;
  checked=diagnostic(context,0,"runtime.host-failure");if(checked)return checked;
 }else if(LO_CASE==8){
  if(result.status!=1||!result.value)return 13;
  unsigned char byte=0xa5;Size required=99;
  if(lo_context_diagnostic(context,&result,&byte,1,&required)!=LO_DIAGNOSTIC_TOO_LARGE||required||byte!=0xa5)return 14;
 }
 if(result.value)lo_value_release(result.value);
 if(context){if(LO_CASE!=5&&!lo_context_close(context))return 15;lo_context_free(context);}
 return lo_runtime_live_programs()||lo_runtime_live_contexts()||lo_runtime_live_values()?16:0;
}
#endif
