typedef __UINT64_TYPE__ U64;
typedef __UINTPTR_TYPE__ Thread;
typedef __SIZE_TYPE__ Size;
extern int lo_cli_main(int,const char *const *,const char *const *);
extern U64 lo_runtime_live_contexts(void),lo_runtime_live_values(void);
extern int pthread_create(Thread *,const void *,void *(*)(void *),void *);
extern int pthread_join(Thread,void **);
extern void *dlsym(void *,const char *);
extern int open(const char *,int,...),close(int),dup2(int,int),fcntl(int,int,...);
extern long read(int,void *,Size),write(int,const void *,Size);
extern int linkat(int,const char *,int,const char *,int),setenv(const char *,const char *,int);
extern char *getenv(const char *);
extern int poll(void *,unsigned long,int);
struct Poll {int descriptor;short events,returned;};
static int paused,released,armed,pipe_count;
static int (*real_pipe)(int *);
static int console_status=99,probe_status=99;
static int equal(const char *a,const char *b) {
 while(*a && *a==*b){++a;++b;}
 return *a==*b;
}
static int decimal(const char *text) {
 if(!text || !*text)return -1;
 int value=0;
 while(*text){if(*text<'0'||*text>'9')return -1;value=value*10+*text++-'0';}
 return value;
}
static int publish(const char *name,int descriptor) {
 char reversed[16],text[16];unsigned count=0,index=0;
 do{reversed[count++]=(char)('0'+descriptor%10);descriptor/=10;}while(descriptor);
 while(count)text[index++]=reversed[--count];
 text[index]=0;return setenv(name,text,1);
}
int pipe(int descriptors[2]) {
 int result=real_pipe(descriptors);
 if(result==0 && __atomic_load_n(&armed,__ATOMIC_SEQ_CST)) {
  int count=__atomic_add_fetch(&pipe_count,1,__ATOMIC_SEQ_CST);
  if(count==3) {
   /* The second invocation starts only after this publication finishes. */
   if(publish("LO_TEST_CONSOLE_LIVENESS_FD",descriptors[1]))return -1;
   __atomic_store_n(&paused,1,__ATOMIC_SEQ_CST);
   while(!__atomic_load_n(&released,__ATOMIC_SEQ_CST))poll(0,0,1);
  }
 }
 return result;
}
static void *console(void *unused) {
 (void)unused;
 const char *args[]={"inheritance","run","--open-files-root","."};
 console_status=lo_cli_main(4,args,0);return 0;
}
static void *probe(void *unused) {
 (void)unused;
 const char *args[]={"inheritance","run","--open-files-root",".","--","probe"};
 probe_status=lo_cli_main(6,args,0);return 0;
}
int main(int argc,const char **argv) {
 if(argc==4 && equal(argv[1],"-Fn")) {
  int liveness=decimal(getenv("LO_TEST_CONSOLE_LIVENESS_FD"));
  int report=decimal(getenv("LO_TEST_CONSOLE_REPORT_FD"));
  if(liveness<0 || report<0)return 31;
  unsigned char inherited=fcntl(liveness,1)>=0;
  return write(report,&inherited,1)==1?0:32;
 }
 real_pipe=(int (*)(int *))dlsym((void *)-1,"pipe");
 if(!real_pipe)return 33;
 int report[2];
 if(real_pipe(report) || fcntl(report[0],2,1)<0 || publish("LO_TEST_CONSOLE_REPORT_FD",report[1]))return 34;
 int null=open("/dev/null",1);
 if(null<0 || dup2(null,1)<0)return 35;
 close(null);
#ifdef __APPLE__
 const char *executable=argv[0];
 const int current_directory=-2;
#else
 const char *executable="/probe";
 const int current_directory=-100;
#endif
 if(linkat(current_directory,executable,current_directory,"lsof",0) || setenv("PATH",".",1))return 36;
 __atomic_store_n(&armed,1,__ATOMIC_SEQ_CST);
 Thread first,second;
 if(pthread_create(&first,0,console,0))return 37;
 unsigned tries;
 for(tries=0;tries<5000 && !__atomic_load_n(&paused,__ATOMIC_SEQ_CST);++tries)poll(0,0,1);
 if(!__atomic_load_n(&paused,__ATOMIC_SEQ_CST))return 38;
 if(pthread_create(&second,0,probe,0))return 39;
 struct Poll pending={report[0],1,0};
 int available=poll(&pending,1,1000);
 __atomic_store_n(&released,1,__ATOMIC_SEQ_CST);
 if(pthread_join(first,0) || pthread_join(second,0))return 40;
 if(available==0){pending.returned=0;available=poll(&pending,1,1000);}
 unsigned char inherited=99;
 if(available<=0 || read(report[0],&inherited,1)!=1)return 41;
 close(report[0]);close(report[1]);
 if(inherited)return 43;
 if(console_status || probe_status)return 44;
 if(lo_runtime_live_contexts() || lo_runtime_live_values())return 45;
 return 0;
}
