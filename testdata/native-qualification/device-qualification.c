#define main unused_device_model_main
#define model_upload base_model_upload
#define model_download base_model_download
#define model_allocate base_model_allocate
#define model_supports base_model_supports
#include "../native-runtime/metal-executor.c"
#undef main
#undef model_upload
#undef model_download
#undef model_allocate
#undef model_supports
static unsigned upload_calls,download_calls,ledger_allocations,ledger_releases;
static uint64_t ledger_ids[1024],ledger_bytes[1024],ledger_live;
static int ledger_error;
static int qualification_open(uintptr_t user,const char *selector,uintptr_t *session,LoDeviceIdentity *identity,LoDeviceError *error){
 if(LO_CASE==110||LO_CASE==111){
  if(strcmp(selector,"cuda-sim:0"))return 0;
  int result=model_open(user,"metal:0",session,identity,error);
  if(result){for(unsigned i=0;i<64;++i)identity->profile[i]=0;memcpy(identity->profile,"cuda-sim-fp32",13);}
  return result;
 }
 return model_open(user,selector,session,identity,error);
}
static int qualification_allocate(uintptr_t user,uintptr_t session,uint64_t bytes,uintptr_t *output,uint64_t *reserved,LoDeviceError *error){
 int result=base_model_allocate(user,session,bytes,output,reserved,error);
 if(result && LO_CASE==103)*reserved=67108864;
 if(result && (LO_CASE==110||LO_CASE==111))*reserved=bytes;
 return result;
}
static int qualification_upload(uintptr_t user,uintptr_t handle,const float *values,uint64_t bytes,LoDeviceError *error){
 ++upload_calls;
 if(upload_calls==3 && LO_CASE==100)return model_error(error,LO_DEVICE_TRANSFER,"third state upload failed");
 int result=base_model_upload(user,handle,values,bytes,error);
 if(upload_calls==3 && LO_CASE==102 && kill(getpid(),2))return model_error(error,LO_DEVICE_EXECUTION,"cancel injection failed");
 return result;
}
static int qualification_download(uintptr_t user,uintptr_t handle,float *values,uint64_t bytes,LoDeviceError *error){
 ++download_calls;
 if(LO_CASE==101)return model_error(error,LO_DEVICE_TRANSFER,"state download failed");
 return base_model_download(user,handle,values,bytes,error);
}
static int qualification_supports(uintptr_t user,uintptr_t session,const LoDeviceOperation *operation,LoDeviceError *error){
 if(LO_CASE==122)return model_error(error,LO_DEVICE_UNSUPPORTED_OPERATION,"storage-only backend");
 return base_model_supports(user,session,operation,error);
}
static void qualification_observer(uintptr_t user,const LoDeviceReceipt*receipt){
 model_observer(user,receipt);
 if(receipt->event==LO_DEVICE_ALLOCATE){
  unsigned slot;for(slot=0;slot<1024 && ledger_ids[slot];++slot);
  if(slot==1024){ledger_error=1;return;}
  for(unsigned i=0;i<1024;++i)if(ledger_ids[i]==receipt->allocation)ledger_error=2;
  ledger_ids[slot]=receipt->allocation;ledger_bytes[slot]=receipt->bytes;ledger_live+=receipt->bytes;++ledger_allocations;
 }else if(receipt->event==LO_DEVICE_RELEASE){
  unsigned slot;for(slot=0;slot<1024 && ledger_ids[slot]!=receipt->allocation;++slot);
  if(slot==1024 || ledger_bytes[slot]!=receipt->bytes){ledger_error=3;return;}
  ledger_ids[slot]=0;ledger_live-=receipt->bytes;++ledger_releases;
 }
 if(ledger_live!=receipt->live_bytes)ledger_error=4;
}
int main(void){
 LoDeviceDriver driver={0,qualification_open,model_close,qualification_allocate,qualification_upload,qualification_download,model_release,qualification_supports,model_compute};
 if(!lo_device_install_driver(&driver))return 10;
 lo_device_set_observer(0,qualification_observer);
 int failing=(LO_CASE>=100 && LO_CASE<=103)||(LO_CASE>=120 && LO_CASE<=124);
 if(failing){int capture_error=lo_capture_start();if(capture_error)return capture_error;}
 const char *args[]={"device-qualification","run","--format","json","--allow-device",LO_CASE==110||LO_CASE==111?"cuda-sim:0":"metal:0","--write-root",".","--max-steps","100000000"};
 int status=lo_cli_main(10,args,0);
 if(failing){
  int capture_error=lo_capture_finish();if(capture_error)return capture_error;char *contents=lo_capture_bytes;long length=(long)lo_capture_length;
  int expected=LO_CASE==102||LO_CASE==103||LO_CASE==121?2:1;
  const char *kind=LO_CASE==102?"runtime.cancelled":LO_CASE==103?"runtime.limit":LO_CASE==121?"runtime.tensor-publication":LO_CASE==122?"Device.UnsupportedOperation":LO_CASE==123?"Tensor.DeviceMismatch":LO_CASE>=120?"Tensor.HostObservationRequired":"Device.TransferFailed";
  if(status!=expected || !model_has(contents,kind) || model_has(contents,"\"status\":\"ok\"")){write(2,contents,(size_t)length);return 20;}
  if(LO_CASE>=120 && download_calls)return 21;
  if((LO_CASE==100||LO_CASE==102) && (upload_calls!=3||download_calls))return 22;
  if(LO_CASE==101 && (upload_calls!=5||download_calls!=1))return 23;
 }else if(status)return 24;
 if(ledger_error||ledger_live||ledger_allocations!=ledger_releases||allocations!=releases||opens!=1||closes!=1)return 30;
 if(LO_CASE==110 && (transfers!=200||upload_calls!=100||download_calls!=100||peak>64))return 31;
 if(LO_CASE==111 && (transfers!=61||upload_calls!=21||download_calls!=40||ledger_allocations<61||peak>512))return 32;
 if(LO_CASE==140 && (download_calls!=1000||upload_calls!=6||computes<200))return 33;
 int later=open("never",0);if(later>=0){close(later);return 34;}
 if(lo_runtime_live_contexts()||lo_runtime_live_values()||lo_runtime_live_storages())return 35;
 return 0;
}
