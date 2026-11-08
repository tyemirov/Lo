#include "lo_runtime.h"
#include "lo_process.h"
#include "capture.h"
typedef __UINTPTR_TYPE__ Thread;
extern void lo_signal_entry(Context *,const Value *const *,uint64_t,uint64_t,ResultSlot *);
extern void lo_commit(Context *,const Value *const *,uint64_t,uint64_t,ResultSlot *);
extern int lo_cli_main(int,const char *const *,const char *const *);
extern uint64_t lo_runtime_live_contexts(void),lo_runtime_live_values(void);
extern int getpid(void),kill(int,int),setpgid(int,int),waitpid(int,int *,int),flock(int,int),ftruncate(int,long);
extern unsigned alarm(unsigned);
extern int pthread_create(Thread *,const void *,void *(*)(void *),void *),pthread_join(Thread,void **);
extern Thread pthread_self(void);
extern int poll(void *,unsigned long,int),socket(int,int,int),connect(int,const void *,unsigned);
extern void *mmap(void *,__SIZE_TYPE__,int,int,int,long),*dlsym(void *,const char *);
extern int munmap(void *,__SIZE_TYPE__);
#ifdef __APPLE__
extern int *__error(void);
#define LO_ERRNO (*__error())
#define LO_BUSY 35
#else
extern int *__errno_location(void);
#define LO_ERRNO (*__errno_location())
#define LO_BUSY 11
#endif
#ifndef __APPLE__
extern int ioctl(int,unsigned long,...);
struct Address{unsigned short family,port;unsigned address;unsigned char zero[8];};
#else
struct Address{unsigned char length,family;unsigned short port;unsigned address;unsigned char zero[8];};
#endif
struct State{unsigned phase,signal_number,group,pid,port,ready,writer_entered,writer_pid,signal_seen,release_writer,owner_gate,release_owner,worker_started,worker_cleanup,release_worker,worker_finished,returned,failed,bridge_calls;};
static struct State *state;
static Thread owner;
static const char *image;
static void (*handlers[2])(int);
static unsigned load(unsigned *value){return __atomic_load_n(value,__ATOMIC_SEQ_CST);}
static void store(unsigned *value,unsigned next){__atomic_store_n(value,next,__ATOMIC_SEQ_CST);}
static int has(const void *pointer,__SIZE_TYPE__ size,const char *text){const unsigned char *bytes=pointer;for(__SIZE_TYPE__ start=0;start<size;++start){__SIZE_TYPE__ n=0;while(text[n]&&start+n<size&&bytes[start+n]==(unsigned char)text[n])++n;if(!text[n])return 1;}return 0;}
static struct State *mapping(void){
 if(state)return state;
 int file=open(lo_capture_path,2);if(file<0)return 0;
 void *memory=mmap(0,sizeof(struct State),3,1,file,0);close(file);
 if(memory==(void *)-1)return 0;state=memory;return state;
}
static void received(int number){
 if(state)store(&state->signal_seen,1);
 void (*handler)(int)=handlers[number==2?0:1];if(handler)handler(number);
}
void *signal(int number,void *handler){
 void *(*delegate)(int,void *)=(void *(*)(int,void *))dlsym((void *)-1,"signal");
 if(!delegate)return (void *)-1;
 if((number==2||number==15)&&handler!=(void *)0&&handler!=(void *)1){handlers[number==2?0:1]=(void (*)(int))handler;return delegate(number,(void *)received);}
 return delegate(number,handler);
}
long write(int descriptor,const void *bytes,__SIZE_TYPE__ size){
 long (*delegate)(int,const void *,__SIZE_TYPE__)=(long (*)(int,const void *,__SIZE_TYPE__))dlsym((void *)-1,"write");
 if(!delegate)return -1;
 struct State *shared=mapping();
 if(shared && (descriptor==1||descriptor==2)){
  if(has(bytes,size,"\"status\":\"ready\"")){
   const unsigned char *text=bytes;
   for(__SIZE_TYPE__ i=0;i+12<size;++i)if(has(text+i,10,"127.0.0.1:")){
    __SIZE_TYPE__ next=i;while(next<size&&text[next]!=':')++next;++next;
    unsigned port=0;while(next<size&&text[next]>='0'&&text[next]<='9')port=port*10+text[next++]-'0';
    shared->port=port;break;
   }
   store(&shared->ready,1);
  }
  if(shared->phase<2&&has(bytes,size,"\"event\":\"console\"")){
   store(&shared->writer_pid,(unsigned)getpid());store(&shared->writer_entered,1);
   while(!load(&shared->release_writer))poll(0,0,1);
  }
 }
 return delegate(descriptor,bytes,size);
}
void lo_entry(Context *context,const Value *const *arguments,uint64_t count,uint64_t site,ResultSlot *result){lo_signal_entry(context,arguments,count,site,result);}
void lo_signal_commit(Context *context,const Value *const *arguments,uint64_t count,uint64_t site,ResultSlot *result){
 lo_commit(context,arguments,count,site,result);
 unsigned calls=__atomic_add_fetch(&state->bridge_calls,1,__ATOMIC_SEQ_CST);
 if((state->phase==0&&calls==2)||(state->phase==1&&pthread_self()!=owner)){
  store(&state->owner_gate,1);while(!load(&state->release_owner))poll(0,0,1);
 }
}
int lo_native_process_execute(const char *launcher,int embedded,const char *directory,const unsigned char *configuration,uintptr_t configuration_size,const LoProcessRequest *request,LoProcessResult *result){
 (void)launcher;(void)embedded;(void)directory;(void)configuration;(void)configuration_size;
 store(&state->worker_started,1);while(!request->stop(request->stop_token))poll(0,0,1);
 store(&state->worker_cleanup,1);while(!load(&state->release_worker))poll(0,0,1);
 result->stage=7;result->class_id=LO_PROCESS_CANCELLED;result->started=1;result->cleanup_complete=1;result->termination_kind=2;result->termination_value=15;
 store(&state->worker_finished,1);return 0;
}
static int busy(int expected){int file=open(image,0);if(file<0)return 1;int result=flock(file,6),error=LO_ERRNO;if(close(file))return 1;return expected?result!=-1||error!=LO_BUSY:result!=0;}
static int wait_for(unsigned *value){for(unsigned n=0;n<5000;++n){if(load(value))return 0;poll(0,0,1);}return 1;}
static void fail(unsigned code){store(&state->failed,code);store(&state->release_writer,1);store(&state->release_owner,1);store(&state->release_worker,1);kill(getpid(),15);}
static void *control(void *unused){
 (void)unused;int client=-1;
 if(state->phase==1){
  if(wait_for(&state->ready)||!state->port){fail(1);return 0;}
  struct Address address={0};
#ifdef __APPLE__
  address.length=16;
#endif
  address.family=2;address.port=(unsigned short)((state->port<<8)|(state->port>>8));address.address=0x0100007f;
  client=socket(2,1,0);if(client<0||connect(client,&address,16)){fail(2);return 0;}
  const char request[]="GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n";
  if(write(client,request,sizeof(request)-1)!=(long)sizeof(request)-1){fail(3);return 0;}
 }
 unsigned *entered=state->phase==2?&state->worker_started:&state->writer_entered;
 if(wait_for(entered)){fail(4);return 0;}
 int target=state->group?-(int)state->pid:(int)state->pid;
 if(kill(target,(int)state->signal_number)||wait_for(&state->signal_seen)){fail(5);return 0;}
 if(load(&state->returned)||busy(1)){fail(6);return 0;}
 if(state->phase==2){
  if(wait_for(&state->worker_cleanup)){fail(7);return 0;}
  poll(0,0,1100);
  if(load(&state->returned)||load(&state->worker_finished)||busy(1)){fail(8);return 0;}
  store(&state->release_worker,1);
 }else{
  store(&state->release_writer,1);
  if(wait_for(&state->owner_gate)){fail(9);return 0;}
  if(load(&state->returned)||busy(1)){fail(10);return 0;}
  store(&state->release_owner,1);
 }
 if(client>=0){
  char response[4096];long count=read(client,response,sizeof(response));close(client);
  if(!state->group && (count<=0||!has(response,(__SIZE_TYPE__)count,"200 OK"))){fail(11);return 0;}
 }
 return 0;
}
int main(int argc,const char **argv){
 (void)argc;alarm(15);if(setpgid(0,0))return 1;
#ifdef __APPLE__
 image=argv[0];
#else
 image="/probe";
#endif
 int file=open(lo_capture_path,2);if(file<0||ftruncate(file,sizeof(struct State)))return 2;
 state=mmap(0,sizeof(struct State),3,1,file,0);close(file);if(state==(void *)-1)return 3;
 for(unsigned n=0;n<sizeof(*state);++n)((unsigned char *)state)[n]=0;
 state->phase=LO_CASE/4;state->signal_number=LO_CASE%4<2?2:15;state->group=LO_CASE%2;state->pid=(unsigned)getpid();owner=pthread_self();
#ifndef __APPLE__
 if(state->phase==1){int control=socket(2,2,0);unsigned char request[40]={0};request[0]='l';request[1]='o';if(control<0||ioctl(control,0x8913UL,request))return 4;request[16]|=1;if(ioctl(control,0x8914UL,request)||close(control))return 5;}
#endif
 Thread controller;if(pthread_create(&controller,0,control,0))return 6;
 const char *arguments[]={"signal-lifetime",state->phase==1?"serve":"run","--format","json","--lock-entry","--listen","0"};
 const char *process_arguments[]={"signal-lifetime","run","--format","json","--lock-entry","--process-allow",@PROCESS_GRANT@,"--process-read-root",@PROCESS_ROOT@,"--process-launcher",@PROCESS_LAUNCHER@};
 int status=state->phase==2?lo_cli_main(11,process_arguments,0):lo_cli_main(state->phase==1?7:5,arguments,0);
 store(&state->returned,1);if(pthread_join(controller,0))return 7;
 if(state->failed)return 30+(int)state->failed;
 if(status!=(state->phase==1&&!state->group?0:2))return 50+status;
 if(busy(0)||lo_runtime_live_contexts()||lo_runtime_live_values())return 60;
 if(state->phase==0&&state->bridge_calls!=2)return 61;
 if(state->phase==2&&!state->worker_finished)return 62;
 if(state->writer_pid){int child_status=0;if(waitpid((int)state->writer_pid,&child_status,1)!=-1||LO_ERRNO!=10)return 63;}
 if(munmap(state,sizeof(*state))||unlink(lo_capture_path))return 64;return 0;
}
