#ifndef LO_PROCESS_OWNERSHIP_FIXTURE_H
#define LO_PROCESS_OWNERSHIP_FIXTURE_H
#include <bsm/libbsm.h>
#include <errno.h>
#include <fcntl.h>
#include <libproc.h>
#include <mach/mach.h>
#include <poll.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <time.h>
#include <unistd.h>

enum { FIXTURE_LAUNCHER=1, FIXTURE_ENTER=2, FIXTURE_FORK=3,
       FIXTURE_SPAWN=4, FIXTURE_EXEC=5 };
enum { FIXTURE_MAGIC=0x4c4f4636, FIXTURE_PATH=1024, FIXTURE_PROCESSES=128 };
struct fixture_packet {
    uint32_t magic, kind;
    audit_token_t actor, target;
    uint64_t device, inode;
    char path[FIXTURE_PATH];
};
static uint64_t fixture_now(void) {
    struct timespec time;
    if(clock_gettime(CLOCK_MONOTONIC,&time))_exit(90);
    return (uint64_t)time.tv_sec*1000+(uint64_t)time.tv_nsec/1000000;
}
static int fixture_token(audit_token_t *token) {
    mach_msg_type_number_t count=TASK_AUDIT_TOKEN_COUNT;
    return task_info(mach_task_self(),TASK_AUDIT_TOKEN,(task_info_t)token,&count)!=KERN_SUCCESS
        || count!=TASK_AUDIT_TOKEN_COUNT;
}
static void fixture_token_text(const audit_token_t *token,char text[65]) {
    for(unsigned index=0;index<8;++index)snprintf(text+index*8,9,"%08x",token->val[index]);
}
static int fixture_parse_token(const char *text,audit_token_t *token) {
    if(strlen(text)!=64)return 1;
    for(unsigned index=0;index<8;++index) {
        char word[9];memcpy(word,text+index*8,8);word[8]=0;
        char *end=NULL;unsigned long value=strtoul(word,&end,16);
        if(!end||*end||value>UINT32_MAX)return 1;
        token->val[index]=(uint32_t)value;
    }
    return 0;
}
static int fixture_io(int descriptor,void *buffer,size_t size,int writing) {
    unsigned char *bytes=buffer;uint64_t deadline=fixture_now()+4000;
    while(size) {
        if(fixture_now()>=deadline)return 1;
        struct pollfd event={descriptor,writing?POLLOUT:POLLIN,0};
        int ready=poll(&event,1,20);
        if(ready<0&&errno==EINTR)continue;
        if(ready<0)return 1;
        if(!ready)continue;
        ssize_t count=writing?write(descriptor,bytes,size):read(descriptor,bytes,size);
        if(count<0&&(errno==EINTR||errno==EAGAIN))continue;
        if(count<=0)return 1;
        bytes+=count;size-=(size_t)count;
    }
    return 0;
}
static int fixture_report(const char *socket_path,uint32_t kind,const audit_token_t *actor,
                          const char *image) {
    if(strlen(socket_path)>=sizeof(((struct sockaddr_un *)0)->sun_path))return 1;
    struct fixture_packet packet={0};packet.magic=FIXTURE_MAGIC;packet.kind=kind;
    if(actor)packet.actor=*actor;
    if(fixture_token(&packet.target))return 1;
    if(image) {
        if(strlen(image)>=sizeof(packet.path))return 1;
        memcpy(packet.path,image,strlen(image)+1);
        int descriptor=open(image,O_RDONLY|O_CLOEXEC);struct stat information;
        if(descriptor<0)return 1;
        int observed=fstat(descriptor,&information),closed=close(descriptor);
        if(observed||closed)return 1;
        packet.device=(uint64_t)information.st_dev;packet.inode=(uint64_t)information.st_ino;
    }
    int connection=socket(AF_UNIX,SOCK_STREAM,0);if(connection<0)return 1;
    struct sockaddr_un address={0};address.sun_family=AF_UNIX;
    memcpy(address.sun_path,socket_path,strlen(socket_path)+1);
    int failed=connect(connection,(struct sockaddr *)&address,sizeof(address));
    uint32_t accepted=0;
    if(!failed)failed=fixture_io(connection,&packet,sizeof(packet),1)
        ||fixture_io(connection,&accepted,sizeof(accepted),0);
    int closed=close(connection);
    return failed||closed||accepted!=1;
}
static int fixture_touch(const char *path,const char *text) {
    FILE *file=fopen(path,"wb");if(!file)return 1;
    int failed=fputs(text,file)<0;return fclose(file)||failed;
}
static int fixture_wait_path(const char *path) {
    uint64_t deadline=fixture_now()+4000;
    while(fixture_now()<deadline) {
        if(access(path,F_OK)==0)return 0;
        struct timespec pause={0,1000000};nanosleep(&pause,NULL);
    }
    return 1;
}
#endif
