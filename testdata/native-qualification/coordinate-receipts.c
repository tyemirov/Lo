typedef __UINT64_TYPE__ U64;
typedef __UINTPTR_TYPE__ Thread;
typedef __SIZE_TYPE__ Size;
extern const char *lo_coordinate_arguments[];
extern const int lo_coordinate_argument_count;
extern int lo_cli_main(int,const char *const *,const char *const *);
extern U64 lo_runtime_live_contexts(void),lo_runtime_live_values(void),lo_runtime_live_storages(void),lo_runtime_live_storage_bytes(void);
extern int socket(int,int,int),bind(int,const void*,unsigned int),listen(int,int),accept(int,void*,void*),setsockopt(int,int,int,const void*,unsigned int),close(int),poll(void*,unsigned long,int);
extern long read(int,void*,Size),write(int,const void*,Size);
extern int pthread_create(Thread*,const void*,void*(*)(void*),void*),pthread_join(Thread,void**);
#if defined(__APPLE__)
struct Address{unsigned char length,family;unsigned short port;unsigned int address;unsigned char zero[8];};
#else
struct Address{unsigned short family,port;unsigned int address;unsigned char zero[8];};
extern int ioctl(int,unsigned long,...);
#endif
struct Poll{int descriptor;short events,revents;};
static int listening;
static void *server(void *unused){
 (void)unused;struct Poll event={listening,1,0};if(poll(&event,1,5000)<=0)return (void*)1;
 int client=accept(listening,0,0);if(client<0)return (void*)2;
 char request[8192];Size size=0;int complete=0;
 while(size<sizeof(request)&&!complete){long count=read(client,request+size,sizeof(request)-size);if(count<=0)return (void*)3;size+=count;for(Size index=3;index<size;++index)if(request[index-3]=='\r'&&request[index-2]=='\n'&&request[index-1]=='\r'&&request[index]=='\n'){complete=1;break;}}
 if(!complete)return (void*)4;
 const char response[]="HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n";
 if(write(client,response,sizeof(response)-1)!=(long)sizeof(response)-1)return (void*)5;
 close(client);return 0;
}
int main(int argc,const char *const *argv,const char *const *environment){
 if(argc>1)return lo_cli_main(argc,argv,environment);
#if !defined(__APPLE__)
 int control=socket(2,2,0);if(control<0)return 70;unsigned char interface_request[40]={0};interface_request[0]='l';interface_request[1]='o';if(ioctl(control,0x8913UL,interface_request))return 71;interface_request[16]|=1;if(ioctl(control,0x8914UL,interface_request))return 72;close(control);
#endif
 Thread thread;
 if(LO_CASE==3){
  listening=socket(2,1,0);if(listening<0)return 20;struct Address address={0};
#if defined(__APPLE__)
  address.length=16;int level=65535,option=4;
#else
  int level=1,option=2;
#endif
  address.family=2;address.port=(unsigned short)((34510<<8)|(34510>>8));address.address=0x0100007f;int reuse=1;
  if(setsockopt(listening,level,option,&reuse,sizeof(reuse))||bind(listening,&address,16)||listen(listening,1)||pthread_create(&thread,0,server,0))return 21;
 }
 const char *arguments[64];int count=lo_coordinate_argument_count;
 if(count+2>64)return 22;
 for(int index=0;index<count;++index)arguments[index]=lo_coordinate_arguments[index];
 if(LO_CASE==2)arguments[1]="serve";
 if(LO_CASE==4){arguments[count++]="--process-launcher";arguments[count++]=argv[0];}
 int status=lo_cli_main(count,arguments,environment);
 if(LO_CASE==3){void *result=(void*)1;if(pthread_join(thread,&result)||result)return 23;close(listening);}
 if(lo_runtime_live_contexts()||lo_runtime_live_values()||lo_runtime_live_storages()||lo_runtime_live_storage_bytes())return 24;
 return status;
}
