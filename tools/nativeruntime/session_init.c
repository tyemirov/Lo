/* Persistent software-guest request executor. All case state belongs to PID 1. */
typedef struct stream FILE;
extern int mount(const char *,const char *,const char *,unsigned long,const void *);
extern int umount2(const char *,int),mkdir(const char *,unsigned int),chdir(const char *),chroot(const char *);
extern int link(const char *,const char *),unlink(const char *),rmdir(const char *);
extern int open(const char *,int,...),close(int),dup2(int,int),fork(void),execve(const char *,char *const[],char *const[]);
extern int waitpid(int,int *,int),kill(int,int),setpgid(int,int),usleep(unsigned int),reboot(int);
extern int close_range(unsigned int,unsigned int,int);
extern int unshare(int);
extern int socket(int,int,int),ioctl(int,unsigned long,...);
extern int *__errno_location(void);
extern long read(int,void *,unsigned long),write(int,const void *,unsigned long),lseek(int,long,int);
extern int snprintf(char *,unsigned long,const char *,...),puts(const char *);
extern FILE *fopen(const char *,const char *);
extern int fclose(FILE *),fileno(FILE *),fflush(FILE *);
extern unsigned long fread(void *,unsigned long,unsigned long,FILE *),fwrite(const void *,unsigned long,unsigned long,FILE *);
extern void _exit(int);
struct timespec {long seconds;long nanoseconds;};
extern int clock_gettime(int,struct timespec *);
typedef struct directory DIR;
struct dirent {unsigned long inode;long offset;unsigned short length;unsigned char type;char name[256];};
extern DIR *opendir(const char *);extern struct dirent *readdir(DIR *);extern int closedir(DIR *);

#define REQUEST_MAGIC 0x4c4f4731U
#define RESPONSE_MAGIC 0x4c4f5231U
#define STREAM_LIMIT (64U*1024U*1024U)
#define ARGUMENT_LIMIT (1024U*1024U)
#define CASE_NAMESPACES (0x00020000|0x04000000|0x08000000|0x40000000)
static char arguments[ARGUMENT_LIMIT],*argv[4096],*environment[4096];
static int channel;

