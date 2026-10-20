typedef __SIZE_TYPE__ Size;
extern int open(const char*,int,...);extern int dup2(int,int);extern int close(int);extern long lseek(int,long,int);extern long read(int,void*,Size);extern int lo_cli_main(int,const char**,const char**);
static int contains(const char *text,const char *needle){for(;*text;++text){Size i=0;while(needle[i]&&text[i]==needle[i])++i;if(!needle[i])return 1;}return 0;}
int main(void){
 int diagnostic=open("diagnostic.json",2|0x40|0x200,0666);if(diagnostic<0||dup2(diagnostic,2)<0)return 10;
 const char *arguments[]={"fixture","run","--format","json","--process-allow","0000000000000000000000000000000000000000000000000000000000000000,/unavailable-command","--process-launcher","/unavailable-launcher","--process-read-root","/unavailable-root"};
 int result=lo_cli_main(LO_CASE?10:4,arguments,0);if(result!=2||lseek(diagnostic,0,0)<0)return 11;
 char bytes[8192];long n=read(diagnostic,bytes,sizeof(bytes)-1);if(n<=0)return 12;bytes[n]=0;
 if(!contains(bytes,"\"code\":\"runtime.process-unavailable\"")||!contains(bytes,"Process activation requires a configured, qualified host execution profile"))return 20;
 close(diagnostic);return 0;
}
