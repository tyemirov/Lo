#ifdef __APPLE__
#ifdef LO_NATIVE_RUNTIME
#include "lo_process_abi.h"
#else
#include <sys/event.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <spawn.h>
#include <poll.h>
#include <fcntl.h>
#include <unistd.h>
#include <errno.h>
#include <stdint.h>
#include <string.h>
#include <signal.h>
#include <time.h>
#include <stdlib.h>
#endif

/* A single synchronous owner retains every endpoint and primary until reap.
 * The Rust caller can stop waiting; this owner still completes cleanup. */
struct lo_process_result {
    int stage, class_id, started, cleanup_complete, termination_kind, termination_value;
    size_t stdin_written, stdout_size, stderr_size;
    unsigned int cleanup_failures;
    uintptr_t ownership;
};
struct ambiguous_descriptor {int number,error;};
struct lo_process_inventory {struct ambiguous_descriptor descriptors[32];unsigned int count,failures;pid_t uncertain_pid;void *actions,*attributes;};
static _Thread_local struct lo_process_inventory *owned;
static void retain_ambiguous(int descriptor,int error){owned->descriptors[owned->count++]=(struct ambiguous_descriptor){descriptor,error};owned->failures|=1;}
static uint64_t now_ms(void) {
    struct timespec now; clock_gettime(CLOCK_MONOTONIC, &now);
    return (uint64_t)now.tv_sec * 1000 + (uint64_t)now.tv_nsec / 1000000;
}
static int release_fd(int *fd) {
    if (*fd < 0) return 0;
    int value=*fd; *fd=-1;
    int result=close(value);
    /* An attempted close can release the numeric slot even on error. Never
     * address it again; retain its ambiguous disposition with the owner. */
    if(result) retain_ambiguous(value,errno);
    return result;
}
static int nonblock(int fd) {
    int flags=fcntl(fd,F_GETFL); return flags<0?-1:fcntl(fd,F_SETFL,flags|O_NONBLOCK);
}
static void fail(struct lo_process_result *r,int stage,int class_id) {
    if (!r->stage) {r->stage=stage;r->class_id=class_id;}
}
int lo_native_process_execute(const char *launcher,int embedded,const char *directory,
    const unsigned char *frame,size_t frame_size,const unsigned char *configuration,size_t configuration_size,
    const unsigned char *input,size_t input_size,unsigned char *output,unsigned char *diagnostic,size_t capacity,
    int (*cancelled)(void*),void *token,uint64_t timeout_ms,struct lo_process_result *r) {
    memset(r,0,sizeof(*r));
    owned=malloc(sizeof(*owned));
    if(!owned){r->stage=1;r->class_id=2;r->cleanup_complete=1;return 0;}
    memset(owned,0,sizeof(*owned));
    int pipes[7][2],queue=-1; pid_t pid=-1;
    for(int i=0;i<7;i++) pipes[i][0]=pipes[i][1]=-1;
    uint64_t deadline=now_ms()+timeout_ms;
    for(int i=0;i<7;i++) {
        if(pipe(pipes[i])) {fail(r,1,2);goto finish;}
        /* Move every source above destination descriptors before spawn actions. */
        for(int j=0;j<2;j++) {
            int original=pipes[i][j];
            int moved=fcntl(original,F_DUPFD_CLOEXEC,16);
            if(moved<0) {fail(r,1,2);goto finish;}
            pipes[i][j]=moved;
            if(close(original)) {retain_ambiguous(original,errno);fail(r,1,2);goto finish;}
        }
    }
    queue=kqueue(); if(queue<0) {fail(r,1,2);goto finish;}
    if(fcntl(queue,F_SETFD,FD_CLOEXEC)) {fail(r,1,2);goto finish;}
    posix_spawn_file_actions_t actions=NULL;
    if(posix_spawn_file_actions_init(&actions)) {if(actions){owned->actions=actions;owned->failures|=4;}fail(r,1,2);goto finish;}
    posix_spawnattr_t attributes=NULL;
    int attributes_ready=posix_spawnattr_init(&attributes)==0;
    if(!attributes_ready&&attributes){owned->attributes=attributes;owned->failures|=4;}
    int attributes_flags=attributes_ready?posix_spawnattr_setflags(&attributes,POSIX_SPAWN_CLOEXEC_DEFAULT):-1;
    int sources[7]={pipes[0][0],pipes[1][1],pipes[2][1],pipes[3][0],pipes[4][1],pipes[5][0],pipes[6][0]};
    int setup=!attributes_ready||attributes_flags!=0;
    setup|=posix_spawn_file_actions_addchdir_np(&actions,directory);
    for(int i=0;i<7;i++) setup|=posix_spawn_file_actions_adddup2(&actions,sources[i],i);
    char *arguments[]={(char*)launcher,embedded?"--lo-process-launcher":NULL,NULL};
    size_t environment_size=strlen(directory)+8;
    char *temporary=malloc(environment_size);
    if(!temporary) setup=1;
    else {memcpy(temporary,"TMPDIR=",7);strcpy(temporary+7,directory);}
    char *environment[]={"CI=1",temporary,NULL};
    if(cancelled(token)) {fail(r,1,1);setup=1;}
    int spawned=setup ? -1 : posix_spawn(&pid,launcher,&actions,&attributes,arguments,environment);
    free(temporary);
    int destroyed=posix_spawn_file_actions_destroy(&actions);
    int attributes_destroyed=attributes_ready?posix_spawnattr_destroy(&attributes):0;
    if(destroyed){owned->actions=actions;owned->failures|=4;}
    if(attributes_destroyed){owned->attributes=attributes;owned->failures|=4;}
    if(spawned) {fail(r,1,2);goto finish;}
    r->started=1;
    if(destroyed||attributes_destroyed) fail(r,1,2);
    for(int i=0;i<7;i++) {
        int child_end=(i==1||i==2||i==4)?1:0;
        if(release_fd(&pipes[i][child_end])) fail(r,1,2);
        if(nonblock(pipes[i][1-child_end])) fail(r,1,2);
    }
    struct kevent watch;
    EV_SET(&watch,(uintptr_t)pid,EVFILT_PROC,EV_ADD|EV_ENABLE|EV_CLEAR,NOTE_EXIT,0,NULL);
    if(kevent(queue,&watch,1,NULL,0,NULL)<0) fail(r,1,0);
    size_t frame_offset=0,configuration_offset=0,status_size=0;
    unsigned char status[6];
    int ready=0,permitted=0,executed=0,exit_seen=0,reaped=0,stopping=0;
    for(;;) {
        if(!r->stage && cancelled(token)) fail(r,7,1);
        if(!r->stage && now_ms()>=deadline) fail(r,7,1);
        if(r->stage && !stopping) {
            stopping=1;
            for(int i=0;i<7;i++) for(int j=0;j<2;j++) if(release_fd(&pipes[i][j])) r->cleanup_complete=-1;
            if(!reaped && !exit_seen) (void)kill(pid,SIGKILL);
        }
        struct kevent events[8]; struct timespec zero={0,0};
        int count=kevent(queue,NULL,0,events,8,&zero);
        if(count<0 && errno!=EINTR) {fail(r,5,2);}
        for(int i=0;i<count;i++) {
            if(events[i].flags&EV_ERROR) {fail(r,5,2);}
            if(events[i].fflags&NOTE_EXEC) {
                if(!permitted) fail(r,5,0);
                else executed=1;
            }
            if(events[i].fflags&NOTE_EXIT) exit_seen=1;
        }
        if(!reaped && (exit_seen||stopping)) {
            int state=0;pid_t observed=waitpid(pid,&state,WNOHANG);
            if(observed==pid) {
                reaped=1;
                if(WIFEXITED(state)) {r->termination_kind=1;r->termination_value=WEXITSTATUS(state);}
                else if(WIFSIGNALED(state)) {r->termination_kind=2;r->termination_value=WTERMSIG(state);}
                else fail(r,5,2);
                if(!executed && !r->stage) fail(r,5,0);
            } else if(observed<0 && errno!=EINTR) {fail(r,5,2);owned->uncertain_pid=pid;owned->failures|=2;break;}
        }
        if(stopping) {if(reaped) break;struct timespec pause={0,10000000};nanosleep(&pause,NULL);continue;}
        struct pollfd ports[7];
        for(int i=0;i<7;i++) {int end=(i==1||i==2||i==4)?0:1;ports[i]=(struct pollfd){pipes[i][end],(i==1||i==2||i==4)?POLLIN:POLLOUT,0};}
        ports[5].fd=-1;
        if(poll(ports,7,10)<0 && errno!=EINTR) {fail(r,5,2);continue;}
        for(int i=0;i<7 && !r->stage;i++) {
            if(ports[i].fd<0||!ports[i].revents) continue;
            if(i==5) continue;
            if(i==0||i==3||i==6) {
                const unsigned char *bytes=i==0?input:i==3?frame:configuration;
                size_t size=i==0?input_size:i==3?frame_size:configuration_size;
                size_t *offset=i==0?&r->stdin_written:i==3?&frame_offset:&configuration_offset;
                if(*offset<size) {
                    ssize_t wrote=write(ports[i].fd,bytes+*offset,size-*offset);
                    if(wrote>0) *offset+=(size_t)wrote;
                    else if(wrote==0 || (errno!=EAGAIN&&errno!=EINTR)) fail(r,i==0?2:1,0);
                }
                if(*offset==size && release_fd(&pipes[i][1])) fail(r,i==0?2:1,0);
            } else if(i==4) {
                ssize_t got=read(ports[i].fd,status+status_size,6-status_size);
                if(got>0) {
                    status_size+=(size_t)got;
                    if(status_size==6) {
                        if(status[0]=='R' && status[1]==0 && status[2]==0 && status[3]==0 && status[4]==0 && status[5]==0 && !ready) {
                            ready=1;
                            EV_SET(&watch,(uintptr_t)pid,EVFILT_PROC,EV_ADD|EV_ENABLE|EV_CLEAR,NOTE_EXEC|NOTE_EXIT,0,NULL);
                            if(kevent(queue,&watch,1,NULL,0,NULL)<0) {fail(r,1,0);continue;}
                            if(cancelled(token)) {fail(r,7,1);continue;}
                            unsigned char permit='P';
                            ssize_t sent=write(pipes[5][1],&permit,1);permitted=sent==1;
                            if(!permitted || release_fd(&pipes[5][1])) fail(r,1,0);
                        } else fail(r,1,0);
                        status_size=0;
                    }
                } else if(got==0) {
                    if(status_size||!ready) fail(r,1,0);
                    if(release_fd(&pipes[4][0])) fail(r,1,0);
                } else if(errno!=EAGAIN&&errno!=EINTR) fail(r,1,0);
            } else {
                size_t *size=i==1?&r->stdout_size:&r->stderr_size;
                unsigned char *destination=i==1?output:diagnostic;
                unsigned char buffer[4096];size_t remain=capacity-*size;
                ssize_t got=read(ports[i].fd,buffer,remain<sizeof(buffer)?remain+1:sizeof(buffer));
                if(got>0) {
                    size_t keep=(size_t)got<remain?(size_t)got:remain;
                    memcpy(destination+*size,buffer,keep);*size+=keep;
                    if((size_t)got>remain) fail(r,i==1?3:4,3);
                } else if(got==0) {if(release_fd(&pipes[i][0])) fail(r,i==1?3:4,0);}
                else if(errno!=EAGAIN&&errno!=EINTR) fail(r,i==1?3:4,0);
            }
        }
        if(reaped && pipes[0][1]<0 && pipes[1][0]<0 && pipes[2][0]<0 && pipes[4][0]<0) break;
    }
finish:
    for(int i=0;i<7;i++) for(int j=0;j<2;j++) if(release_fd(&pipes[i][j])) {fail(r,6,2);r->cleanup_complete=-1;}
    if(release_fd(&queue)) {fail(r,6,2);r->cleanup_complete=-1;}
    r->cleanup_failures=owned->failures;
    if(owned->count||owned->uncertain_pid||owned->actions||owned->attributes){r->cleanup_complete=0;r->ownership=(uintptr_t)owned;}
    else {r->cleanup_complete=1;free(owned);}
    owned=NULL;
    return 0;
}
#endif
