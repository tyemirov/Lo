#include "../../runtime/native/include/lo_device_driver.h"
typedef __SIZE_TYPE__ Size;
extern void *malloc(Size);
extern void free(void *);
extern void *memcpy(void *,const void *,Size);
extern int pipe(int *),fcntl(int,int,...),dup2(int,int),close(int),kill(int,int),getpid(void),open(const char *,int,...),unlink(const char *);
extern long read(int,void *,Size),write(int,const void *,Size);
extern int lo_cli_main(int,const char *const *,const char *const *);
extern uint64_t lo_runtime_live_storages(void),lo_runtime_live_storage_bytes(void);
struct Buffer {uint64_t bytes;unsigned char *values;};
static unsigned opens,closes,allocations,releases,uploads,downloads,live_buffers,events,alloc_events,release_events,transfer_events,open_events,close_events;
static uint64_t observed_live,peak,registered[32],registered_bytes[32];
static int invalid;
static void text(char *target,const char *source){while((*target++=*source++));}
static int has(const char *text,const char *part){for(;*text;text++){const char *a=text,*b=part;while(*a&&*a==*b){a++;b++;}if(!*b)return 1;}return 0;}
static int fail(LoDeviceError *error,unsigned kind){error->kind=kind;text(error->message,"controlled device boundary failure");return 0;}
static int host_open(uintptr_t user,const char *selector,uintptr_t *session,LoDeviceIdentity *identity,LoDeviceError *error){(void)user;(void)selector;opens++;if(LO_CASE==1)return fail(error,LO_DEVICE_UNAVAILABLE);*session=1;text(identity->name,"controlled transfer backend");text(identity->profile,"cuda-sim-fp32");return 1;}
static int host_close(uintptr_t user,uintptr_t session,LoDeviceError *error){(void)user;(void)session;closes++;if(LO_CASE==6)return fail(error,LO_DEVICE_EXECUTION);return 1;}
static int host_allocate(uintptr_t user,uintptr_t session,uint64_t bytes,uintptr_t *out,uint64_t *reserved,LoDeviceError *error){(void)user;(void)session;if(LO_CASE==2)return fail(error,LO_DEVICE_ALLOCATION);struct Buffer *buffer=malloc(sizeof(*buffer));if(!buffer)return fail(error,LO_DEVICE_ALLOCATION);buffer->bytes=bytes;buffer->values=malloc(bytes?bytes:1);if(!buffer->values){free(buffer);return fail(error,LO_DEVICE_ALLOCATION);}*out=(uintptr_t)buffer;*reserved=LO_CASE==8?67108864:LO_CASE==9?bytes-4:bytes*2;allocations++;live_buffers++;return 1;}
static int host_upload(uintptr_t user,uintptr_t handle,const float *values,uint64_t bytes,LoDeviceError *error){(void)user;uploads++;if(LO_CASE==7||LO_CASE==11){if(kill(getpid(),2))return fail(error,LO_DEVICE_EXECUTION);}if(LO_CASE==3)return fail(error,LO_DEVICE_TRANSFER);if(LO_CASE==10||LO_CASE==11)return fail(error,LO_DEVICE_LOST);struct Buffer *buffer=(void *)handle;if(buffer->bytes!=bytes)return fail(error,LO_DEVICE_TRANSFER);memcpy(buffer->values,values,(Size)bytes);return 1;}
static int host_download(uintptr_t user,uintptr_t handle,float *values,uint64_t bytes,LoDeviceError *error){(void)user;downloads++;if(LO_CASE==4)return fail(error,LO_DEVICE_TRANSFER);struct Buffer *buffer=(void *)handle;if(buffer->bytes!=bytes)return fail(error,LO_DEVICE_TRANSFER);memcpy(values,buffer->values,(Size)bytes);return 1;}
static int host_release(uintptr_t user,uintptr_t handle,LoDeviceError *error){(void)user;struct Buffer *buffer=(void *)handle;free(buffer->values);free(buffer);releases++;live_buffers--;if(LO_CASE==5)return fail(error,LO_DEVICE_EXECUTION);return 1;}
static int host_supports(uintptr_t user,uintptr_t session,const LoDeviceOperation *operation,LoDeviceError *error){(void)user;(void)session;(void)operation;return fail(error,LO_DEVICE_UNSUPPORTED_OPERATION);}
static int host_compute(uintptr_t user,uintptr_t session,const LoDeviceOperation *operation,const uintptr_t *inputs,size_t count,uintptr_t output,uintptr_t status,uint32_t *numeric,LoDeviceError *error){(void)user;(void)session;(void)operation;(void)inputs;(void)count;(void)output;(void)status;(void)numeric;return fail(error,LO_DEVICE_UNSUPPORTED_OPERATION);}
static void observe(uintptr_t user,const LoDeviceReceipt *receipt){(void)user;events++;
 if(receipt->event==LO_DEVICE_ALLOCATE){if(receipt->allocation>=32||registered[receipt->allocation]){invalid=1;return;}registered[receipt->allocation]=1;registered_bytes[receipt->allocation]=receipt->bytes;observed_live+=receipt->bytes;alloc_events++;}
 else if(receipt->event==LO_DEVICE_RELEASE){if(receipt->allocation>=32||!registered[receipt->allocation]||registered_bytes[receipt->allocation]!=receipt->bytes){invalid=2;return;}registered[receipt->allocation]=0;observed_live-=receipt->bytes;release_events++;}
 else if(receipt->event==LO_DEVICE_TRANSFER_EVENT)transfer_events++;
 else if(receipt->event==LO_DEVICE_OPEN){open_events++;if(!receipt->backend||!has(receipt->backend->profile,"cuda-sim-fp32"))invalid=3;}
 else if(receipt->event==LO_DEVICE_CLOSE)close_events++;
 else invalid=4;
 if(observed_live!=receipt->live_bytes)invalid=5;if(peak<observed_live)peak=observed_live;
}
int main(void){LoDeviceDriver driver={0,host_open,host_close,host_allocate,host_upload,host_download,host_release,host_supports,host_compute};if(!lo_device_install_driver(&driver))return 1;lo_device_set_observer(0,observe);
 int output[2];if(pipe(output))return 2;int saved_out=fcntl(1,0,3),saved_err=fcntl(2,0,3);if(saved_out<0||saved_err<0||dup2(output[1],1)<0||dup2(output[1],2)<0)return 3;close(output[1]);
 const char *args[]={"device-resources","run","--format","json","--allow-device","cuda-sim:0","--write-root","."};int status=lo_cli_main(8,args,0);
 if(dup2(saved_out,1)<0||dup2(saved_err,2)<0)return 4;close(saved_out);close(saved_err);char contents[16384];long length=read(output[0],contents,sizeof(contents)-1);close(output[0]);if(length<0)return 5;contents[length]=0;
 int expected=LO_CASE==0||LO_CASE==10?0:LO_CASE==5||LO_CASE==6||LO_CASE==7||LO_CASE==8||LO_CASE==11?2:1;
 const char *kind=LO_CASE==1?"Device.Unavailable":LO_CASE==2||LO_CASE==9?"Device.AllocationFailed":LO_CASE==3||LO_CASE==4?"Device.TransferFailed":LO_CASE==5||LO_CASE==6?"runtime.host-failure":LO_CASE==7||LO_CASE==11?"runtime.cancelled":LO_CASE==8?"runtime.limit":LO_CASE==10?"Device.Lost":"\"status\":\"ok\"";
 if(status!=expected||!has(contents,kind)){write(2,contents,(Size)length);return 20+LO_CASE;}
 if(invalid||observed_live||alloc_events!=release_events||close_events!=1||live_buffers||allocations!=releases||lo_runtime_live_storages()||lo_runtime_live_storage_bytes())return 40+LO_CASE;
 if(LO_CASE==0&&(peak!=48||alloc_events!=3||transfer_events!=2||uploads!=1||downloads!=1||opens!=1||closes!=1))return 60;
 if(LO_CASE==10&&(opens!=1||allocations!=1||uploads!=1||downloads!=0))return 61;
 int later=open("later",0);int expected_later=LO_CASE==0||LO_CASE==5||LO_CASE==6;if((later>=0)!=expected_later)return 70+LO_CASE;if(later>=0){close(later);if(unlink("later"))return 82;}return 0;
}
