#include "lo_process.h"
struct Clock { long seconds,nanoseconds; };
struct Event { uintptr_t ident;short filter;unsigned short flags;unsigned int fflags;long data;void *user; };
struct Port { int descriptor;short events,revents; };
static uint64_t now, duration;
static unsigned below, signals, reaped;
int clock_gettime(int clock,struct Clock *value) {
    (void)clock;value->seconds=(long)(now/1000);value->nanoseconds=(long)(now%1000)*1000000;return 0;
}
int posix_spawn(int *pid,const char *path,const void *actions,const void *attributes,char *const *arguments,char *const *environment) {
    (void)path;(void)actions;(void)attributes;(void)arguments;(void)environment;
    *pid=4242;now=duration-1;return 0;
}
int kevent(int queue,const struct Event *change,int changes,struct Event *events,int capacity,const void *timeout) {
    (void)queue;(void)change;(void)events;(void)capacity;(void)timeout;
    if(!changes && now==duration-1){++below;if(signals)return -1;now=duration;}
    return 0;
}
int poll(struct Port *ports,unsigned int count,int timeout) {
    (void)timeout;for(unsigned int index=0;index<count;++index)ports[index].revents=0;return 0;
}
int kill(int pid,int signal){if(pid!=4242 || signal!=9)return -1;++signals;return 0;}
int waitpid(int pid,int *status,int flags){(void)flags;if(pid!=4242 || !signals)return 0;*status=9;++reaped;return pid;}
static int stop(const void *token){(void)token;return 0;}
int main(void) {
    const uint64_t durations[]={30000,60000,180000,1800000};duration=durations[LO_CASE];
    LoProcessRequest request={0};request.timeout_ms=duration;request.cleanup_ms=1000;request.output_bytes=1024;request.stop=stop;
    LoProcessResult result={0};
    lo_native_process_execute("./fixture-launcher",0,".",0,0,&request,&result);
    if(below!=1 || signals!=1 || reaped!=1 || result.stage!=7 || result.class_id!=LO_PROCESS_TIMEOUT
        || !result.started || !result.cleanup_complete || result.ownership || result.termination_kind!=2 || result.termination_value!=9)return 10;
    return 0;
}
