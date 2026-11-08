#include "lo_host_observer.h"
extern void lo_observation_handler(Context *, const Value **, uint64_t, uint64_t, ResultSlot *);
static int rejected_providers;
static int supplied_clock(uintptr_t user, LoClockMetadata *metadata) { (void)user; (void)metadata; return 0; }
static int supplied_volume(uintptr_t user, const unsigned char *root, uint64_t length, LoVolumeMetadata *metadata) { (void)user; (void)root; (void)length; (void)metadata; return 0; }
void lo_flow_0(Context *context, const Value **arguments, uint64_t count, uint64_t site, ResultSlot *result) {
    if (!lo_context_clock_observer(context, supplied_clock, 0)
        && !lo_context_volume_observer(context, supplied_volume, 0)) ++rejected_providers;
    lo_observation_handler(context, arguments, count, site, result);
}
typedef __UINT64_TYPE__ U64;
typedef __UINTPTR_TYPE__ Thread;
typedef __SIZE_TYPE__ Size;
extern int lo_cli_main(int,const char *const *,const char *const *);
extern U64 lo_runtime_live_contexts(void);
extern U64 lo_runtime_live_values(void);
extern int pthread_create(Thread *,const void *,void *(*)(void *),void *);
extern int pthread_join(Thread,void **);
extern int socket(int,int,int);
extern int connect(int,const void *,unsigned int);
extern long read(int,void *,Size);
extern long write(int,const void *,Size);
extern int close(int);
extern int shutdown(int,int);
extern int poll(void *,unsigned long,int);
extern int open(const char *,int,...);
extern int dup2(int,int);
extern int kill(int,int);
extern int getpid(void);

#if defined(__APPLE__)
struct Address{unsigned char length,family;unsigned short port;unsigned int address;unsigned char zero[8];};
#else
struct Address{unsigned short family,port;unsigned int address;unsigned char zero[8];};
extern int ioctl(int,unsigned long,...);
#endif
static int service_status=99;
static void *service(void *unused){(void)unused;const char *args[]={"lifetime","serve","--listen","34481","--format","json"};service_status=lo_cli_main(6,args,0);return 0;}
static int body(int descriptor){char buffer[65536];Size n=0;for(;;){if(n==sizeof(buffer))return 1;long count=read(descriptor,buffer+n,1);if(count!=1)return 2;n++;if(n>=4&&buffer[n-4]=='\r'&&buffer[n-3]=='\n'&&buffer[n-2]=='\r'&&buffer[n-1]=='\n')break;}char payload[2];for(unsigned i=0;i<2;i++){if(read(descriptor,payload+i,1)!=1)return 3;}return payload[0]!='o'||payload[1]!='k';}
int main(void){int null=open("/dev/null",1);if(null<0||dup2(null,1)<0)return 73;close(null);
#if !defined(__APPLE__)
 int control=socket(2,2,0);if(control<0)return 70;unsigned char interface_request[40]={0};interface_request[0]='l';interface_request[1]='o';if(ioctl(control,0x8913UL,interface_request))return 71;interface_request[16]|=1;if(ioctl(control,0x8914UL,interface_request))return 72;close(control);
#endif
 Thread worker;if(pthread_create(&worker,0,service,0))return 1;
 struct Address address={0};
#if defined(__APPLE__)
 address.length=16;
#endif
 address.family=2;address.port=0xb186;address.address=0x0100007f;
 int descriptor=-1;for(unsigned tries=0;tries<500;tries++){descriptor=socket(2,1,0);if(descriptor<0)return 2;if(connect(descriptor,&address,16)==0)break;close(descriptor);descriptor=-1;poll(0,0,10);}if(descriptor<0)return 3;
 static const char request[]="GET / HTTP/1.1\r\nHost: localhost\r\n\r\n";U64 baseline=0;
 for(unsigned iteration=0;iteration<1;iteration++){if(write(descriptor,request,sizeof(request)-1)!=(long)(sizeof(request)-1)||body(descriptor))return 4;if(iteration%100==99){poll(0,0,20);if(lo_runtime_live_contexts()>2)return 5;U64 live=lo_runtime_live_values();if(iteration==99)baseline=live;else if(live!=baseline)return 6;}}
 shutdown(descriptor,2);close(descriptor);if(kill(getpid(),2))return 7;if(pthread_join(worker,0)||service_status!=0)return 8;if(lo_runtime_live_contexts()||lo_runtime_live_values())return 9;return rejected_providers==1?0:31;
}
