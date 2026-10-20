typedef __SIZE_TYPE__ Size;
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef __INT64_TYPE__ Offset;
typedef struct Context Context;
typedef struct Value Value;
typedef struct { U32 status,code; const Value *value; } Result;
extern int lo_cli_main(int,const char **,const char **);
extern void lo_op_File_2Elist(Context*,const Value**,U64,U64,Result*);
extern void lo_op_File_2ERemove_2Enew(Context*,const Value**,U64,U64,Result*);
extern void lo_value_retain(const Value*);
extern void lo_value_release(const Value*);
extern U64 lo_runtime_live_values(void);
extern U64 lo_runtime_live_contexts(void);
extern int open(const char*,int,...);
extern int close(int);
extern int dup2(int,int);
extern int fcntl(int,int,...);
extern long write(int,const void*,Size);
extern long read(int,void*,Size);
extern Offset lseek(int,Offset,int);
extern int mkdir(const char*,unsigned);
extern int rename(const char*,const char*);
#ifdef __APPLE__
#define CREATE (2|0x200|0x400)
#else
#define CREATE (2|0x40|0x200)
#endif
static const Value *retained;
static int invocation;
static int hook_error;
static int put(const char *path,const char *data,Size count){int file=open(path,CREATE,0666);if(file<0)return 1;long n=write(file,data,count);return close(file)||n!=(long)count;}
static int contains(const char *text,const char *needle){for(;*text;++text){Size i=0;while(needle[i]&&text[i]==needle[i])++i;if(!needle[i])return 1;}return 0;}
void lo_files_list_bridge(Context *context,const Value **args,U64 count,U64 site,Result *out){
 lo_op_File_2Elist(context,args,count,site,out);
 if(out->status)return;
 if(LO_CASE==0){
  if(!invocation){retained=out->value;lo_value_retain(retained);}else{lo_value_release(out->value);lo_value_retain(retained);out->value=retained;}
 }else if(LO_CASE==2){
  if(rename("grant","held")||mkdir("grant",0777)||put("grant/selected","replacement",11))hook_error=1;
 }
}
void lo_files_selection_bridge(Context *context,const Value **args,U64 count,U64 site,Result *out){
 lo_op_File_2ERemove_2Enew(context,args,count,site,out);
 if(!out->status&&LO_CASE==1&&put("grant/selected","changed",7))hook_error=1;
}
int main(void){
 if(mkdir("grant",0777)||put("grant/selected","original",8))return 10;
 int output=open("observation-output",CREATE,0666), saved_out=fcntl(1,0,3),saved_err=fcntl(2,0,3);
 if(output<0||saved_out<0||saved_err<0||dup2(output,1)<0||dup2(output,2)<0)return 11;
 const char *args[]={"fixture","run","--format","json","--list-root","grant","--remove-root","grant","--write-root","grant"};
 int status=lo_cli_main(10,args,0);
 if(LO_CASE==0){if(status)return 12;invocation=1;status=lo_cli_main(10,args,0);lo_value_release(retained);}
 if(dup2(saved_out,1)<0||dup2(saved_err,2)<0||lseek(output,0,0)<0)return 13;
 char bytes[32768];long count=read(output,bytes,sizeof(bytes)-1);if(count<0)return 14;bytes[count]=0;
 if(hook_error)return 15;
 if(LO_CASE==2){if(status!=2||!contains(bytes,"runtime.capability-denied")||!contains(bytes,"removal_progress"))return 20;int old=open("held/selected",0),replacement=open("grant/selected",0);if(old<0||replacement<0)return 21;close(old);close(replacement);}
 else {if(status||!contains(bytes,LO_CASE==0?"File.InvalidSelection":"File.EntryChanged"))return 22;}
 int later=open("grant/later",0);if(later>=0){close(later);return 23;}
 int original=open("grant/selected",0);if(original<0)return 24;close(original);
 if(lo_runtime_live_contexts()||lo_runtime_live_values())return 25;
 close(output);close(saved_out);close(saved_err);return 0;
}
