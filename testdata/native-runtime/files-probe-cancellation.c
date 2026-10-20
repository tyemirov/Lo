typedef __SIZE_TYPE__ Size;
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef __INT64_TYPE__ Offset;
typedef struct Context Context;
typedef struct Value Value;
typedef struct {U32 status,code;const Value *value;} Result;
extern int lo_cli_main(int,const char**);
extern void lo_op_File_2Eopen_5Fpaths(Context*,const Value**,U64,U64,Result*);
extern void lo_context_cancel_after_polls(Context*,U64);
extern U64 lo_runtime_live_contexts(void);
extern U64 lo_runtime_live_values(void);
extern int open(const char*,int,...);
extern int close(int);
extern int dup2(int,int);
extern int fcntl(int,int,...);
extern Offset lseek(int,Offset,int);
extern long read(int,void*,Size);
extern int linkat(int,const char*,int,const char*,int);
extern int setenv(const char*,const char*,int);
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
int execvp(const char *program,char *const arguments[]){(void)program;(void)arguments;int marker=open("spawned",CREATE,0666);if(marker>=0)close(marker);*ERROR_ADDRESS()=5;return -1;}
static int contains(const char *text,const char *needle){for(;*text;++text){Size i=0;while(needle[i]&&text[i]==needle[i])++i;if(!needle[i])return 1;}return 0;}
void lo_files_probe_bridge(Context *context,const Value **args,U64 count,U64 site,Result *out){
 /* Two operation admission polls precede the before/after first hash read. */
 lo_context_cancel_after_polls(context,4);
 lo_op_File_2Eopen_5Fpaths(context,args,count,site,out);
}
int main(int argc,const char **argv){
 (void)argc;
#ifdef __APPLE__
 const char *executable=argv[0];
#else
 (void)argv;
 const char *executable="/probe";
#endif
 /* A hard link retains an actual executable without an installation step. */
 if(linkat(CURRENT_DIRECTORY,executable,CURRENT_DIRECTORY,"lsof",0)||setenv("PATH",".",1))return 10;
 int output=open("probe-cancellation-output",CREATE,0666),saved=fcntl(2,0,3);
 if(output<0||saved<0||dup2(output,2)<0)return 11;
 const char *arguments[]={"fixture","run","--format","json","--open-files-root",".","--write-root","."};
 int status=lo_cli_main(8,arguments);
 if(dup2(saved,2)<0||lseek(output,0,0)<0)return 12;
 char bytes[16384];long count=read(output,bytes,sizeof(bytes)-1);if(count<0)return 13;bytes[count]=0;
 if(status!=2||!contains(bytes,"runtime.cancelled")||contains(bytes,"File.OpenFilesFailed"))return 20;
 int spawned=open("spawned",0);if(spawned>=0){close(spawned);return 23;}
 int later=open("later",0);if(later>=0){close(later);return 21;}
 if(lo_runtime_live_contexts()||lo_runtime_live_values())return 22;
 close(output);close(saved);return 0;
}
