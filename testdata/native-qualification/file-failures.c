typedef __SIZE_TYPE__ Size;
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef __INT64_TYPE__ Offset;
typedef struct Context Context;
typedef struct Value Value;
typedef struct {U32 status,code;const Value *value;} Result;
extern void lo_qualified_entry(Context*,const Value**,U64,U64,Result*);
extern void lo_commit(Context*,const Value**,U64,U64,Result*);
extern void lo_context_cancel(Context*);
extern int lo_cli_main(int,const char**,const char**);
extern U64 lo_runtime_live_contexts(void),lo_runtime_live_values(void);
extern int open(const char*,int,...),fcntl(int,int,...),dup2(int,int),mkdir(const char*,unsigned),unlink(const char*);
extern long write(int,const void*,Size),read(int,void*,Size);
extern Offset lseek(int,Offset,int);
extern void *dlsym(void*,const char*);
#ifdef __APPLE__
extern int *__error(void);
#define ERROR_ADDRESS __error
#define CREATE (2|0x200|0x400)
#else
extern int *__errno_location(void);
#define ERROR_ADDRESS __errno_location
#define CREATE (2|0x40|0x200)
#endif
static Context *active;
static int attempts,removed,close_failures,armed,hook_error;
static int counts[4];
static void *(*real_readdir)(void*);
static int (*real_close)(int);
static int (*real_stat)(int,const char*,void*,int);
static int (*real_unlink)(int,const char*,int);
void *readdir(void *stream){
 void *entry=real_readdir(stream);if(entry&&LO_CASE==0){
#ifdef __APPLE__
 /* The selected libc dirent layout has a 21-byte name offset. */
 char *name=(char*)entry+21;
#else
 /* The selected glibc dirent layout has a 19-byte name offset. */
 char *name=(char*)entry+19;
#endif
 if(name[0]=='a'&&!name[1])name[0]=(char)255;
 }return entry;
}
int close(int fd){int result=real_close(fd);if(armed){++close_failures;*ERROR_ADDRESS()=5;return -1;}return result;}
static int put(const char *path,const char *text,Size size){int fd=open(path,CREATE,0666);if(fd<0)return 1;long n=write(fd,text,size);return real_close(fd)||n!=(long)size;}
static void grant_path(char *path,const char *name){const char prefix[]="grant/";Size n=0;while(prefix[n]){path[n]=prefix[n];++n;}while(*name)path[n++]=*name++;path[n]=0;}
int fstatat(int fd,const char *name,void *info,int flags){
 if(name[0]>='a'&&name[0]<='d'&&!name[1]){
  int count=++counts[name[0]-'a'];char path[32];grant_path(path,name);
  if(LO_CASE==2 && count==1){if(unlink(path))hook_error=1;}
  if((LO_CASE==3 && name[0]=='a'&&count==2)||(LO_CASE==4&&name[0]=='b'&&count==3))if(put(path,"changed",7))hook_error=1;
 }
 return real_stat(fd,name,info,flags);
}
int unlinkat(int fd,const char *name,int flags){
 ++attempts;
 if((LO_CASE==5||LO_CASE==8)&&attempts==3){if(LO_CASE==8)armed=1;*ERROR_ADDRESS()=5;return -1;}
 int result=real_unlink(fd,name,flags);
 if(!result){++removed;if((LO_CASE==6&&removed==1)||(LO_CASE==9&&removed==3))lo_context_cancel(active);if(LO_CASE==7&&removed==4)armed=1;}
 return result;
}
void lo_qualified_commit(Context*c,const Value**a,U64 n,U64 s,Result*out){lo_commit(c,a,n,s,out);if(LO_CASE==10&&!out->status)armed=1;}
void lo_entry(Context*c,const Value**a,U64 n,U64 s,Result*out){active=c;lo_qualified_entry(c,a,n,s,out);active=0;}
int main(void){
 real_readdir=(void*(*)(void*))dlsym((void*)-1,"readdir");real_close=(int(*)(int))dlsym((void*)-1,"close");real_stat=(int(*)(int,const char*,void*,int))dlsym((void*)-1,"fstatat");real_unlink=(int(*)(int,const char*,int))dlsym((void*)-1,"unlinkat");
 if(!real_readdir||!real_close||!real_stat||!real_unlink)return 10;
 if(mkdir("grant",0777))return 11;
 if(LO_CASE==0){if(put("grant/a","x",1))return 12;}
 else if(LO_CASE==1){for(unsigned index=0;index<10001;++index){char path[]="grant/entry00000";unsigned number=index;for(int digit=14;digit>=10;--digit){path[digit]=(char)('0'+number%10);number/=10;}if(put(path,"",0))return 13;}}
 else if(LO_CASE!=10){unsigned size=(LO_CASE>=3&&LO_CASE<=9)?4:1;for(unsigned i=0;i<size;++i){char path[]="grant/a";path[6]+=(char)i;if(put(path,"x",1))return 14;}}
 int output=open("diagnostic",CREATE,0666),saved_out=fcntl(1,0,3),saved_err=fcntl(2,0,3);
 if(output<0||saved_out<0||saved_err<0||dup2(output,1)<0||dup2(output,2)<0)return 15;
 const char *args[]={"fixture","run","--format","json","--list-root","grant","--remove-root","grant","--write-root","grant","--max-steps",LO_CASE==11?"150":"10000000"};
 int status=lo_cli_main(12,args,0);armed=0;
 if(dup2(saved_out,1)<0||dup2(saved_err,2)<0||lseek(output,0,0)<0)return 16;
 char diagnostic[65536];long count=read(output,diagnostic,sizeof(diagnostic)-1);if(count<=0||count==(long)sizeof(diagnostic)-1)return 17;
 int expected=(LO_CASE==0||LO_CASE==2||LO_CASE==3||LO_CASE==4||LO_CASE==5||LO_CASE==8)?1:2;
 if(status!=expected||hook_error){write(2,diagnostic,(Size)count);return 20;}
 if(LO_CASE>=3&&LO_CASE<=9){const int expected_removed[]={0,1,2,1,4,2,3};const int expected_attempts[]={0,1,3,1,4,3,3};if(removed!=expected_removed[LO_CASE-3]||attempts!=expected_attempts[LO_CASE-3])return 21;
  int present=0;for(unsigned i=0;i<4;++i){char path[]="grant/a";path[6]+=(char)i;int fd=open(path,0);if(fd>=0){++present;real_close(fd);}}if(present!=4-removed)return 22;
 }
 if((LO_CASE==7||LO_CASE==8||LO_CASE==10)&&!close_failures)return 23;
 if(LO_CASE==10){int fd=open("grant/written",0);char text[4]={0};if(fd<0||read(fd,text,4)!=3||text[0]!='a'||text[1]!='b'||text[2]!='c')return 24;real_close(fd);}
 if(LO_CASE>=11&&removed!=1)return 25;
 if(lo_runtime_live_values()||lo_runtime_live_contexts())return 26;
 real_close(output);real_close(saved_out);real_close(saved_err);
 return write(1,diagnostic,(Size)count)==count?0:27;
}
