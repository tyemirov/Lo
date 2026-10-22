#define main unused_native_metal_main
#include "../native-runtime/metal-executor.c"
#undef main
int main(void){
 LoDeviceDriver driver={0,model_open,model_close,model_allocate,model_upload,model_download,model_release,model_supports,model_compute};
 if(!lo_device_install_driver(&driver))return 10;
 lo_device_set_observer(0,model_observer);
 const char*args[]={"semantic-metal","run","--format","json","--allow-device","metal:0","--max-steps","100000000"};
 int status=lo_cli_main(8,args,0);
 unsigned expected=LO_CASE==0?6:LO_CASE==1?5:13;
 if(status || transfers!=expected || opens!=1 || closes!=1 || !allocations || allocations!=releases)return 20;
 if(lo_runtime_live_contexts()||lo_runtime_live_values()||lo_runtime_live_storages())return 21;
 return 0;
}
