extern long write(int,const void*,unsigned long);
extern long read(int,void*,unsigned long);
extern unsigned long strlen(const char*);
int main(int argc,char **argv){char b[16];long n=read(0,b,sizeof b);write(1,"stdout\n",7);write(2,"stderr\n",7);if(argc>1)write(1,argv[1],strlen(argv[1]));if(n>0)write(1,b,(unsigned long)n);return 9;}
