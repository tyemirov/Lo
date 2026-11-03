#include "lo_runtime.h"
#include "lo_file_observer.h"
#include "capture.h"
extern void lo_observed_entry(Context *,const Value *const *,uint64_t,uint64_t,ResultSlot *);
extern int lo_cli_main(int,const char *const *,const char *const *);
extern uint64_t lo_runtime_live_contexts(void),lo_runtime_live_values(void);
static Context *active;
static unsigned observations,steady_descriptors;
static int invalid;
static unsigned descriptors(void){unsigned count=0;for(int descriptor=0;descriptor<256;++descriptor)if(fcntl(descriptor,1)>=0)++count;return count;}
static int contains(const char *bytes,const char *needle){for(;*bytes;++bytes){const char *a=bytes,*b=needle;while(*a&&*a==*b){++a;++b;}if(!*b)return 1;}return 0;}
static int observe(uintptr_t user,uint32_t phase,const unsigned char *root,uint64_t root_size,const unsigned char *name,uint64_t name_size,LoFileMetadata *metadata){
 (void)name;(void)name_size;
 if(user!=81||!root||!root_size||!metadata){invalid=1;return -1;}
 if(phase==LO_FILE_DIRECTORY){
  ++observations;unsigned current=descriptors();
  if(observations==1)steady_descriptors=current;
  else if(current!=steady_descriptors){invalid=2;return -1;}
  if(LO_CASE%2 && observations==257)lo_context_cancel(active);
 }
 return 0;
}
void lo_entry(Context *context,const Value *const *arguments,uint64_t count,uint64_t site,ResultSlot *result){
 active=context;
 if(!lo_context_file_metadata_observer((LoContext *)context,observe,81))invalid=3;
 lo_observed_entry(context,arguments,count,site,result);active=0;
}
int main(void){
 int capture=lo_capture_start();if(capture)return capture;
 unsigned before=descriptors();
 const char *arguments[]={"observation-lifetime","run","--format","json","--list-root",".","--max-steps","1000000"};
 int status=lo_cli_main(8,arguments,0);
 unsigned after=descriptors();
 capture=lo_capture_finish();if(capture)return capture;
 if(invalid||before!=after)return 30+invalid;
 if(LO_CASE%2){if(status!=2||observations!=257||!contains(lo_capture_bytes,"runtime.cancelled"))return 40;}
 else if(status||observations!=512||!contains(lo_capture_bytes,"\"value\":\"512\""))return 41;
 return lo_runtime_live_contexts()||lo_runtime_live_values()?42:0;
}
