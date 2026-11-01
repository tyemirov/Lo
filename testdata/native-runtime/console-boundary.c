#include "lo_context_options.h"
#include "lo_console.h"
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef struct {U32 status,code;const Value *value;} ResultSlot;
extern Context *lo_context_new(const ContextOptions *);
extern void lo_context_run(Context *,ResultSlot *);
extern void lo_context_cancel(Context *);
extern void lo_context_free(Context *);
extern void lo_value_release(const Value *);
extern U64 lo_runtime_live_contexts(void),lo_runtime_live_values(void);
static Context *active;
static unsigned calls,flushes,total,sequence;
static int invalid;
static int output(void *user,U32 destination,const unsigned char *bytes,__SIZE_TYPE__ length,__SIZE_TYPE__ *confirmed) {
 if(user!=(void *)42 || !length || (destination!=1 && destination!=2))invalid=1;
 ++calls;
 if(LO_CASE==3){*confirmed=0;if(calls==3)lo_context_cancel(active);return LO_CONSOLE_WOULD_BLOCK;}
 if(LO_CASE==6){*confirmed=length+1;return LO_CONSOLE_OK;}
 if(LO_CASE>=7){
  const char *expected=destination==1?"out":"err";
  if(length!=3 || bytes[0]!=expected[0] || bytes[1]!=expected[1] || bytes[2]!=expected[2])invalid=2;
  if(destination!=sequence+1)invalid=3;
  ++sequence;*confirmed=length;total+=length;return LO_CONSOLE_OK;
 }
 if(destination!=1 || bytes[0]!=(total==0?0xc3:total==1?0xa9:10))invalid=4;
 *confirmed=1;++total;return LO_CASE==1?LO_CONSOLE_CLOSED:LO_CONSOLE_OK;
}
static int flush(void *user,U32 destination){if(user!=(void *)42 || (destination!=1 && destination!=2))invalid=5;++flushes;return LO_CASE==2||LO_CASE==10||LO_CASE==15||LO_CASE==16?LO_CONSOLE_FAILED:LO_CONSOLE_OK;}
int main(void) {
 const ContextOptions options={100000,16ULL<<20,64ULL<<20,0,0,0,128};
 active=lo_context_new(&options);if(!active)return 1;
 U64 quota=LO_CASE==4?2:LO_CASE==12?5:1048576;
 if(!lo_context_set_console(active,LO_CASE==5?2:LO_CASE==13?1:3,(void *)42,output,flush,quota))return 2;
 if(lo_context_set_console(active,3,(void *)42,output,flush,quota))return 3;
 ResultSlot result={99,99,0};lo_context_run(active,&result);
 U32 expected=LO_CASE==1||LO_CASE==2?1:LO_CASE>=3&&LO_CASE<=6?2:0;
 if(result.status!=expected || (result.value!=0)!=(expected!=2))return 4;
 if(LO_CASE==14){
  if(calls||total||flushes||lo_context_set_console(active,3,(void *)42,output,flush,quota))return 26;
  int status=99;if(!lo_context_complete(active,result.value,&status)||status!=0)return 27;
  lo_value_release(result.value);lo_context_free(active);
  return lo_runtime_live_contexts()||lo_runtime_live_values()?28:0;
 }
 if(LO_CASE==0 && (total!=3 || calls!=3 || flushes!=1))return 5;
 if(LO_CASE==1 && (total!=1 || calls!=1 || flushes))return 6;
 if(LO_CASE==2 && (total!=3 || calls!=3 || flushes!=1))return 7;
 if(LO_CASE==3 && (calls!=3 || total || flushes))return 8;
 if((LO_CASE==4||LO_CASE==5) && (calls||total||flushes))return 9;
 if(lo_context_set_console(active,3,(void *)42,output,flush,quota))return 10;
 if(LO_CASE>=7) {
  if(total||calls||flushes)return 11;
  if(LO_CASE==11)lo_context_cancel(active);
  int status=99;int complete=lo_context_complete(active,result.value,&status);
  if(LO_CASE==10||LO_CASE==11||LO_CASE==12||LO_CASE==13||LO_CASE==15||LO_CASE==16){if(complete||status!=99)return 12;}else if(!complete||status!=(LO_CASE==7?0:LO_CASE==8?1:2))return 13;
  if((LO_CASE==10||LO_CASE==15||LO_CASE==16) && (total!=3||calls!=1||flushes!=1||sequence!=1))return 29;
  if((LO_CASE==11||LO_CASE==12||LO_CASE==13) && (total||calls||flushes))return 14;
  if(LO_CASE>=7 && LO_CASE<=9 && (total!=6||calls!=2||flushes!=2||sequence!=2))return 15;
  if(lo_context_complete(active,result.value,&status))return 16;
  ResultSlot after={99,99,0};lo_context_run(active,&after);if(after.status!=2||after.value)return 17;
 }
 if(result.value)lo_value_release(result.value);
 lo_context_free(active);
 if(invalid)return 18+invalid;
 if(lo_runtime_live_contexts()||lo_runtime_live_values())return 25;
 return 0;
}
