#include "lo_process.h"
typedef __SIZE_TYPE__ Size;typedef __UINT64_TYPE__ U64;typedef __UINTPTR_TYPE__ Word;
struct Event {Word ident;short filter;unsigned short flags;unsigned int fflags;long data;void *user;};
extern int *__error(void);extern int open(const char*,int,...);extern int dup2(int,int);extern long write(int,const void*,Size);extern void *dlsym(void*,const char*);

static unsigned observations,signals,waits;
int posix_spawn(int *pid,const char *path,const void *actions,const void *attributes,char *const *argv,char *const *env){(void)path;(void)actions;(void)attributes;(void)argv;(void)env;*pid=4242;return 0;}
int kevent(int queue,const struct Event *change,int changes,struct Event *events,int capacity,const void *timeout){(void)queue;(void)change;(void)capacity;(void)timeout;if(changes)return 0;++observations;if(observations==1){if(LO_CASE==0){*__error()=5;return -1;}events[0]=(struct Event){4242,-5,0x4000,0,0,0};return 1;}return 0;}
int kill(int pid,int signal){if(pid!=4242||signal!=9)return -1;++signals;return 0;}
int waitpid(int pid,int *state,int flags){(void)flags;++waits;if(pid==4242&&signals){*state=7<<8;return pid;}if(waits>3){*__error()=10;return -1;}return 0;}
#if LO_CASE==2
static int replacement=-1;static unsigned attempted;
int close(int descriptor){int(*real_close)(int)=(int(*)(int))dlsym((void*)-1,"close");if(replacement<0){if(real_close(descriptor))return -1;replacement=open("replacement",2|0x200|0x400,0666);if(replacement<0)return -1;if(replacement!=descriptor){if(dup2(replacement,descriptor)<0)return -1;real_close(replacement);replacement=descriptor;}attempted=1;*__error()=5;return -1;}if(descriptor==replacement)++attempted;return real_close(descriptor);}
#endif
static int cancelled(const void *token){(void)token;return 0;}
int main(void){
 LoProcessResult result={0};
 LoProcessRequest request={0};request.timeout_ms=1000;request.cleanup_ms=1000;request.output_bytes=16;request.stop=cancelled;
 lo_native_process_execute("/unused",0,".",0,0,&request,&result);
#if LO_CASE==2
 if(result.cleanup_complete||!result.ownership||!(result.cleanup_failures&1)||attempted!=1||replacement<0)return 30;
 if(write(replacement,"retained",8)!=8)return 31;
#else
 if(result.stage!=5||result.class_id!=2||!result.started||!result.cleanup_complete||result.ownership||signals!=1||result.termination_kind!=1||result.termination_value!=7)return 20;
#endif
 return 0;
}
