typedef __SIZE_TYPE__ Size;
typedef __INT64_TYPE__ Offset;
extern int lo_cli_main(int,const char**,const char**);
extern unsigned long long lo_runtime_live_contexts(void);
extern unsigned long long lo_runtime_live_values(void);
extern int open(const char*,int,...);
extern int close(int);
extern int dup2(int,int);
extern int fcntl(int,int,...);
extern Offset lseek(int,Offset,int);
extern long read(int,void*,Size);
extern long write(int,const void*,Size);
extern int linkat(int,const char*,int,const char*,int);
extern int setenv(const char*,const char*,int);
extern int waitpid(int,int*,int);
#ifdef __APPLE__
#define CREATE (2|0x200|0x400)
#define CURRENT_DIRECTORY -2
extern int *__error(void);
#define ERROR_ADDRESS __error
#else
#define CREATE (2|0x40|0x200)
#define CURRENT_DIRECTORY -100
extern int *__errno_location(void);
#define ERROR_ADDRESS __errno_location
#endif
#define STREAM_LIMIT (8u<<20)
static int contains(const char *text,const char *needle){for(;*text;++text){Size i=0;while(needle[i]&&text[i]==needle[i])++i;if(!needle[i])return 1;}return 0;}
static int equal(const char *a,const char *b){for(;*a&&*a==*b;++a,++b){}return *a==*b;}
static int emit(int descriptor,Size count){
 char bytes[65536];for(Size index=0;index<sizeof(bytes);++index)bytes[index]='\n';
 while(count){Size selected=count<sizeof(bytes)?count:sizeof(bytes);long done=write(descriptor,bytes,selected);if(done<=0)return 30;count-=(Size)done;}
 return 0;
}
int main(int argc,const char **argv){
 if(argc==4&&equal(argv[1],"-Fn")){
  if(LO_CASE==0||LO_CASE==1||LO_CASE==4){int status=emit(1,STREAM_LIMIT+(LO_CASE==1));if(status)return status;}
  if(LO_CASE==2||LO_CASE==3||LO_CASE==4){int status=emit(2,STREAM_LIMIT+(LO_CASE==3));if(status)return status;}
  return 0;
 }
#ifdef __APPLE__
 const char *executable=argv[0];
#else
 const char *executable="/probe";
#endif
 if(linkat(CURRENT_DIRECTORY,executable,CURRENT_DIRECTORY,"lsof",0)||setenv("PATH",".",1))return 10;
 int output=open("probe-output",CREATE,0666),saved_out=fcntl(1,0,3),saved_err=fcntl(2,0,3);
 if(output<0||saved_out<0||saved_err<0||dup2(output,1)<0||dup2(output,2)<0)return 11;
 const char *arguments[]={"fixture","run","--format","json","--open-files-root",".","--write-root",".","--max-steps","100000000","--max-value-bytes","0","--max-live-bytes","0","--max-data-bytes","0"};
 int status=lo_cli_main(16,arguments,0);
 if(dup2(saved_out,1)<0||dup2(saved_err,2)<0||lseek(output,0,0)<0)return 12;
 char bytes[16384];long count=read(output,bytes,sizeof(bytes)-1);if(count<0)return 13;bytes[count]=0;
 int overflow=LO_CASE==1||LO_CASE==3;
 if(overflow){
  if(status!=2||!contains(bytes,"runtime.limit")||!contains(bytes,"open-file probe output")||contains(bytes,"File.OpenFilesFailed")){
   const char message[]="probe transport overflow was not terminal\n";write(2,message,sizeof(message)-1);write(2,bytes,(Size)count);return 20;
  }
 }else if(status||!contains(bytes,"\"value\":\"0\""))return 21;
 int later=open("later",0);
 if(overflow?(later>=0):(later<0))return 22;
 if(later>=0)close(later);
 if(lo_runtime_live_contexts()||lo_runtime_live_values())return 23;
 int child_status;if(waitpid(-1,&child_status,1)!=-1||*ERROR_ADDRESS()!=10)return 24;
 close(output);close(saved_out);close(saved_err);return 0;
}
