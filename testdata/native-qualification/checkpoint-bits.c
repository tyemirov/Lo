#define main unused_native_metal_main
#include "../native-runtime/metal-executor.c"
#undef main
int main(void) {
 LoDeviceDriver driver={0,model_open,model_close,model_allocate,model_upload,model_download,model_release,model_supports,model_compute};
 if(!lo_device_install_driver(&driver))return 10;
 lo_device_set_observer(0,model_observer);
 const char *args[]={"checkpoint-bits","run","--format","json","--allow-device","metal:0"};
 for(unsigned iteration=0;iteration<3;++iteration) {
  if(lo_cli_main(6,args,0))return 11;
  if(opens!=iteration+1||closes!=iteration+1||allocations!=releases||computes)return 12;
  if(transfers!=2*(iteration+1))return 13;
  if(LO_CASE==1 ? peak!=0 : peak==0)return 14;
  if(lo_runtime_live_contexts()||lo_runtime_live_values()||lo_runtime_live_storages())return 15;
 }
 return 0;
}
