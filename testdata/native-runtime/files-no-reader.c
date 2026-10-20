typedef __SIZE_TYPE__ Size;
typedef __INT64_TYPE__ Offset;
extern int open(const char*,int,...);
extern int close(int);
extern int dup2(int,int);
extern int fcntl(int,int,...);
extern long read(int,void*,Size);
extern Offset lseek(int,Offset,int);
extern int mkfifo(const char*,unsigned);
extern unsigned alarm(unsigned);
extern int getpid(void);
extern int kill(int,int);
extern void *dlsym(void*,const char*);
extern int lo_cli_main(int,const char**);
#ifdef __APPLE__
#define CREATE (2|0x200|0x400)
extern int *__error(void);
#define ERROR_ADDRESS __error
#else
#define CREATE (2|0x40|0x200)
extern int *__errno_location(void);
#define ERROR_ADDRESS __errno_location
#endif
static int witnessed;
int openat(int directory,const char *path,int flags,...){
 typedef int (*OpenAt)(int,const char*,int,...);
 static OpenAt native;
 if(!native)native=(OpenAt)dlsym((void*)(__INTPTR_TYPE__)-1,"openat");
 if(!native){*ERROR_ADDRESS()=38;return -1;}
 int result=native(directory,path,flags,0666);
 if(!witnessed&&result<0&&*ERROR_ADDRESS()==6&&(flags&3)==1&&path[0]=='p'&&path[1]=='i'&&path[2]=='p'&&path[3]=='e'&&!path[4]){
  witnessed=1;
  kill(getpid(),2);
  *ERROR_ADDRESS()=6;
 }
 return result;
}
static int contains(const char *text,const char *needle){for(;*text;++text){Size i=0;while(needle[i]&&text[i]==needle[i])++i;if(!needle[i])return 1;}return 0;}
int main(void){
 int output=open("no-reader-diagnostic",CREATE,0666),saved_out=fcntl(1,0,3),saved_err=fcntl(2,0,3);
 if(output<0||saved_out<0||saved_err<0||dup2(output,1)<0||dup2(output,2)<0||mkfifo("pipe",0666))return 10;
 const char *arguments[]={"fixture","run","--format","json","--write-root","."};
 alarm(10);
 int status=lo_cli_main(6,arguments);
 alarm(0);
 if(dup2(saved_out,1)<0||dup2(saved_err,2)<0||lseek(output,0,0)<0)return 11;
 char bytes[16384];long count=read(output,bytes,sizeof(bytes)-1);if(count<0)return 12;bytes[count]=0;
 if(!witnessed||status!=2||!contains(bytes,"runtime.cancelled")||!contains(bytes,"write_progress")||contains(bytes,"\"result\""))return 20;
 int later=open("later",0);if(later>=0){close(later);return 21;}
 close(output);close(saved_out);close(saved_err);return 0;
}
