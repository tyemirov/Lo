#include "capture.h"
#include <stddef.h>
#include <stdint.h>
extern int lo_cli_main(int,const char *const *,const char *const *);
extern uint64_t lo_runtime_live_contexts(void);
extern uint64_t lo_runtime_live_values(void);
extern uint64_t lo_runtime_live_storages(void);
extern int fcntl(int,int,...);
extern int dup2(int,int);
extern int close(int);
extern int open(const char *,int,...);
extern long read(int,void *,size_t);
static unsigned opens;
/* Only device discovery is controlled. The production Metal factory and bridge run. */
void *MTLCreateSystemDefaultDevice(void){opens++;return 0;}
static int contains(const char *text,const char *needle){
 for(;*text;text++){size_t n=0;while(needle[n]&&text[n]==needle[n])n++;if(!needle[n])return 1;}return 0;
}
int main(void){
 int capture_error=lo_capture_start();if(capture_error)return capture_error;
 const char *arguments[]={"fixture","run","--allow-device","metal:0","--write-root",".","--format","json"};
 int status=lo_cli_main(8,arguments,0);
 capture_error=lo_capture_finish();if(capture_error)return capture_error;char *bytes=lo_capture_bytes;size_t length=lo_capture_length;
 if(opens!=1||status!=1||!contains(bytes,"Device.Unavailable")||!contains(bytes,"no system Metal device")||contains(bytes,"\"result\""))return 20;
 int later=open("later",0);if(later>=0){close(later);return 21;}
 if(lo_runtime_live_contexts()||lo_runtime_live_values()||lo_runtime_live_storages())return 22;
 return 0;
}
