#include "lo_runtime.h"
#include "lo_program_lock.h"
#include "lo_console.h"
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef __SIZE_TYPE__ Size;
extern int lo_cli_main(int,const char *const *,const char *const *);
extern int lo_boolean_value(const Value *);
extern Context *lo_context_process_peer(Context *);
extern U64 lo_runtime_live_contexts(void),lo_runtime_live_values(void);
extern int open(const char *,int,...),close(int),fcntl(int,int,...),fork(void),execve(const char *,char *const *,char *const *),waitpid(int,int *,int),rename(const char *,const char *),symlink(const char *,const char *),linkat(int,const char *,int,const char *,int);
extern char *getcwd(char *,Size);
extern void *dlsym(void *,const char *);
extern void _exit(int);
#ifdef __APPLE__
extern int *__error(void);
#define ERRNO (*__error())
#else
extern int *__errno_location(void);
#define ERRNO (*__errno_location())
#endif
static int lease_fd=-1,attempt_fd=-1,armed,injections,writes;
static int (*real_flock)(int,int);
int flock(int fd,int operation){
 if(!real_flock)real_flock=(int (*)(int,int))dlsym((void *)-1,"flock");
 if(!real_flock){ERRNO=5;return -1;}
 int result=real_flock(fd,operation);
 if(operation==6 && result==0 && lease_fd<0)lease_fd=fd;
 if((LO_CASE==10||LO_CASE==19) && operation==6 && result!=0){attempt_fd=fd;armed=1;}
 return result;
}
int close(int fd){
 int (*delegate)(int)=(int (*)(int))dlsym((void *)-1,"close");
 if(!delegate){ERRNO=5;return -1;}
 int result=delegate(fd);
 if(result==0 && armed && fd==((LO_CASE==10||LO_CASE==19)?attempt_fd:lease_fd)){armed=0;++injections;ERRNO=5;return -1;}
 return result;
}
typedef __UINTPTR_TYPE__ Thread;
typedef struct {int stage,class_id,started,cleanup_complete,termination_kind,termination_value;Size stdin_written,stdout_size,stderr_size;unsigned cleanup_failures;Size ownership;} NativeResult;
typedef int (*Cancel)(const void *);
typedef void (*Driver)(Size,const unsigned char *,Size,const unsigned char *,Size,unsigned char *,unsigned char *,Size,Cancel,const void *,U64,NativeResult *);
extern int lo_context_process_driver(Context *,Driver,Size),lo_context_process_driver_limits(Context *,U64,U64,Size);
extern int pthread_create(Thread *,const void *,void *(*)(void *),void *),pthread_join(Thread,void **),poll(void *,unsigned long,int);
extern unsigned alarm(unsigned);
static int released,settled,finished,observer_failed;
static const char *observer_path;
static int probe(const char *,int);
static void delayed(Size token,const unsigned char *frame,Size frame_size,const unsigned char *input,Size input_size,unsigned char *out,unsigned char *err,Size capacity,Cancel cancelled,const void *cancel,U64 timeout,NativeResult *result){
 (void)token;(void)frame;(void)frame_size;(void)input;(void)input_size;(void)out;(void)err;(void)capacity;(void)cancelled;(void)cancel;(void)timeout;
 if(LO_CASE==14)return;
 while(!__atomic_load_n(&released,__ATOMIC_SEQ_CST))poll(0,0,1);
 result->started=1;result->termination_kind=1;result->termination_value=0;result->cleanup_complete=1;
 __atomic_store_n(&settled,1,__ATOMIC_SEQ_CST);
}
static void *observe_cleanup(void *unused){
 (void)unused;poll(0,0,100);
 if(__atomic_load_n(&finished,__ATOMIC_SEQ_CST)||__atomic_load_n(&settled,__ATOMIC_SEQ_CST)||probe(observer_path,1))observer_failed=1;
 __atomic_store_n(&released,1,__ATOMIC_SEQ_CST);return 0;
}
static int equal(const char *left,const char *right){while(*left && *left==*right){++left;++right;}return *left==*right;}
static unsigned length(const char *text){unsigned n=0;while(text[n])++n;return n;}
static int output(void *user,U32 stream,const unsigned char *bytes,Size count,Size *confirmed){(void)user;(void)stream;(void)bytes;++writes;*confirmed=count;return LO_CONSOLE_OK;}
static int flush(void *user,U32 stream){(void)user;(void)stream;return LO_CONSOLE_OK;}
static int probe(const char *path,int expected_busy){
 int file=open(path,0);if(file<0)return 1;
 int result=real_flock(file,6);
 int wrong=expected_busy?result==0:result!=0;
 if(close(file))return 2;
 return wrong?3:0;
}
int main(int argc,const char **argv){
 if(argc==3 && equal(argv[1],"--probe-lock-fd")){
  int fd=0;for(const char *p=argv[2];*p;++p)fd=fd*10+*p-'0';
  return fcntl(fd,1)>=0?51:0;
 }
 real_flock=(int (*)(int,int))dlsym((void *)-1,"flock");if(!real_flock)return 1;
 char path[4096];if(!getcwd(path,sizeof(path)))return 2;
 unsigned count=length(path);if(count+6>=sizeof(path))return 2;if(count>1)path[count++]='/';
 for(unsigned index=0;index<5;++index)path[count++]= "lock"[index];
 
#ifdef __APPLE__
 const int at_current=-2;const char *image=argv[0],*replacement="program.ll";
#else
 const int at_current=-100;const char *image="/probe",*replacement="/arguments";
#endif
 if(linkat(at_current,image,at_current,path,0))return 3;
 if(LO_CASE==19){const char *arguments[]={"forged-entry","run","--lock-entry","--format","json"};int status=lo_cli_main(5,arguments,0);return status!=2||injections!=1?79:0;}
 const ContextOptions options={100000,16ULL<<20,64ULL<<20,0,0,0,128};
 Context *context=lo_context_new(&options);if(!context)return 4;
 if(LO_CASE==16){Context *peer=lo_context_process_peer(context);if(!peer)return 70;if(lo_context_set_program_entry(context,(const unsigned char *)path,length(path))||lo_context_set_lock_files(context,0,0,0)||lo_context_set_program_entry(peer,(const unsigned char *)path,length(path)))return 71;lo_context_free(peer);}
 const char *injected=path;char alias[4112];
 if(LO_CASE==9){unsigned i=0;for(;path[i];++i)alias[i]=path[i];const char *tail="-alias";for(unsigned j=0;j<7;++j)alias[i+j]=tail[j];if(symlink(path,alias))return 50;injected=alias;}
 if(LO_CASE!=0&&LO_CASE!=17){
  if(!lo_context_set_program_entry(context,(const unsigned char *)injected,length(injected)))return 5;
  if(lo_context_set_program_entry(context,(const unsigned char *)path,length(path)))return 6;
 }
 if(LO_CASE==11||LO_CASE==12||LO_CASE==15){
  unsigned i=0;for(;path[i];++i)alias[i]=path[i];const char *tail="-original";for(unsigned j=0;j<10;++j)alias[i+j]=tail[j];
  if(rename(path,alias))return 54;if(linkat(at_current,replacement,at_current,path,0))return 55;
  const char *paths[]={path};
  if(lo_context_set_lock_files(context,LO_CASE!=11?paths:0,LO_CASE!=11?1:0,LO_CASE==15?0:1))return 53;
  if(!lo_context_close(context))return 56;lo_context_free(context);
  return lo_runtime_live_contexts()||lo_runtime_live_values()?57:0;
 }
 if(LO_CASE>=2&&LO_CASE!=17){
  if(!lo_context_set_lock_files(context,0,0,1))return 7;
  if(lo_context_set_lock_files(context,0,0,1))return 8;
 }
 if(LO_CASE==5 && !lo_context_set_console(context,3,0,output,flush,1048576))return 9;
 if(LO_CASE==13||LO_CASE==14){alarm(5);if(!lo_context_process_driver(context,delayed,0)||!lo_context_process_driver_limits(context,20,20,1024))return 60;}
 ResultSlot result={99,99,0};lo_context_run(context,&result);
 if(LO_CASE==13||LO_CASE==14){
  if(result.status!=2||result.value||lease_fd<0||probe(path,1))return 61;
  Thread observer=0;observer_path=path;
  if(LO_CASE==13 && pthread_create(&observer,0,observe_cleanup,0))return 62;
  if(lo_context_close(context))return 63;
  __atomic_store_n(&finished,1,__ATOMIC_SEQ_CST);
  if(LO_CASE==13 && (pthread_join(observer,0)||observer_failed||!settled))return 64;
  lo_context_free(context);
  if(probe(path,LO_CASE==14))return 65;
  if(lo_runtime_live_contexts()||lo_runtime_live_values())return 66;
  return 0;
 }
 if(LO_CASE==18){
  if(result.status||!result.value)return 75;
  U64 before=lo_runtime_live_values();ResultSlot second={99,99,0};lo_context_run(context,&second);
  if(second.status||!second.value||lo_runtime_live_values()!=before+1)return 76;
  lo_value_release(result.value);lo_value_release(second.value);
  if(!lo_context_close(context))return 77;lo_context_free(context);
  return lo_runtime_live_contexts()||lo_runtime_live_values()?78:0;
 }
 if(LO_CASE==17){if(result.status||!result.value)return 72;lo_value_release(result.value);if(lo_context_set_program_entry(context,(const unsigned char *)path,length(path))||lo_context_set_lock_files(context,0,0,0)||!lo_context_close(context))return 73;lo_context_free(context);return lo_runtime_live_contexts()||lo_runtime_live_values()?74:0;}
 U32 expected=LO_CASE==10?2:LO_CASE==3||LO_CASE==6?1:0;
 if(result.status!=expected || (expected==2?result.value!=0:result.value==0))return 10;
 if(LO_CASE<=2||LO_CASE==4||LO_CASE==7||LO_CASE==8||LO_CASE==9||LO_CASE==16){if(!lo_boolean_value(result.value))return 11;}
 if(lo_context_set_program_entry(context,(const unsigned char *)path,length(path)))return 12;
 if(LO_CASE>=2){if(lease_fd<0||probe(path,1))return 13;}
 if(LO_CASE==7){
  char digits[16],number[16];unsigned n=0,i=0,value=(unsigned)lease_fd;
  do{digits[n++]=(char)('0'+value%10);value/=10;}while(value);while(n)number[i++]=digits[--n];number[i]=0;
#ifdef __APPLE__
  const char *executable=argv[0];
#else
  const char *executable="/probe";
#endif
  char *arguments[]={(char *)executable,"--probe-lock-fd",number,0};
  int child=fork();if(child<0)return 14;if(!child){char *environment[]={0};execve(executable,arguments,environment);_exit(52);}
  int status=0;if(waitpid(child,&status,0)!=child || status!=0)return 15;
 }
 if(LO_CASE==4)lo_context_cancel(context);
 if(LO_CASE==5){
  armed=1;int status=99;
  if(lo_context_complete(context,result.value,&status)||status!=99||writes||injections!=1)return 16;
  if(lo_context_close(context))return 17;
 }else{
  lo_value_release(result.value);result.value=0;
  if(LO_CASE>=2 && probe(path,1))return 18;
  if(LO_CASE==6)armed=1;
  if(LO_CASE==8){lo_context_free(context);context=0;}
  else {
   int success=lo_context_close(context);
   if(success!=(LO_CASE!=6)||injections!=(LO_CASE==6||LO_CASE==10))return 19;
   if(lo_context_close(context)!=success)return 20;
  }
 }
 if(result.value)lo_value_release(result.value);
 if(context)lo_context_free(context);
 if(LO_CASE>=2 && probe(path,0))return 21;
 if(lo_runtime_live_contexts()||lo_runtime_live_values())return 22;
 return 0;
}
