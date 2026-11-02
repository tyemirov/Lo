#define main unused_console_device_main
#define model_close base_model_close
#include "metal-executor.c"
#undef main
#undef model_close
#include "lo_context_options.h"
#include "lo_console.h"
typedef struct {uint32_t status,code;const Value *value;} ConsoleResult;
extern Context *lo_context_new(const ContextOptions *);
extern void lo_context_run(Context *,ConsoleResult *);
extern void lo_context_free(Context *);
extern void lo_value_release(const Value *);
static unsigned output_calls;
static int failing_close(uintptr_t user,uintptr_t session,LoDeviceError *error) {
 base_model_close(user,session,error);
 return model_error(error,LO_DEVICE_EXECUTION,"controlled completion cleanup failure");
}
static int output(void *user,uint32_t destination,const uint8_t *bytes,size_t length,size_t *confirmed){(void)user;(void)destination;(void)bytes;++output_calls;*confirmed=length;return 0;}
static int flush(void *user,uint32_t destination){(void)user;(void)destination;++output_calls;return 0;}
int main(void) {
 LoDeviceDriver driver={0,model_open,failing_close,model_allocate,model_upload,model_download,model_release,model_supports,model_compute};
 if(!lo_device_install_driver(&driver))return 1;
 const ContextOptions options={100000,16ULL<<20,64ULL<<20,0,0,0,128};
 Context *context=lo_context_new(&options);
 if(!context || !lo_context_set_console(context,3,0,output,flush,1048576) || !lo_context_set_device_grant(context,"metal:0"))return 2;
 ConsoleResult result={99,99,0};lo_context_run(context,&result);
 if(result.status || !result.value || opens!=1 || closes || output_calls)return 3;
 int status=99;
 if(lo_context_complete(context,result.value,&status) || status!=99 || output_calls || closes!=1)return 4;
 lo_value_release(result.value);lo_context_free(context);
 if(allocations!=releases || lo_runtime_live_contexts() || lo_runtime_live_values() || lo_runtime_live_storages())return 5;
 return 0;
}
