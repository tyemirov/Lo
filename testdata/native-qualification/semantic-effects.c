#include "capture.h"
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef __SIZE_TYPE__ Size;
typedef struct Context Context;
typedef struct Value Value;
typedef struct {U32 status,code;const Value *value;} Result;
extern void lo_semantic_entry(Context*,const Value**,U64,U64,Result*);
extern void lo_op_File_2Eread(Context*,const Value**,U64,U64,Result*);
extern void lo_commit(Context*,const Value**,U64,U64,Result*);
extern void lo_context_cancel(Context*);
extern int lo_cli_main(int,const char**,const char**);
extern U64 lo_runtime_live_contexts(void),lo_runtime_live_values(void);
extern int open(const char*,int,...),close(int),dup2(int,int),fcntl(int,int,...),unlink(const char*);
extern long lseek(int,long,int);
extern long write(int,const void*,Size),read(int,void*,Size);
extern Size strlen(const char*);
#ifdef __APPLE__
#define CREATE (2|0x200|0x400)
#else
#define CREATE (2|0x40|0x200)
#endif
static unsigned reads,commits;
static int hook_error;
static int contains(const char *text,const char *needle){for(;*text;++text){Size i=0;while(needle[i]&&text[i]==needle[i])++i;if(!needle[i])return 1;}return 0;}
static int put(const char*path,const char*text){int fd=open(path,CREATE,0666);if(fd<0)return 1;Size size=strlen(text);long count=write(fd,text,size);return close(fd)||count!=(long)size;}
void lo_semantic_read(Context*c,const Value**a,U64 n,U64 s,Result*out){
 ++reads;
 if(LO_CASE==0){const char*names[]={"unused","repeat","b","a"};for(unsigned i=0;i<4;++i){char text[64];Size size=strlen(names[i]);for(Size j=0;j<size;++j)text[j]=names[i][j];text[size++]=':';text[size++]=(char)('0'+reads);text[size++]=';';text[size]=0;if(put(names[i],text))hook_error=1;}}
 lo_op_File_2Eread(c,a,n,s,out);
}
void lo_semantic_commit(Context*c,const Value**a,U64 n,U64 s,Result*out){++commits;lo_commit(c,a,n,s,out);}
void lo_entry(Context*c,const Value**a,U64 n,U64 s,Result*out){if(LO_CASE==3)lo_context_cancel(c);lo_semantic_entry(c,a,n,s,out);}
static int large_output(Size count){
 const char prefix[]="{\"command\":\"run\",\"status\":\"ok\",\"result\":{\"type\":\"String\",\"value\":\"";
 const char suffix[]="CAPTURE_TRAILER_B005\"}}\n";
 Size head=sizeof(prefix)-1,body=2U*1024U*1024U,tail=sizeof(suffix)-1;
 if(count!=head+body+tail)return 1;
 for(Size i=0;i<head;++i)if(lo_capture_bytes[i]!=prefix[i])return 1;
 for(Size i=0;i<body;++i)if(lo_capture_bytes[head+i]!='A')return 1;
 for(Size i=0;i<tail;++i)if(lo_capture_bytes[head+body+i]!=suffix[i])return 1;
 return 0;
}
int main(void){
 int failure=0;
 if(LO_CASE==1 && (put("selector","selector")||put("selected","selected")||put("unselected","unselected")||put("unreached-body","unreached-body")||put("default","default")))return 10;
 if(LO_CASE>=2){failure=lo_capture_start();if(failure)return failure;}
 const char*args[]={"semantic-fixture","run","--format","json","--read-root",".","--write-root","."};
 int status=lo_cli_main(8,args,0);
 if(LO_CASE>=2){
  failure=lo_capture_finish();if(failure)return failure;char *output=lo_capture_bytes;Size count=lo_capture_length;
  if(LO_CASE==2){if(status || !contains(output,"\"kind\":\"File.ReadFailed\"") || !contains(output,"\"file_path\":{\"type\":\"File.Path\",\"value\":\"first\"}") || !contains(output,"\"flow\":{\"type\":\"String\",\"value\":\"Main\"}") || contains(output,"\"value\":\"later\"")){write(2,output,count);failure=20;}}
  else if(LO_CASE==3){if(status!=2 || !contains(output,"runtime.cancelled") || contains(output,"File.ReadFailed") || contains(output,"\"status\":\"ok\"")){write(2,output,count);failure=21;}}
  else if(LO_CASE==4 && (status||large_output(count)))failure=23;
 }else if(status)failure=22;
 unsigned expected=LO_CASE==0?5:LO_CASE==1?2:LO_CASE==2?1:0;
 if(reads!=expected||commits||hook_error)failure=30;
 int later=open("unwritten",0);if(later>=0){close(later);failure=31;}
 if(lo_runtime_live_contexts()||lo_runtime_live_values())failure=32;
 if(!failure&&LO_CASE==4){const char done[]="capture-complete\n";if(write(1,done,sizeof(done)-1)!=(long)(sizeof(done)-1))failure=33;}
 return failure;
}
