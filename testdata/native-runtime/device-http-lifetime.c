#include "lo_device_driver.h"
typedef __UINT64_TYPE__ U64;
typedef __UINTPTR_TYPE__ Thread;
typedef __SIZE_TYPE__ Size;
extern int lo_cli_main(int,const char *const *,const char *const *);
extern U64 lo_runtime_live_contexts(void);
extern U64 lo_runtime_live_values(void);
extern U64 lo_runtime_live_storages(void);
extern U64 lo_runtime_live_storage_bytes(void);
static U64 opens, closes, allocations, releases, transfers, receipt_errors;
static U64 next_id, session_live;
static void observe(uintptr_t user,const LoDeviceReceipt *r){(void)user;
 switch(r->event){
 case LO_DEVICE_OPEN: opens++; if(next_id!=1)receipt_errors++; break;
 case LO_DEVICE_ALLOCATE: allocations++; if(r->allocation!=++next_id||r->bytes!=4)receipt_errors++; session_live+=r->bytes; break;
 case LO_DEVICE_RELEASE: releases++; session_live-=r->bytes; break;
 case LO_DEVICE_TRANSFER_EVENT: transfers++; break;
 case LO_DEVICE_CLOSE: closes++; if(session_live||r->live_bytes)receipt_errors++; next_id=0; break;
 default: receipt_errors++;
 }
 if(r->live_bytes!=session_live)receipt_errors++;
}
extern int pthread_create(Thread *,const void *,void *(*)(void *),void *);
extern int pthread_join(Thread,void **);
extern int socket(int,int,int);
extern int connect(int,const void *,unsigned int);
extern long read(int,void *,Size);
extern long write(int,const void *,Size);
extern int close(int);
extern int shutdown(int,int);
extern int poll(void *,unsigned long,int);
extern int pipe(int *);
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
static void *service(void *unused){(void)unused;const char *args[]={"lifetime","serve","--listen","34445","--format","json","--allow-device","cuda-sim:0"};service_status=lo_cli_main(8,args,0);return 0;}
static int body(int descriptor){char buffer[65536];Size n=0;for(;;){if(n==sizeof(buffer))return 1;long count=read(descriptor,buffer+n,1);if(count!=1)return 2;n++;if(n>=4&&buffer[n-4]=='\r'&&buffer[n-3]=='\n'&&buffer[n-2]=='\r'&&buffer[n-1]=='\n')break;}char payload[2];for(unsigned i=0;i<2;i++){if(read(descriptor,payload+i,1)!=1)return 3;}return payload[0]!='-'||payload[1]!='0';}
static void number(U64 value){char digits[32];unsigned end=sizeof(digits),start=end;do{digits[--start]='0'+value%10;value/=10;}while(value);write(2,digits+start,end-start);}
static void field(const char *name,U64 value){const char *end=name;while(*end)++end;write(2,name,end-name);number(value);}
static int lifetime_failure(U64 request,U64 contexts,U64 values){
 field("{\"fixture\":\"device-http-lifetime\",\"request\":",request);
 field(",\"expected_contexts\":",contexts);field(",\"actual_contexts\":",lo_runtime_live_contexts());
 field(",\"expected_values\":",values);field(",\"actual_values\":",lo_runtime_live_values());
 field(",\"storages\":",lo_runtime_live_storages());field(",\"storage_bytes\":",lo_runtime_live_storage_bytes());
 write(2,"}\n",2);return 6;
}
/* Compare equal lifecycle phases, with bounded waits for worker cleanup. */
static int settled(U64 contexts,U64 values,U64 *observed){
 U64 previous=~(U64)0;unsigned stable=0;
 for(unsigned attempt=0;attempt<500;++attempt){
  U64 live=lo_runtime_live_values();
  if(lo_runtime_live_contexts()==contexts && !lo_runtime_live_storages() && !lo_runtime_live_storage_bytes() &&
     (values==~(U64)0 || live==values)){
   stable=live==previous?stable+1:1;if(stable==2){*observed=live;return 1;}
  }else stable=0;
  previous=live;poll(0,0,10);
 }
 return 0;
}
struct ReadyPoll{int descriptor;short events,revents;};
static int ready(int descriptor){
 char buffer[4096];Size size=0;
 /* The public readiness line is emitted before any client connects. */
 for(unsigned attempt=0;attempt<500 && size<sizeof(buffer);++attempt){
  struct ReadyPoll event={descriptor,1,0};if(poll(&event,1,10)<0)return 0;
  if(!event.revents)continue;
  if(read(descriptor,buffer+size,1)!=1)return 0;
  if(buffer[size++]=='\n'){
   const char expected[]="\"status\":\"ready\"";
   for(Size start=0;start+sizeof(expected)-1<=size;++start){unsigned index=0;while(index<sizeof(expected)-1 && buffer[start+index]==expected[index])++index;if(index==sizeof(expected)-1)return 1;}
   return 0;
  }
 }
 return 0;
}
int main(void){int readiness[2];if(pipe(readiness) || dup2(readiness[1],1)<0)return 73;close(readiness[1]);
#if !defined(__APPLE__)
 int control=socket(2,2,0);if(control<0)return 70;unsigned char interface_request[40]={0};interface_request[0]='l';interface_request[1]='o';if(ioctl(control,0x8913UL,interface_request))return 71;interface_request[16]|=1;if(ioctl(control,0x8914UL,interface_request))return 72;close(control);
#endif
 lo_device_set_observer(0,observe);
 Thread worker;if(pthread_create(&worker,0,service,0))return 1;
 if(!ready(readiness[0]))return 74;
 U64 server_baseline=0,idle_baseline=0,observed=0;
 if(!settled(1,~(U64)0,&server_baseline))return lifetime_failure(0,1,0);
 struct Address address={0};
#if defined(__APPLE__)
 address.length=16;
#endif
 address.family=2;address.port=0x8d86;address.address=0x0100007f;
 int descriptor=-1;for(unsigned tries=0;tries<500;tries++){descriptor=socket(2,1,0);if(descriptor<0)return 2;if(connect(descriptor,&address,16)==0)break;close(descriptor);descriptor=-1;poll(0,0,10);}if(descriptor<0)return 3;
 if(!settled(2,~(U64)0,&idle_baseline))return lifetime_failure(0,2,0);
 static const char request[]="GET / HTTP/1.1\r\nHost: localhost\r\n\r\n";
 static const char last_request[]="GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n";
 for(unsigned iteration=0;iteration<1000;iteration++){
  const char *message=iteration==999?last_request:request;Size size=iteration==999?sizeof(last_request)-1:sizeof(request)-1;
  if(write(descriptor,message,size)!=(long)size||body(descriptor))return 4;
  if(iteration%100==99){
   U64 contexts=iteration==999?1:2,values=iteration==999?server_baseline:idle_baseline;
   if(!settled(contexts,values,&observed))return lifetime_failure(iteration+1,contexts,values);
  }
 }
 shutdown(descriptor,2);close(descriptor);if(kill(getpid(),2))return 7;if(pthread_join(worker,0)||service_status!=0)return 8;close(readiness[0]);if(lo_runtime_live_contexts()||lo_runtime_live_values()||lo_runtime_live_storages()||lo_runtime_live_storage_bytes())return lifetime_failure(1001,0,0);
 if(opens!=1000)return 10;
 if(closes!=1001)return 11;
 if(allocations!=3000)return 12;
 if(releases!=3000)return 13;
 if(transfers!=2000)return 14;
 if(receipt_errors)return 15;
 return 0;
}
