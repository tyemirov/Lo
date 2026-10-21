#include <stddef.h>
#include <stdint.h>
extern int lo_cli_main(int,const char *const *,const char *const *);
extern uint64_t lo_runtime_live_contexts(void);
extern uint64_t lo_runtime_live_values(void);
extern uint64_t lo_runtime_live_storages(void);
extern int pipe(int[2]);
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
 int output[2];if(pipe(output))return 10;
 int saved_out=fcntl(1,0,10),saved_err=fcntl(2,0,10);
 if(saved_out<0||saved_err<0||dup2(output[1],1)<0||dup2(output[1],2)<0)return 11;
 const char *arguments[]={"fixture","run","--allow-device","metal:0","--write-root",".","--format","json"};
 int status=lo_cli_main(8,arguments,0);
 if(dup2(saved_out,1)<0||dup2(saved_err,2)<0||close(output[1]))return 12;
 close(saved_out);close(saved_err);
 char bytes[16384];size_t length=0;
 for(;;){long count=read(output[0],bytes+length,sizeof(bytes)-length-1);if(count<0)return 13;if(!count)break;length+=(size_t)count;if(length==sizeof(bytes)-1)return 14;}
 bytes[length]=0;close(output[0]);
 if(opens!=1||status!=1||!contains(bytes,"Device.Unavailable")||!contains(bytes,"no system Metal device")||contains(bytes,"\"result\""))return 20;
 int later=open("later",0);if(later>=0){close(later);return 21;}
 if(lo_runtime_live_contexts()||lo_runtime_live_values()||lo_runtime_live_storages())return 22;
 return 0;
}
