typedef __SIZE_TYPE__ Size;
typedef __INT64_TYPE__ Offset;
typedef __UINTPTR_TYPE__ Thread;
extern int open(const char *,int,...);
extern int fcntl(int,int,...);
extern int dup2(int,int);
extern Offset lseek(int,Offset,int);
extern long read(int,void *,Size);
extern long write(int,const void *,Size);
extern int socket(int,int,int);
extern int bind(int,const void *,unsigned int);
extern int listen(int,int);
extern int accept(int,void *,void *);
extern int setsockopt(int,int,int,const void *,unsigned int);
extern int pthread_create(Thread *,const void *,void *(*)(void *),void *);
extern int pthread_join(Thread,void **);
extern int lo_cli_main(int,const char *const *,const char *const *);
#if defined(__APPLE__)
extern int *__error(void);
#define ERROR_ADDRESS __error
#define CREATE_FLAGS (2|0x200|0x400)
struct Address{unsigned char length,family;unsigned short port;unsigned int address;unsigned char zero[8];};
#else
extern int *__errno_location(void);
#define ERROR_ADDRESS __errno_location
#define CREATE_FLAGS (2|0x40|0x200)
struct Address{unsigned short family,port;unsigned int address;unsigned char zero[8];};
extern int ioctl(int,unsigned long,...);
#endif
static int descriptor_invariant=0;
#if LO_CASE == 0
int connect(int descriptor,const void *address,unsigned int length){(void)address;(void)length;if((fcntl(descriptor,1)&1)==0)descriptor_invariant=1;
#if defined(__APPLE__)
 *ERROR_ADDRESS()=61;
#else
 *ERROR_ADDRESS()=111;
#endif
 return -1;}
#endif
int close(int descriptor){(void)descriptor;*ERROR_ADDRESS()=5;return -1;}
static void *server(void *pointer){int descriptor=*(int *)pointer;int client=accept(descriptor,0,0);if(client<0)return (void *)1;char request[4096];if(read(client,request,sizeof(request))<=0)return (void *)2;static const char response[] =
#if LO_CASE == 2
"HTTP/1.0 200 OK\r\nContent-Length: 0\r\n\r\n";
#else
"HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n";
#endif
if(write(client,response,sizeof(response)-1)!=(long)(sizeof(response)-1))return (void *)3;return 0;}
static int contains(const char *text,const char *needle){for(;*text;text++){Size n=0;while(needle[n]&&text[n]==needle[n])n++;if(!needle[n])return 1;}return 0;}
int main(void){
#if !defined(__APPLE__)
 int control=socket(2,2,0);if(control<0)return 70;unsigned char request[40]={0};request[0]='l';request[1]='o';if(ioctl(control,0x8913UL,request))return 71;request[16]|=1;if(ioctl(control,0x8914UL,request))return 72;
#endif
 int output=open("http-cleanup-diagnostic.json",CREATE_FLAGS,0666);int saved=fcntl(2,0,3);if(output<0||saved<0||dup2(output,2)<0)return 10;Thread thread;int listening=-1;
#if LO_CASE != 0
 listening=socket(2,1,0);if(listening<0)return 11;struct Address address={0};
#if defined(__APPLE__)
 address.length=16;
#endif
 address.family=2;unsigned port=34446+LO_CASE;address.port=(unsigned short)((port<<8)|(port>>8));address.address=0x0100007f;int reuse=1;
#if defined(__APPLE__)
 if(setsockopt(listening,65535,4,&reuse,sizeof(reuse)))return 16;
#else
 if(setsockopt(listening,1,2,&reuse,sizeof(reuse)))return 16;
#endif
if(bind(listening,&address,16)||listen(listening,1)||pthread_create(&thread,0,server,&listening))return 12;
#endif
 const char *grant=
#if LO_CASE == 0
 "GET,http://127.0.0.1:34446,127.0.0.1";
#elif LO_CASE == 1
 "GET,http://127.0.0.1:34447,127.0.0.1";
#else
 "GET,http://127.0.0.1:34448,127.0.0.1";
#endif
 const char *arguments[]={"fixture","run","--format","json","--http-allow",grant};int status=lo_cli_main(6,arguments,0);
#if LO_CASE != 0
 void *result=0;if(pthread_join(thread,&result)||result)return 13;
#endif
 if(dup2(saved,2)<0||lseek(output,0,0)<0)return 14;char diagnostic[8192];long count=read(output,diagnostic,sizeof(diagnostic)-1);if(count<=0)return 15;diagnostic[count]=0;if(status!=1||!contains(diagnostic,"HTTP.ExchangeFailed")||!contains(diagnostic,"cleanup_errors")||!contains(diagnostic,"HTTP resource release failed"))return 20;
#if LO_CASE == 0
 if(!contains(diagnostic,"connect")||!contains(diagnostic,"not_started"))return 21;if(descriptor_invariant)return 24;
#elif LO_CASE == 1
 if(!contains(diagnostic,"finalize")||!contains(diagnostic,"unknown"))return 22;
#else
 if(!contains(diagnostic,"read-response")||!contains(diagnostic,"unknown"))return 23;
#endif
 return 0;}