static long long now(void){struct timespec t;if(clock_gettime(1,&t))return -1;return t.seconds*1000000000LL+t.nanoseconds;}
static unsigned int decode(const unsigned char *p){return ((unsigned int)p[0]<<24)|((unsigned int)p[1]<<16)|((unsigned int)p[2]<<8)|p[3];}
static void encode(unsigned char *p,unsigned int v){p[0]=v>>24;p[1]=v>>16;p[2]=v>>8;p[3]=v;}
static int receive(void *buffer,unsigned long size){
 unsigned char *bytes=buffer;while(size){long n=read(channel,bytes,size);if(n<=0)return -1;bytes+=n;size-=n;}return 0;
}
static int send(const void *buffer,unsigned long size){
 const unsigned char *bytes=buffer;while(size){long n=write(channel,bytes,size);if(n<=0)return -1;bytes+=n;size-=n;}return 0;
}
static int transfer(const char *path,unsigned long length){
 FILE *file=fopen(path,"wb");if(!file)return -1;
 unsigned char buffer[65536];int failed=0;
 while(length){unsigned long size=length>sizeof(buffer)?sizeof(buffer):length;
  if(receive(buffer,size)||fwrite(buffer,1,size,file)!=size){failed=1;break;}length-=size;
 }
 if(fclose(file))failed=1;return failed?-1:0;
}
static int output(FILE *file,unsigned long size){
 unsigned char buffer[65536];if(lseek(fileno(file),0,0)<0)return -1;
 while(size){unsigned long length=size>sizeof(buffer)?sizeof(buffer):size;
  if(fread(buffer,1,length,file)!=length||send(buffer,length))return -1;size-=length;
 }return 0;
}
static int stop_case(void){
 for(;;){
  if(kill(-1,9)&&*__errno_location()!=3)return -1;
  int result=waitpid(-1,0,1);if(result>0)continue;
  if(result<0){int error=*__errno_location();if(error==10)return 0;if(error!=4)return -1;}
  if(usleep(1000)&&*__errno_location()!=4)return -1;
 }
}
static int loopback(void){
 int fd=socket(2,2,0);if(fd<0)return -1;
 struct {char name[16];short flags;char padding[22];} request={{'l','o',0},0x49,{0}};
 int result=ioctl(fd,0x8914,&request);close(fd);return result;
}
static int case_root(void){
 if(mkdir("/case/lib",511)||mkdir("/case/dev",511)||mkdir("/case/proc",511)||mkdir("/case/tmp",511))return -1;
 if(link("/base/probe","/case/probe"))return -1;
 if(mount("/base/lib","/case/lib",0,4096,0)||mount(0,"/case/lib",0,4096|32|1,0))return -1;
 if(mount("proc","/case/proc","proc",0,0)||mount("devtmpfs","/case/dev","devtmpfs",0,0))return -1;
 return 0;
}
static int remove_children(const char *path){
 DIR *directory=opendir(path);if(!directory)return -1;struct dirent *entry;int failed=0;
 for(;;){*__errno_location()=0;entry=readdir(directory);if(!entry){if(*__errno_location())failed=1;break;}
  if(entry->name[0]=='.'&&(!entry->name[1]||(entry->name[1]=='.'&&!entry->name[2])))continue;
  char child[4096];int length=snprintf(child,sizeof(child),"%s/%s",path,entry->name);
  if(length<0||(unsigned long)length>=sizeof(child)){failed=1;break;}
  if(entry->type==4){if(remove_children(child)||rmdir(child)){failed=1;break;}}
  else if(!entry->type||unlink(child)){failed=1;break;}
 }
 if(closedir(directory))failed=1;return failed?-1:0;
}
static int clear_root(void){
 if(umount2("/case/dev",0)||umount2("/case/proc",0)||umount2("/case/lib",0))return -1;
 return remove_children("/case");
}
static char *item(unsigned long *position,unsigned long length){
 if(*position>=length)return 0;char *value=arguments+*position;
 while(*position<length&&arguments[*position])++*position;
 if(*position>=length)return 0;++*position;return value;
}
static int directory(const char *cwd){
 char path[4102];int length=snprintf(path,sizeof(path),"/case%s",cwd);if(length<0||(unsigned long)length>=sizeof(path))return -1;
 for(int i=6;i<=length;i++)if(!path[i]||path[i]=='/'){
  char held=path[i];path[i]=0;if(mkdir(path,511)&&*__errno_location()!=17)return -1;path[i]=held;
 }return 0;
}
static int execute(const unsigned int *request){
 unsigned int identifier=request[1],timeout=request[2],executable=request[3],companion=request[4],arglen=request[5],stdinlen=request[6],capture=request[7];
 if(!identifier||!timeout||timeout>120000||!executable||executable>STREAM_LIMIT||companion>STREAM_LIMIT||!arglen||arglen>=ARGUMENT_LIMIT||stdinlen>STREAM_LIMIT||capture>1)return -1;
 if(case_root())return -1;
 if(transfer("/case/probe",executable))return -1;
 if(companion&&(link("/base/companion","/case/companion")||transfer("/case/companion",companion)))return -1;
 if(receive(arguments,arglen)||arguments[arglen-1])return -1;
 if(arglen<10)return -1;
 unsigned long count=decode((unsigned char *)arguments),variables=decode((unsigned char *)arguments+4),position=8;
 if(!count||count>=4095||variables>=4095)return -1;
 for(unsigned long i=0;i<count;i++)if(!(argv[i]=item(&position,arglen)))return -1;argv[count]=0;
 for(unsigned long i=0;i<variables;i++)if(!(environment[i]=item(&position,arglen)))return -1;environment[variables]=0;
 char *cwd=item(&position,arglen);if(!cwd||cwd[0]!='/'||position!=arglen||directory(cwd))return -1;
 FILE *argument_file=fopen("/case/arguments","wb");
 if(!argument_file||fwrite(arguments,1,arglen,argument_file)!=arglen||fclose(argument_file))return -1;
 if(transfer("/case/stdin",stdinlen))return -1;
 if(capture){char path[4128];int length=snprintf(path,sizeof(path),"/case%s/.lo-native-capture",cwd);
  if(length<0||(unsigned long)length>=sizeof(path))return -1;FILE *file=fopen(path,"wb");if(!file||fclose(file))return -1;}
 FILE *input=fopen("/case/stdin","rb"),*out=fopen("/stdout","w+b"),*err=fopen("/stderr","w+b");
 if(!input||!out||!err)return -1;
 long long start=now();if(start<0)return -1;
 int child=fork();if(child<0)return -1;
 if(!child){
  if(unshare(CASE_NAMESPACES)||loopback()||setpgid(0,0)||chroot("/case")||chdir(cwd)||dup2(fileno(input),0)<0||dup2(fileno(out),1)<0||dup2(fileno(err),2)<0)_exit(126);
  if(close_range(3,~0U,0))_exit(126);
  execve("/probe",argv,environment);_exit(127);
 }
 int status=0,timed_out=0;
 for(;;){int result=waitpid(child,&status,1);if(result==child)break;if(result<0&&*__errno_location()!=4)return -1;
  if(now()-start>=(long long)timeout*1000000LL){timed_out=1;break;}usleep(1000);
 }
 if(stop_case())return -1;
 unsigned int code=timed_out?124:((status&127)?128+(status&127):(status>>8)&255);
 long outlen=lseek(fileno(out),0,2),errlen=lseek(fileno(err),0,2);long long elapsed=now()-start;
 if(outlen<0||outlen>STREAM_LIMIT||errlen<0||errlen>STREAM_LIMIT)return -1;
 if(fclose(input)||clear_root())return -1;
 unsigned char header[32];unsigned int fields[]={RESPONSE_MAGIC,identifier,code,(unsigned int)outlen,(unsigned int)errlen,(unsigned int)(elapsed>>32),(unsigned int)elapsed,0};
 for(int i=0;i<8;i++)encode(header+4*i,fields[i]);
 if(send(header,sizeof(header))||output(out,(unsigned long)outlen)||output(err,(unsigned long)errlen))return -1;
 if(fclose(out)||fclose(err))return -1;
 return 0;
}
int main(void){
 if(mount("devtmpfs","/dev","devtmpfs",0,0)||mount("proc","/proc","proc",0,0))return 70;
 int console=open("/dev/console",2);if(console<0)return 71;for(int i=0;i<3;i++)if(dup2(console,i)<0)return 72;
 channel=open("/dev/vport0p1",2);if(channel<0)return 73;
 if(loopback()){puts("guest session: loopback unavailable");return 74;}
 if(case_root()){puts("guest session: private case filesystem unavailable");fflush(0);return 75;}
 if(clear_root())return 76;
 if(send("LOG_READY\n",10))return 77;
 unsigned char header[32];
 while(!receive(header,sizeof(header))){unsigned int fields[8];for(int i=0;i<8;i++)fields[i]=decode(header+4*i);
  if(fields[0]!=REQUEST_MAGIC||execute(fields)){puts("guest session: request or case cleanup failed");fflush(0);return 78;}
 }
 reboot(0x4321fedc);return 0;
}
