/* Trusted ABI fixture: retain a real OnError argument, then publish it by CLI. */
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef __UINTPTR_TYPE__ Thread;
typedef __SIZE_TYPE__ Size;
typedef struct Context Context;
typedef struct Value Value;
typedef struct { U32 status,code; const Value *value; } Result;
extern int lo_cli_main(int,const char *const *,const char *const *);
extern void lo_http_original_entry(Context *,const Value *const *,U64,U64,Result *);
extern void lo_http_original_onerror(Context *,const Value *const *,U64,U64,Result *);
extern void lo_value_retain(const Value *),lo_value_release(const Value *);
extern U64 lo_runtime_live_programs(void),lo_runtime_live_contexts(void),lo_runtime_live_values(void);
extern U64 lo_runtime_live_storages(void),lo_runtime_live_storage_bytes(void);
extern int pthread_create(Thread *,const void *,void *(*)(void *),void *),pthread_join(Thread,void **);
extern int socket(int,int,int),connect(int,const void *,unsigned int),shutdown(int,int),close(int),poll(void *,unsigned long,int);
extern long read(int,void *,Size),write(int,const void *,Size);
extern int open(const char *,int,...),dup2(int,int),kill(int,int),getpid(void);
#if defined(__APPLE__)
struct Address{unsigned char length,family;unsigned short port;unsigned int address;unsigned char zero[8];};
#else
struct Address{unsigned short family,port;unsigned int address;unsigned char zero[8];};
#endif
static const Value *captured;
static U64 callback_count;
static int callback_problem,dump_mode,service_status=99;
void lo_http_error_capture(Context *context,const Value *const *arguments,U64 count,U64 site,Result *out){
 if(count!=1||!arguments||!arguments[0])__atomic_store_n(&callback_problem,1,__ATOMIC_RELAXED);
 else if(__atomic_fetch_add(&callback_count,1,__ATOMIC_RELAXED)==0){lo_value_retain(arguments[0]);__atomic_store_n(&captured,arguments[0],__ATOMIC_RELEASE);}
 else __atomic_store_n(&callback_problem,1,__ATOMIC_RELAXED);
 lo_http_original_onerror(context,arguments,count,site,out);
}
void lo_entry(Context *context,const Value *const *arguments,U64 count,U64 site,Result *out){
 if(dump_mode){out->status=0;out->code=0;out->value=__atomic_exchange_n(&captured,(const Value *)0,__ATOMIC_ACQ_REL);return;}
 lo_http_original_entry(context,arguments,count,site,out);
}
static int zero(void){return !lo_runtime_live_programs()&&!lo_runtime_live_contexts()&&!lo_runtime_live_values()&&!lo_runtime_live_storages()&&!lo_runtime_live_storage_bytes();}
static void *service(void *unused){(void)unused;const char *args[]={"http-error-input","serve","--listen","34447","--format","json"};service_status=lo_cli_main(6,args,0);return 0;}
static int send_all(int descriptor,const char *data,Size size){while(size){long count=write(descriptor,data,size);if(count<=0)return 1;data+=count;size-=(Size)count;}return 0;}
static Size length(const char *text){Size count=0;while(text[count])count++;return count;}
static int equal(const char *left,const char *right,Size size){for(Size i=0;i<size;i++)if(left[i]!=right[i])return 0;return 1;}
int main(void){
 if(!zero())return 10;
 int output=73;if(dup2(1,output)<0)return 10;int null=open("/dev/null",1);if(output<0||null<0||dup2(null,1)<0)return 11;close(null);
 Thread worker;if(pthread_create(&worker,0,service,0))return 12;
 struct Address address={0};
#if defined(__APPLE__)
 address.length=16;
#endif
 address.family=2;address.port=0x8f86;address.address=0x0100007f;
 int descriptor=-1;for(unsigned tries=0;tries<500;tries++){descriptor=socket(2,1,0);if(descriptor<0)return 13;if(connect(descriptor,&address,16)==0)break;close(descriptor);descriptor=-1;poll(0,0,10);}if(descriptor<0)return 14;
 const char *request;
 if(LO_CASE==0)request="PATCH /%FF?ignored=secret HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n";
 else if(LO_CASE==1)request="POST /declared%2Fbody?ignored=secret HTTP/1.1\r\nHost: localhost\r\nContent-Length: 1048577\r\nConnection: close\r\n\r\n";
 else if(LO_CASE==2)request="POST /chunks%2Foverflow?ignored=secret HTTP/1.1\r\nHost: localhost\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n100001\r\n";
 else if(LO_CASE==3)request="POST /length%2Fshort?ignored=secret HTTP/1.1\r\nHost: localhost\r\nContent-Length: 4\r\nConnection: close\r\n\r\nab";
 else if(LO_CASE==4)request="POST /chunk%2Fshort?ignored=secret HTTP/1.1\r\nHost: localhost\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n4\r\nab";
 else request="GET /response HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n";
 if(send_all(descriptor,request,length(request)))return 15;
 if((LO_CASE==3||LO_CASE==4)&&shutdown(descriptor,1))return 15;
 char response[8192];Size size=0;for(;;){if(size==sizeof(response))return 16;long count=read(descriptor,response+size,sizeof(response)-size);if(count<0)return 17;if(!count)break;size+=(Size)count;}
 close(descriptor);if(size<20||!equal(response,"HTTP/1.1 500",12)||!equal(response+size-7,"handled",7))return 18;
 if(kill(getpid(),2)||pthread_join(worker,0)||service_status!=0)return 19;
 if(__atomic_load_n(&callback_problem,__ATOMIC_RELAXED)||__atomic_load_n(&callback_count,__ATOMIC_RELAXED)!=1||!__atomic_load_n(&captured,__ATOMIC_ACQUIRE))return 20;
 if(lo_runtime_live_programs()||lo_runtime_live_contexts()||!lo_runtime_live_values()||lo_runtime_live_storages()||lo_runtime_live_storage_bytes())return 21;
 if(dup2(output,1)<0)return 22;close(output);dump_mode=1;
 const char *args[]={"http-error-input","run","--format","json"};int status=lo_cli_main(4,args,0);
 if(status||__atomic_load_n(&captured,__ATOMIC_ACQUIRE)||!zero())return 23;
 return 0;
}
