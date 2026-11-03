#include "lo_runtime.h"
typedef __SIZE_TYPE__ Size;
extern int lo_cli_main(int,const char *const *,const char *const *);
extern void lo_op_File_2Eopen_5Fpaths(Context *,const Value **,uint64_t,uint64_t,ResultSlot *);
extern uint64_t lo_runtime_live_contexts(void),lo_runtime_live_values(void);
extern int open(const char *,int,...),close(int),rename(const char *,const char *),linkat(int,const char *,int,const char *,int),setenv(const char *,const char *,int),waitpid(int,int *,int);
extern long read(int,void *,Size),write(int,const void *,Size);
extern char *getcwd(char *,Size);
extern void *dlsym(void *,const char *);
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
static char root_path[4096],held_path[4096],replacement_template[4096];
static unsigned bridged;
static int equal(const char *left,const char *right){while(*left&&*left==*right){++left;++right;}return *left==*right;}
static int path(char *output,Size capacity,const char *base,const char *suffix){Size count=0;while(*base){if(count+1>=capacity)return 1;output[count++]=*base++;}while(*suffix){if(count+1>=capacity)return 1;output[count++]=*suffix++;}output[count]=0;return 0;}
static int put(const char *name,const char *bytes,Size size){int descriptor=open(name,CREATE,0666);if(descriptor<0)return 1;Size offset=0;while(offset<size){long count=write(descriptor,bytes+offset,size-offset);if(count<=0){close(descriptor);return 1;}offset+=(Size)count;}return close(descriptor);}
static char *temporary_directory(char *name){char *(*create)(char *)=(char *(*)(char *))dlsym((void *)-1,"mkdtemp");return create?create(name):0;}
int fchdir(int descriptor){
 int (*delegate)(int)=(int (*)(int))dlsym((void *)-1,"fchdir");
 if(!delegate){*ERROR_ADDRESS()=5;return -1;}
 if(LO_CASE==2){
  if(rename(root_path,held_path)||!temporary_directory(replacement_template)||rename(replacement_template,root_path)){*ERROR_ADDRESS()=5;return -1;}
  char replacement[4096];if(path(replacement,sizeof(replacement),root_path,"/replacement.log")||put(replacement,"replacement",11)){*ERROR_ADDRESS()=5;return -1;}
 }
 return delegate(descriptor);
}
void lo_probe_admission(Context *context,const Value **arguments,uint64_t count,uint64_t site,ResultSlot *out){
 ++bridged;
 if(setenv("PATH",LO_CASE==1?".":"missing",1)){out->status=2;out->code=2;out->value=0;return;}
 lo_op_File_2Eopen_5Fpaths(context,arguments,count,site,out);
}
int main(int argc,const char **argv){
 if(argc==4&&equal(argv[1],"-Fn")){
  int retained=open("retained.log",0),replacement=open("replacement.log",0);
  if(retained<0||replacement>=0)return 70;
  char byte=0;if(read(retained,&byte,1)!=1||byte!='r'||close(retained)||put("probe-ran","1",1))return 71;
  const char output[]="p1\nf3\nn./retained.log\n";
  return write(1,output,sizeof(output)-1)!=(long)(sizeof(output)-1)?72:0;
 }
 char cwd[4096],directory[]="grant-XXXXXX";
 if(!getcwd(cwd,sizeof(cwd))||!temporary_directory(directory)||rename(directory,"grant")||put("grant/retained.log","retained",8))return 10;
 if(path(root_path,sizeof(root_path),cwd,"/grant")||path(held_path,sizeof(held_path),cwd,"/held")||path(replacement_template,sizeof(replacement_template),cwd,"/replacement-XXXXXX"))return 11;
#ifdef __APPLE__
 const char *executable=argv[0];
#else
 (void)argv;const char *executable="/probe";
#endif
 if(linkat(CURRENT_DIRECTORY,executable,CURRENT_DIRECTORY,"lsof",0)||setenv("PATH",LO_CASE==1?"missing":".",1))return 12;
 const char *arguments[]={"probe-binding","run","--open-files-root","grant","--format","json"};
 int status=lo_cli_main(6,arguments,0);
 if(status||bridged!=1)return 20;
 int marker=open(LO_CASE==2?"held/probe-ran":"grant/probe-ran",0);
 if(LO_CASE==1){if(marker>=0){close(marker);return 21;}}
 else {char byte=0;if(marker<0||read(marker,&byte,1)!=1||byte!='1'||close(marker))return 22;}
 if(LO_CASE==2){int original=open("held/retained.log",0),replacement=open("grant/replacement.log",0);if(original<0||replacement<0)return 23;close(original);close(replacement);}
 int child_status;if(waitpid(-1,&child_status,1)!=-1||*ERROR_ADDRESS()!=10)return 24;
 return lo_runtime_live_contexts()||lo_runtime_live_values()?25:0;
}
