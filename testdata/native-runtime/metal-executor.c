#include "../../runtime/native/include/lo_device_driver.h"
#include "metal-executor.h"
extern void *malloc(size_t);
extern void *calloc(size_t,size_t);
extern void free(void *);
extern void *memcpy(void *,const void *,size_t);
extern int strcmp(const char *,const char *);
extern int open(const char *,int,...),close(int),unlink(const char *);
extern int pipe(int *),fcntl(int,int,...),dup2(int,int),kill(int,int),getpid(void);
extern long read(int,void *,size_t),write(int,const void *,size_t);
extern int lo_cli_main(int,const char *const *,const char *const *);
extern uint64_t lo_runtime_live_contexts(void);
extern uint64_t lo_runtime_live_values(void);
extern uint64_t lo_runtime_live_storages(void);
typedef struct { uint64_t logical; float *values; } ModelBuffer;
static unsigned opens,closes,allocations,releases,computes,transfers;
static int model_has(const char *text,const char *part){for(;*text;++text){const char *a=text,*b=part;while(*a&&*a==*b){++a;++b;}if(!*b)return 1;}return 0;}
static int model_error(LoDeviceError *error,unsigned kind,const char *message){error->kind=kind;char *p=error->message;while((*p++=*message++));return 0;}
static uint64_t peak;
static unsigned uploads,downloads,initial_computes,bad_training;
static int model_open(uintptr_t user,const char *selector,uintptr_t *session,LoDeviceIdentity *identity,LoDeviceError *error) {
    (void)user;(void)error;
    if(strcmp(selector,"metal:0")) return 0;
    ++opens;*session=1;
    memcpy(identity->name,"independent native descriptor model",36);
    if(LO_CASE==5)memcpy(identity->profile,"wrong-profile",14);
    else memcpy(identity->profile,"metal-fp32",11);
    return 1;
}
static int model_close(uintptr_t user,uintptr_t session,LoDeviceError *error) {
    (void)user;(void)session;(void)error;++closes;return 1;
}
static int model_allocate(uintptr_t user,uintptr_t session,uint64_t bytes,uintptr_t *output,uint64_t *reserved,LoDeviceError *error) {
    (void)user;(void)session;(void)error;
    ModelBuffer *buffer=malloc(sizeof(*buffer));
    if(!buffer)return 0;
    buffer->logical=bytes;
    buffer->values=calloc(bytes?bytes:1,1);
    if(!buffer->values){free(buffer);return 0;}
    *output=(uintptr_t)buffer;*reserved=2*bytes;++allocations;return 1;
}
static int model_upload(uintptr_t user,uintptr_t handle,const float *values,uint64_t bytes,LoDeviceError *error) {
    (void)user;(void)error;ModelBuffer *buffer=(ModelBuffer *)handle;
    if(bytes!=buffer->logical)return 0;
    if(bytes)memcpy(buffer->values,values,(size_t)bytes);return 1;
}
static int model_download(uintptr_t user,uintptr_t handle,float *values,uint64_t bytes,LoDeviceError *error) {
    (void)user;(void)error;ModelBuffer *buffer=(ModelBuffer *)handle;
    if(bytes!=buffer->logical)return 0;
    if(bytes)memcpy(values,buffer->values,(size_t)bytes);return 1;
}
static int model_release(uintptr_t user,uintptr_t handle,LoDeviceError *error) {
    (void)user;(void)error;ModelBuffer *buffer=(ModelBuffer *)handle;
    free(buffer->values);free(buffer);++releases;return 1;
}
static int model_supports(uintptr_t user,uintptr_t session,const LoDeviceOperation *operation,LoDeviceError *error) {
    (void)user;(void)session;
    if(LO_CASE==4)return model_error(error,LO_DEVICE_UNSUPPORTED_OPERATION,"controlled compute admission failure");
    return operation->kind>=LO_DEVICE_ADD && operation->kind<=LO_DEVICE_SQRT_GRAD;
}
static int model_compute(uintptr_t user,uintptr_t session,const LoDeviceOperation *operation,const uintptr_t *inputs,size_t count,uintptr_t output,uintptr_t status,uint32_t *numeric,LoDeviceError *error) {
    (void)user;(void)session;(void)status;(void)error;
    LoMetalOperation descriptor;memcpy(&descriptor,operation,sizeof(descriptor));
    const float *a=count?((ModelBuffer *)inputs[0])->values:0;
    const float *b=count>1?((ModelBuffer *)inputs[1])->values:0;
    ++computes;
    if(LO_CASE==9 || LO_CASE==10) {
        if(kill(getpid(),2))return model_error(error,LO_DEVICE_EXECUTION,"cancel injection failed");
        if(LO_CASE==10)return model_error(error,LO_DEVICE_LOST,"lost during cancelled computation");
    }
    if(LO_CASE==6)return model_error(error,LO_DEVICE_EXECUTION,"controlled compute command failure");
    if(LO_CASE==7)return model_error(error,LO_DEVICE_LOST,"lost during computation");
    if(LO_CASE==8 && operation->kind==LO_DEVICE_SQRT_GRAD)return model_error(error,LO_DEVICE_EXECUTION,"reverse command failed");
    return model_execute(&descriptor,a,b,((ModelBuffer *)output)->values,numeric);
}
static void model_observer(uintptr_t user,const LoDeviceReceipt *receipt) {
    (void)user;
    if(receipt->event==LO_DEVICE_TRANSFER_EVENT) {
        ++transfers;
        if(!strcmp(receipt->device,"metal:0"))++uploads;
        else if(!strcmp(receipt->device,"cpu")) {++downloads;if(downloads==1)initial_computes=computes;}
    }
    if(LO_CASE==2 && receipt->event==LO_DEVICE_COMPUTE && (uploads!=6 || downloads>1))bad_training=1;
    if(receipt->live_bytes>peak)peak=receipt->live_bytes;
}
int main(void) {
    LoDeviceDriver driver={0,model_open,model_close,model_allocate,model_upload,model_download,model_release,model_supports,model_compute};
    if(!lo_device_install_driver(&driver))return 10;
    lo_device_set_observer(0,model_observer);
    const char *arguments[]={"native-metal-model","run","--allow-device","metal:0","--max-steps","100000000","--format","json","--write-root","."};
    for(unsigned iteration=0;iteration<(LO_CASE>=9?1:3);++iteration) {
        peak=0;uploads=0;downloads=0;initial_computes=0;bad_training=0;
        unsigned before_computes=computes,before_allocations=allocations;
        int result;
        if(LO_CASE>=9) {
            int output[2];if(pipe(output))return 40;
            int saved_out=fcntl(1,0,3),saved_err=fcntl(2,0,3);
            if(saved_out<0 || saved_err<0 || dup2(output[1],1)<0 || dup2(output[1],2)<0)return 41;
            close(output[1]);
            result=lo_cli_main((int)(sizeof(arguments)/sizeof(arguments[0])),arguments,0);
            if(dup2(saved_out,1)<0 || dup2(saved_err,2)<0)return 42;
            close(saved_out);close(saved_err);
            char contents[16384];long length=read(output[0],contents,sizeof(contents)-1);close(output[0]);
            if(length<0)return 43;contents[length]=0;
            if(result!=2 || !model_has(contents,"runtime.cancelled") || model_has(contents,"Device.Lost") || model_has(contents,"\"status\":\"ok\"")) {write(2,contents,(size_t)length);return 44;}
            result=0;
        } else result=lo_cli_main((int)(sizeof(arguments)/sizeof(arguments[0])),arguments,0);
        if(result)return 20+result;
        if(opens!=iteration+1 || closes!=iteration+1 || allocations!=releases || !allocations)return 30;
        if(LO_CASE!=1 && LO_CASE!=4 && LO_CASE!=5 && computes==before_computes)return 33;
        if((LO_CASE==4 || LO_CASE==5) && (computes!=before_computes || allocations!=before_allocations+1))return 38;
        if(LO_CASE==7 && (computes!=before_computes+1 || uploads!=1 || downloads!=0))return 39;
        if(LO_CASE>=9 && (computes!=before_computes+1 || allocations!=before_allocations+3 || uploads!=1 || downloads!=0))return 45;
        if(LO_CASE==1 && computes!=before_computes)return 35;
        if(LO_CASE==2 && (uploads!=6 || downloads!=6 || peak>2048 || bad_training || computes-initial_computes<200))return 34;
        if(LO_CASE==3 && (uploads!=6 || downloads!=7 || peak>4096 || computes-initial_computes<500))return 36;
        if(!transfers || !peak)return 31;
        int later=open("later",0);if(later>=0){close(later);unlink("later");return 37;}
        if(lo_runtime_live_contexts() || lo_runtime_live_values() || lo_runtime_live_storages())return 32;
    }
    return 0;
}
