#include "capture.h"
typedef __UINT64_TYPE__ U64;
typedef __UINTPTR_TYPE__ Thread;
extern int lo_cli_main(int,const char *const *,const char *const *);
extern U64 lo_runtime_live_contexts(void),lo_runtime_live_values(void);
extern void *dlsym(void *,const char *);
extern int getpid(void),getppid(void),kill(int,int),poll(void *,unsigned long,int);
extern int pthread_create(Thread *,const void *,void *(*)(void *),void *);
extern int pthread_join(Thread,void **);
static long (*real_write)(int,const void *,LoCaptureSize);
static int timer_error;
static int contains(const void *pointer,LoCaptureSize length,const char *needle) {
 const unsigned char *bytes=pointer;
 for(LoCaptureSize index=0;index<length;index++) {
  LoCaptureSize offset=0;
  while(needle[offset] && index+offset<length && bytes[index+offset]==(unsigned char)needle[offset])++offset;
  if(!needle[offset])return 1;
 }
 return 0;
}
long write(int descriptor,const void *bytes,LoCaptureSize length) {
 if(!real_write)real_write=(long (*)(int,const void *,LoCaptureSize))dlsym((void *)-1,"write");
 if(!real_write)return -1;
 long count=real_write(descriptor,bytes,length);
 if(LO_CASE==0 && descriptor==1 && count>0 && contains(bytes,(LoCaptureSize)count,"\"status\":\"ready\"")) {
  if(kill(getppid(),15))return -1;
  poll(0,0,50);
 }
 return count;
}
static void *later_stop(void *unused) {
 (void)unused;
 int input=open(lo_capture_path,0);
 if(input<0){timer_error=1;kill(getpid(),15);return 0;}
 char bytes[4096];unsigned attempts;
 for(attempts=0;attempts<5000;attempts++) {
  if(lseek(input,0,0)<0){timer_error=2;break;}
  long count=read(input,bytes,sizeof(bytes));
  if(count>0 && contains(bytes,(LoCaptureSize)count,"\"status\":\"ready\""))break;
  poll(0,0,1);
 }
 close(input);
 if(attempts==5000)timer_error=3;
 poll(0,0,1500);
 if(kill(getpid(),15))timer_error=4;
 return 0;
}
int main(void) {
 int capture=lo_capture_start();if(capture)return capture;
 Thread timer;
 if(LO_CASE==1 && pthread_create(&timer,0,later_stop,0))return lo_capture_cleanup(31);
 const char *args[]={"ready-cancellation","serve","--listen","0","--format","json"};
 int status=lo_cli_main(6,args,0);
 if(LO_CASE==1 && pthread_join(timer,0))return lo_capture_cleanup(32);
 capture=lo_capture_finish();if(capture)return capture;
 if(status){write(2,lo_capture_bytes,lo_capture_length);return status;}
 if(timer_error)return 33;
 if(!contains(lo_capture_bytes,lo_capture_length,"\"status\":\"ready\"") || !contains(lo_capture_bytes,lo_capture_length,"\"status\":\"stopped\"") || contains(lo_capture_bytes,lo_capture_length,"runtime.cancelled"))return 34;
 if(lo_runtime_live_contexts() || lo_runtime_live_values())return 35;
 return 0;
}
