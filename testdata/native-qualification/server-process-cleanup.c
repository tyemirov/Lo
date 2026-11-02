typedef __SIZE_TYPE__ Size;
typedef __UINTPTR_TYPE__ Thread;
typedef __UINT64_TYPE__ U64;
extern int lo_cli_main(int,const char *const *,const char *const *);
extern U64 lo_runtime_live_contexts(void),lo_runtime_live_values(void),lo_runtime_live_storages(void),lo_runtime_live_storage_bytes(void);
extern int open(const char*,int,...),fcntl(int,int,...),dup2(int,int),socket(int,int,int),connect(int,const void*,unsigned int),poll(void*,unsigned long,int);
extern int kill(int,int),getpid(void);
extern long read(int,void*,Size),write(int,const void*,Size),lseek(int,long,int);
extern Thread pthread_self(void);
extern int pthread_create(Thread*,const void*,void*(*)(void*),void*),pthread_join(Thread,void**);
extern void *dlsym(void*,const char*);
extern int mkdir(const char *,unsigned),chdir(const char *);
extern char *getcwd(char *,Size);
#ifndef __APPLE__
extern long readlink(const char *,char *,Size);
#endif
#ifdef __APPLE__
extern int *__error(void);
#define ERROR_ADDRESS __error
#define CREATE (2|0x200|0x400)
struct Address{unsigned char length,family;unsigned short port;unsigned int address;unsigned char zero[8];};
#else
extern int *__errno_location(void);
#define ERROR_ADDRESS __errno_location
#define CREATE (2|0x40|0x200)
struct Address{unsigned short family,port;unsigned int address;unsigned char zero[8];};
extern int ioctl(int,unsigned long,...);
#endif
static Thread owner;
static int armed,failed_closes;
static char cleanup_root[4096];
static int same_path(const char *left,const char *right){while(*left&&*left==*right){++left;++right;}return *left==*right;}
static int selected_directory(int descriptor,const char *expected){
 if(!*expected)return 0;
 char path[4096];
#ifdef __APPLE__
 if(fcntl(descriptor,50,path))return 0;
#else
 char link[64]="/proc/self/fd/",digits[16];unsigned value=(unsigned)descriptor;Size length=14,count=0;
 do{digits[count++]=(char)('0'+value%10);value/=10;}while(value);
 while(count)link[length++]=digits[--count];link[length]=0;
 long size=readlink(link,path,sizeof(path)-1);if(size<0)return 0;path[size]=0;
#endif
 return same_path(path,expected);
}

struct Poll{int descriptor;short events,revents;};
static int (*real_close)(int),(*real_shutdown)(int,int);
static void *client_failed(unsigned code){kill(getpid(),15);return (void*)(__UINTPTR_TYPE__)code;}
int shutdown(int descriptor,int how){
 int (*shutdown_function)(int,int)=real_shutdown;
 if(!shutdown_function)shutdown_function=(int (*)(int,int))dlsym((void *)-1,"shutdown");
 if(!shutdown_function){*ERROR_ADDRESS()=5;return -1;}
 int result=shutdown_function(descriptor,how);
 if(how==2&&pthread_self()!=owner)__atomic_store_n(&armed,1,__ATOMIC_RELEASE);
 return result;
}
int close(int descriptor){
 int (*close_function)(int)=real_close;
 if(!close_function)close_function=(int (*)(int))dlsym((void *)-1,"close");
 if(!close_function){*ERROR_ADDRESS()=5;return -1;}
 int inject=pthread_self()==owner&&__atomic_load_n(&armed,__ATOMIC_ACQUIRE)&&!failed_closes&&selected_directory(descriptor,cleanup_root);
 int result=close_function(descriptor);
 if(inject&&result==0){
  __atomic_store_n(&armed,0,__ATOMIC_RELEASE);++failed_closes;*ERROR_ADDRESS()=5;return -1;
 }
 return result;
}
static void *request(void *unused){
 (void)unused;struct Address address={0};
#ifdef __APPLE__
 address.length=16;
#endif
 address.family=2;address.port=(unsigned short)((34521<<8)|(34521>>8));address.address=0x0100007f;
 int descriptor=-1;
 for(unsigned attempt=0;attempt<5000;++attempt){
  descriptor=socket(2,1,0);if(descriptor<0)return client_failed(1);
  if(!connect(descriptor,&address,16))break;
  real_close(descriptor);descriptor=-1;poll(0,0,1);
 }
 if(descriptor<0)return client_failed(2);
 const char message[]="GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n";
 if(write(descriptor,message,sizeof(message)-1)!=(long)sizeof(message)-1)return client_failed(3);
 struct Poll event={descriptor,1,0};if(poll(&event,1,5000)<=0)return client_failed(5);
 char buffer[1024];long count=read(descriptor,buffer,sizeof(buffer));real_close(descriptor);
 if(count>0){write(2,buffer,(Size)count);return client_failed(4);}return 0;
}
int main(void){
 real_close=(int(*)(int))dlsym((void*)-1,"close");real_shutdown=(int(*)(int,int))dlsym((void*)-1,"shutdown");
 if(!real_close||!real_shutdown)return 10;owner=pthread_self();
 if(mkdir("cleanup-root",0777)||chdir("cleanup-root")||!getcwd(cleanup_root,sizeof(cleanup_root)))return 18;
#ifndef __APPLE__
 int control=socket(2,2,0);if(control<0)return 70;unsigned char interface_request[40]={0};interface_request[0]='l';interface_request[1]='o';if(ioctl(control,0x8913UL,interface_request))return 71;interface_request[16]|=1;if(ioctl(control,0x8914UL,interface_request))return 72;real_close(control);
#endif
 int output=open("server-diagnostic",CREATE,0666),events=open("server-events",CREATE,0666),saved_out=fcntl(1,0,3),saved_err=fcntl(2,0,3);
 if(output<0||events<0||saved_out<0||saved_err<0||dup2(events,1)<0||dup2(output,2)<0)return 11;
 Thread client;if(pthread_create(&client,0,request,0))return 12;
 const char *args[]={"fixture","serve","--format","json","--listen","34521","--write-root","."};
 int status=lo_cli_main(8,args,0);__atomic_store_n(&armed,0,__ATOMIC_RELEASE);
 void *client_result=(void*)1;if(pthread_join(client,&client_result))return 13;
 if(dup2(saved_out,1)<0||dup2(saved_err,2)<0||lseek(output,0,0)<0)return 14;
 char diagnostic[32768];long count=read(output,diagnostic,sizeof(diagnostic)-1);
 if(client_result){if(count>0)write(2,diagnostic,(Size)count);return 30+(int)(__UINTPTR_TYPE__)client_result;}
 if(count<=0||count==(long)sizeof(diagnostic)-1||status!=2||failed_closes!=1)return 15;
 if(lo_runtime_live_contexts()||lo_runtime_live_values()||lo_runtime_live_storages()||lo_runtime_live_storage_bytes())return 16;
 if(write(1,diagnostic,(Size)count)!=count)return 17;
 return 0;
}
