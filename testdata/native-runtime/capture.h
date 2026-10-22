#ifndef LO_TEST_CAPTURE_H
#define LO_TEST_CAPTURE_H
/* The Go scenario runner owns and precreates this file in its private cwd. */
typedef __SIZE_TYPE__ LoCaptureSize;
extern int open(const char *,int,...),close(int),dup2(int,int),fcntl(int,int,...),unlink(const char *);
extern long read(int,void *,LoCaptureSize),lseek(int,long,int);
#define LO_CAPTURE_LIMIT (4U*1024U*1024U)
static char lo_capture_bytes[LO_CAPTURE_LIMIT+1];
static LoCaptureSize lo_capture_length;
static const char lo_capture_path[]=".lo-native-capture";
static int lo_capture_fd=-1,lo_capture_out=-1,lo_capture_err=-1,lo_capture_owned;
static int lo_capture_restore(void){
 int failed=0;
 if(lo_capture_out>=0){if(dup2(lo_capture_out,1)<0)failed=1;if(close(lo_capture_out))failed=1;lo_capture_out=-1;}
 if(lo_capture_err>=0){if(dup2(lo_capture_err,2)<0)failed=1;if(close(lo_capture_err))failed=1;lo_capture_err=-1;}
 return failed;
}
static int lo_capture_cleanup(int primary){
 int failed=lo_capture_restore();
 if(lo_capture_fd>=0){if(close(lo_capture_fd))failed=1;lo_capture_fd=-1;}
 if(lo_capture_owned){if(unlink(lo_capture_path))failed=1;lo_capture_owned=0;}
 return primary?primary:failed?16:0;
}
static int lo_capture_start(void){
 lo_capture_length=0;lo_capture_owned=1;
 lo_capture_fd=open(lo_capture_path,2);
 if(lo_capture_fd<0)return lo_capture_cleanup(11);
 lo_capture_out=fcntl(1,0,3);lo_capture_err=fcntl(2,0,3);
 if(lo_capture_out<0||lo_capture_err<0||dup2(lo_capture_fd,1)<0||dup2(lo_capture_fd,2)<0)return lo_capture_cleanup(12);
 return 0;
}
static int lo_capture_finish(void){
 if(lo_capture_restore())return lo_capture_cleanup(13);
 if(lseek(lo_capture_fd,0,0)<0)return lo_capture_cleanup(14);
 for(;;){
  char extra;
  long count=lo_capture_length==LO_CAPTURE_LIMIT?read(lo_capture_fd,&extra,1):read(lo_capture_fd,lo_capture_bytes+lo_capture_length,LO_CAPTURE_LIMIT-lo_capture_length);
  if(count<0)return lo_capture_cleanup(14);
  if(!count)break;
  if(lo_capture_length==LO_CAPTURE_LIMIT)return lo_capture_cleanup(15);
  lo_capture_length+=(LoCaptureSize)count;
 }
 lo_capture_bytes[lo_capture_length]=0;
 return lo_capture_cleanup(0);
}
#endif
