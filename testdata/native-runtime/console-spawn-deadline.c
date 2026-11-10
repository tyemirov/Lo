#include "lo_process.h"
#define main unused_inheritance_main
#include "console-spawn-inheritance.c"
#undef main
static LoProcessResult receipt;
static int process_done;
static int never_cancel(const void *unused){(void)unused;return 0;}
static void *process(void *unused) {
 (void)unused;
 LoProcessRequest request={0};request.timeout_ms=50;request.cleanup_ms=1000;request.stop=never_cancel;
 lo_native_process_execute("./absent-task-launcher",0,".",0,0,&request,&receipt);
 __atomic_store_n(&process_done,1,__ATOMIC_SEQ_CST);
 return 0;
}
int main(void) {
 real_pipe=(int (*)(int *))dlsym((void *)-1,"pipe");
 if(!real_pipe)return 51;
 int null=open("/dev/null",1);
 if(null<0 || dup2(null,1)<0)return 52;
 close(null);
 __atomic_store_n(&armed,1,__ATOMIC_SEQ_CST);
 Thread first,second;
 if(pthread_create(&first,0,console,0))return 53;
 unsigned tries;
 for(tries=0;tries<5000 && !__atomic_load_n(&paused,__ATOMIC_SEQ_CST);++tries)poll(0,0,1);
 if(!__atomic_load_n(&paused,__ATOMIC_SEQ_CST))return 54;
 if(pthread_create(&second,0,process,0))return 55;
 for(tries=0;tries<250 && !__atomic_load_n(&process_done,__ATOMIC_SEQ_CST);++tries)poll(0,0,1);
 int completed_while_queued=__atomic_load_n(&process_done,__ATOMIC_SEQ_CST);
 __atomic_store_n(&released,1,__ATOMIC_SEQ_CST);
 if(pthread_join(first,0) || pthread_join(second,0))return 56;
 if(!completed_while_queued)return 57;
 if(receipt.stage!=1 || receipt.class_id!=LO_PROCESS_TIMEOUT || receipt.started || receipt.cleanup_complete!=1 || receipt.ownership)return 58;
 if(console_status || lo_runtime_live_contexts() || lo_runtime_live_values())return 59;
 return 0;
}
