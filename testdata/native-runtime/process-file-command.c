extern long write(int,const void *,unsigned long);
extern int strcmp(const char *,const char *);
int main(int argc,char **argv){
 if(argc!=2)return 70;
 if(!strcmp(argv[1],"large")){
  unsigned char bytes[8192];const char secret[]="process-charge-private-payload";
  for(unsigned index=0;index<sizeof(bytes);++index)bytes[index]=0x80;
  for(unsigned index=0;index<sizeof(secret)-1;++index)bytes[index]=(unsigned char)secret[index];
  unsigned long offset=0;while(offset<sizeof(bytes)){long count=write(1,bytes+offset,sizeof(bytes)-offset);if(count<=0)return 71;offset+=(unsigned long)count;}
  return 0;
 }
 if(strcmp(argv[1],"0")&&strcmp(argv[1],"7"))return 72;
 const unsigned char bytes[]={0,255,'a'};
 if(write(1,bytes,sizeof(bytes))!=(long)sizeof(bytes))return 73;
 return !strcmp(argv[1],"7")?7:0;
}
