typedef __SIZE_TYPE__ Size;typedef __INT64_TYPE__ Offset;
extern int lo_cli_main(int,const char**,const char**);extern int open(const char*,int,...);extern int close(int);extern int dup2(int,int);extern long lseek(int,long,int);extern long read(int,void*,Size);extern long write(int,const void*,Size);extern int fcntl(int,int,...);extern char *getcwd(char*,Size);extern int getpid(void);extern int kill(int,int);extern int *__error(void);extern void *dlsym(void*,const char*);
static unsigned admissions,spawns;
static Size length(const char *text){Size n=0;while(text[n])++n;return n;}
static void append(char *text,const char *tail){Size at=length(text),i=0;while((text[at+i]=tail[i]))++i;}
static int ends(const char *text,const char *tail){Size a=length(text),b=length(tail);if(a<b)return 0;for(Size i=0;i<b;++i)if(text[a-b+i]!=tail[i])return 0;return 1;}
static int contains(const char *text,const char *needle){for(;*text;++text){Size i=0;while(needle[i]&&text[i]==needle[i])++i;if(!needle[i])return 1;}return 0;}
long pread(int descriptor,void *bytes,Size size,Offset offset){
 long(*observed)(int,void*,Size,Offset)=(long(*)(int,void*,Size,Offset))dlsym((void*)-1,"pread");
 long result=observed(descriptor,bytes,size,offset);char path[4096];
 if(offset==0&&fcntl(descriptor,50,path)==0&&ends(path,"/cancel-command")){
  ++admissions;if(admissions==2)kill(getpid(),2);
 }
 return result;
}
int posix_spawn(int *pid,const char *path,const void *actions,const void *attributes,char *const *argv,char *const *env){(void)pid;(void)path;(void)actions;(void)attributes;(void)argv;(void)env;++spawns;return 5;}
static int file(const char *path,const char *content){int fd=open(path,2|0x200|0x400,0666);if(fd<0)return -1;Size n=length(content);if(write(fd,content,n)!=(long)n)return -1;return close(fd);}
int main(void){
 char directory[4096],command[4096],grant[4200],helper[4096];if(!getcwd(directory,sizeof(directory)))return 10;
 command[0]=0;append(command,directory);append(command,"/cancel-command");if(file(command,"reviewed bytes"))return 11;
 if(file("command-path",command)||file("directory-path",directory))return 12;
 grant[0]=0;append(grant,"2f70b28379ec7304447e353e98395457080ae10bc139b695de16828b63efc54a,");append(grant,command);
 const char *source=__FILE__;Size index=0;for(;source[index];++index){if(source[index]=='/'&&source[index+1]=='t'&&source[index+2]=='e'&&source[index+3]=='s'&&source[index+4]=='t'&&source[index+5]=='d'&&source[index+6]=='a'&&source[index+7]=='t'&&source[index+8]=='a'&&source[index+9]=='/')break;helper[index]=source[index];}if(!source[index])return 13;helper[index]=0;append(helper,"/bin/lo-process-launcher");
 int diagnostic=open("diagnostic.json",2|0x200|0x400,0666);if(diagnostic<0||dup2(diagnostic,2)<0)return 14;
 const char *arguments[]={"fixture","run","--format","json","--read-root",".","--process-allow",grant,"--process-launcher",helper,"--process-read-root",directory};
 int status=lo_cli_main(12,arguments,0);if(status!=2)return 20;if(admissions!=2)return 23;if(spawns!=0)return 24;if(lseek(diagnostic,0,0)<0)return 25;
 char bytes[8192];long count=read(diagnostic,bytes,sizeof(bytes)-1);if(count<=0)return 21;bytes[count]=0;if(!contains(bytes,"\"code\":\"runtime.cancelled\"")||contains(bytes,"Process.ExecutionFailed"))return 22;return 0;
}
