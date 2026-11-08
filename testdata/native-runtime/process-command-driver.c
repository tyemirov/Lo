#include "lo_process.h"
#include "lo_console.h"
extern uint64_t lo_runtime_live_contexts(void),lo_runtime_live_values(void);
extern uintptr_t pthread_self(void);
extern int pthread_create(uintptr_t *,const void *,void *(*)(void *),void *),pthread_join(uintptr_t,void **);
struct timespec {long sec,nsec;};
extern int nanosleep(const struct timespec *,struct timespec *);
static Context *active;
static uintptr_t owner;
static unsigned driver_calls,sink_calls,sink_bytes,invalid;
static unsigned producer_ready;
static int contains(const char *text,uintptr_t length,const char *needle) {
    for(uintptr_t start=0;start<length;++start) {
        uintptr_t index=0;while(needle[index] && start+index<length && text[start+index]==needle[index])++index;
        if(!needle[index])return 1;
    }
    return 0;
}
static int sink(void *user,uint32_t stream,const unsigned char *bytes,uintptr_t length,uintptr_t *confirmed) {
    if(user!=(void *)35 || pthread_self()!=owner || !bytes || !length || stream<1 || stream>2)invalid=1;
    ++sink_calls;*confirmed=0;
    if(LO_CASE==7){lo_context_cancel(active);return LO_CONSOLE_WOULD_BLOCK;}
    if(LO_CASE==11)return LO_CONSOLE_WOULD_BLOCK;
    if(LO_CASE==10 && sink_calls<3)return LO_CONSOLE_WOULD_BLOCK;
    if(LO_CASE==9){*confirmed=2;sink_bytes+=2;return LO_CONSOLE_FAILED;}
    if(LO_CASE==16){*confirmed=1;++sink_bytes;lo_context_cancel(active);return LO_CONSOLE_OK;}
    *confirmed=length;sink_bytes+=(unsigned)length;return LO_CONSOLE_OK;
}
static int flush(void *user,uint32_t stream){(void)user;(void)stream;invalid=2;return LO_CONSOLE_OK;}
struct output_call {const LoProcessRequest *request;uint32_t stream;uintptr_t confirmed;int outcome;};
static void *produce(void *token) {
    struct output_call *call=token;const unsigned char bytes[]={'o','u','t'};
    __atomic_fetch_add(&producer_ready,1,__ATOMIC_SEQ_CST);
    const struct timespec pause={0,1000000};
    while(__atomic_load_n(&producer_ready,__ATOMIC_SEQ_CST)<2)nanosleep(&pause,0);
    call->outcome=call->request->output(call->request->output_token,call->stream,bytes,sizeof(bytes),&call->confirmed);
    return 0;
}
static void execute(uintptr_t user,const LoProcessRequest *request,LoProcessResult *result) {
    if(user!=35 || !request || pthread_self()==owner || request->cleanup_ms!=(LO_CASE==24||LO_CASE==26?20U:1000U))invalid=3;
    ++driver_calls;
    const uint64_t durations[]={30000,60000,180000,1800000};
    if(LO_CASE<4 && request->timeout_ms!=durations[LO_CASE])invalid=4;
    if(LO_CASE==19 && request->timeout_ms!=30000)invalid=5;
    result->started=1;result->termination_kind=1;result->termination_value=0;result->cleanup_complete=1;
    if((LO_CASE==5 || LO_CASE==6) && driver_calls==1){result->stage=7;result->class_id=LO_PROCESS_TIMEOUT;if(LO_CASE==6){result->cleanup_complete=0;result->ownership=35;}return;}
    if(driver_calls>1)return;
    if(LO_CASE==25 || LO_CASE==27) {
        struct output_call calls[2]={{request,1,0,0},{request,LO_CASE==27?1:2,0,0}};uintptr_t threads[2];unsigned created=0;
        for(;created<2;++created)if(pthread_create(&threads[created],0,produce,&calls[created])){invalid=8;__atomic_store_n(&producer_ready,2,__ATOMIC_SEQ_CST);break;}
        for(unsigned index=0;index<created;++index)if(pthread_join(threads[index],0))invalid=9;
        if(LO_CASE==25){if(calls[0].outcome||calls[1].outcome)invalid=9;result->stdout_size=calls[0].confirmed;result->stderr_size=calls[1].confirmed;}
        else {if(calls[0].outcome+calls[1].outcome!=LO_PROCESS_LIMIT)invalid=9;result->stdout_size=calls[0].confirmed+calls[1].confirmed;}
        return;
    }
    if(LO_CASE>=20) {
        const unsigned char output[]={'o','u','t'};uintptr_t confirmed=0;
        int outcome=request->output(request->output_token,LO_CASE==22?3:1,output,LO_CASE==20||LO_CASE==23||LO_CASE==26?4097:sizeof(output),&confirmed);
        result->stdout_size=confirmed;
        if(LO_CASE!=24){
            if(outcome!=(LO_CASE==21?LO_PROCESS_LIMIT:LO_PROCESS_HOST))invalid=6;
            if(LO_CASE==23)result->stage=1;
            if(LO_CASE==26){const struct timespec cleanup={0,100000000};nanosleep(&cleanup,0);}
            return;
        }
        if(outcome||confirmed!=3){invalid=7;return;}
        const struct timespec pause={0,1000000},cleanup={0,100000000};
        while(!request->stop(request->stop_token))nanosleep(&pause,0);
        nanosleep(&cleanup,0);return;
    }
    if(LO_CASE>=7 && LO_CASE<=17 && LO_CASE!=13) {
        const unsigned char output[]={0,255,'\n'},error[]={'e','\r','\n'};
        uintptr_t confirmed=0;
        int outcome=request->output(request->output_token,1,output,LO_CASE==17?4097:sizeof(output),&confirmed);
        result->stdout_size=confirmed;
        if(outcome){result->stage=3;result->class_id=outcome;return;}
        outcome=request->output(request->output_token,2,error,sizeof(error),&confirmed);
        result->stderr_size=confirmed;
        if(outcome){result->stage=4;result->class_id=outcome;}
    }
}
int main(void) {
    const ContextOptions options={100000,16ULL<<20,64ULL<<20,0,0,0,128};
    active=lo_context_new(&options);owner=pthread_self();if(!active)return 10;
    LoProcessLimits limits={1800000,LO_CASE==24||LO_CASE==26?20:1000,LO_CASE==12||LO_CASE==21?2:LO_CASE==27?5:1048576,3};
    if(!lo_context_process_driver(active,execute,35) || !lo_context_process_driver_limits(active,&limits))return 11;
    if(!lo_context_set_console(active,LO_CASE==13?1:3,(void *)35,sink,flush,LO_CASE==15?2:1048576))return 12;
    if(LO_CASE==28 || LO_CASE==29) {
        Context *peer=lo_context_process_peer(active);if(!peer)return 40;
        ResultSlot evaluated={99,99,0};lo_context_run(peer,&evaluated);
        if(evaluated.status||!evaluated.value)return 41;
        lo_value_release(evaluated.value);lo_context_free(peer);
        int installed=LO_CASE==28?lo_context_process_driver(active,execute,35):lo_context_process_driver_limits(active,&limits);
        if(installed)return 42;
        lo_context_free(active);return lo_runtime_live_contexts()||lo_runtime_live_values()?43:0;
    }
    ResultSlot result={99,99,0};lo_context_run(active,&result);
    int terminal=LO_CASE==4 || LO_CASE==6 || LO_CASE==7 || LO_CASE==9 || LO_CASE==12 || LO_CASE==13 || LO_CASE==15 || LO_CASE==16 || LO_CASE==17 || (LO_CASE>=20 && LO_CASE!=25);
    if(result.status!=(terminal?2U:0U) || (result.value!=0)==terminal)return 13;
    if(terminal) {
        char diagnostic[16384];uintptr_t length=0;
        if(lo_context_diagnostic(active,&result,(unsigned char *)diagnostic,sizeof(diagnostic),&length)!=LO_DIAGNOSTIC_OK)return 14;
        const char *code=LO_CASE==4 || LO_CASE==13?"runtime.capability-denied":LO_CASE==7 || LO_CASE==16?"runtime.cancelled":LO_CASE==12 || LO_CASE==15 || LO_CASE==21 || LO_CASE==27?"runtime.limit":"runtime.host-failure";
        if(!contains(diagnostic,length,code))return 15;
        if(LO_CASE==26 && !contains(diagnostic,length,"\"cleanup_complete\":{\"type\":\"Bool\",\"value\":false}"))return 17;
        if((LO_CASE==9 && !contains(diagnostic,length,"\"stdout_bytes\":{\"type\":\"Int\",\"value\":\"2\"}"))
            || (LO_CASE==16 && !contains(diagnostic,length,"\"stdout_bytes\":{\"type\":\"Int\",\"value\":\"1\"}"))
            || ((LO_CASE==24 || LO_CASE==27) && !contains(diagnostic,length,"\"stdout_bytes\":{\"type\":\"Int\",\"value\":\"3\"}")))return 16;
    }
    unsigned expected_calls=LO_CASE==4 || LO_CASE==13?0:LO_CASE==5 || LO_CASE==11?2:1;
    if(driver_calls!=expected_calls || invalid)return 20+invalid;
    if((LO_CASE==8 || LO_CASE==10 || LO_CASE==25) && sink_bytes!=6)return 30;
    if(LO_CASE==10 && sink_calls!=4)return 31;
    if(result.value)lo_value_release(result.value);
    if(LO_CASE==24||LO_CASE==26)lo_context_process_wait_retained(active);
    lo_context_free(active);
    return lo_runtime_live_contexts() || lo_runtime_live_values()?32:0;
}
