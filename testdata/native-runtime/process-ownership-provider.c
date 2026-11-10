#include "lo_process_ownership.h"
#include "process-ownership-fixture.h"
#include <pthread.h>
#include <sys/event.h>
#include <spawn.h>
#include <dlfcn.h>

/* These controlled commands stop before fixture effects until the controller
 * replies. The provider reports real child tokens through injected events.
 * Cooperative reports do not prove native ES delivery or kernel exec denial. */
#define FIXTURE_ROOT @F036_ROOT@
#define FIXTURE_SOCKET @F036_SOCKET@
#define FIXTURE_TOKENS @F036_TOKENS@
#define FIXTURE_LOG @F036_LOG@
#define FIXTURE_RELEASE @F036_RELEASE@

struct fixture_owner {
    LoProcessOwnershipConfiguration configuration;
    LoProcessOwnershipEmit emit;
    void *destination;
    pthread_mutex_t serial, response;
    pthread_cond_t changed;
    pthread_t thread;
    int listener, events, stopping, thread_started;
    uint64_t sequence, request, pending;
    int decision;
    unsigned marker_count, known, live;
    audit_token_t tokens[FIXTURE_PROCESSES];
    int alive[FIXTURE_PROCESSES];
};

static void fixture_log(const char *text) {
    FILE *file=fopen(FIXTURE_LOG,"ab");if(!file)_exit(91);
    if(fputs(text,file)<0||fclose(file))_exit(92);
}
int posix_spawn(pid_t *pid,const char *path,const posix_spawn_file_actions_t *actions,
                const posix_spawnattr_t *attributes,char *const *arguments,char *const *environment) {
    int (*original)(pid_t *,const char *,const posix_spawn_file_actions_t *,const posix_spawnattr_t *,char *const *,char *const *)=dlsym(RTLD_NEXT,"posix_spawn");
    if(!original)return ENOSYS;
    int result=original(pid,path,actions,attributes,arguments,environment);
    char message[64];snprintf(message,sizeof(message),"spawn-result=%d\n",result);fixture_log(message);
    return result;
}
static void fixture_record(const char *role,const audit_token_t *token) {
    char text[65];fixture_token_text(token,text);
    FILE *file=fopen(FIXTURE_TOKENS,"ab");if(!file)_exit(93);
    if(fprintf(file,"%s %s\n",role,text)<0||fclose(file))_exit(94);
}
static LoProcessExecutionToken fixture_owned_token(audit_token_t token) {
    LoProcessExecutionToken result;memcpy(result.word,token.val,sizeof(result.word));return result;
}
static audit_token_t fixture_native_token(LoProcessExecutionToken token) {
    audit_token_t result;memcpy(result.val,token.word,sizeof(result.val));return result;
}
static int fixture_emit(struct fixture_owner *owner,LoProcessOwnershipEvent *event,int sequenced) {
    pthread_mutex_lock(&owner->serial);
    event->generation=owner->configuration.generation;
    if(sequenced)event->sequence=++owner->sequence;
    int accepted=owner->emit(owner->destination,event);
    pthread_mutex_unlock(&owner->serial);
    return accepted;
}
static int fixture_find(struct fixture_owner *owner,pid_t pid) {
    for(unsigned index=0;index<owner->known;++index)
        if(audit_token_to_pid(owner->tokens[index])==pid)return (int)index;
    return -1;
}
static int fixture_track(struct fixture_owner *owner,audit_token_t token) {
    pid_t pid=audit_token_to_pid(token);int index=fixture_find(owner,pid);
    if(index<0) {
        if(owner->known==FIXTURE_PROCESSES)return 1;
        index=(int)owner->known;
        struct kevent event;EV_SET(&event,(uintptr_t)pid,EVFILT_PROC,EV_ADD|EV_ENABLE,
                                  NOTE_EXIT|NOTE_EXITSTATUS,0,NULL);
        if(kevent(owner->events,&event,1,NULL,0,NULL))return 1;
        owner->alive[index]=1;
        __atomic_add_fetch(&owner->known,1,__ATOMIC_SEQ_CST);
        __atomic_add_fetch(&owner->live,1,__ATOMIC_SEQ_CST);
    }
    owner->tokens[index]=token;fixture_record("command",&token);return 0;
}
static int fixture_authorize(struct fixture_owner *owner,const struct fixture_packet *packet,
                             audit_token_t previous) {
    LoProcessOwnershipEvent event={0};event.kind=LO_PROCESS_OWNER_AUTH_EXEC;
    event.actor=fixture_owned_token(previous);event.target=fixture_owned_token(packet->target);
    event.file=(LoProcessFileIdentity){packet->device,packet->inode};
    event.path=(const unsigned char *)packet->path;event.path_size=strlen(packet->path);
    event.request=++owner->request;
    if(LO_CASE==12&&packet->kind!=FIXTURE_LAUNCHER)++event.file.inode;
    pthread_mutex_lock(&owner->response);
    owner->pending=event.request;owner->decision=-1;
    pthread_mutex_unlock(&owner->response);
    if((LO_CASE==7||LO_CASE==8)&&packet->kind==FIXTURE_EXEC) {
        fixture_log("H\n");
        if(LO_CASE==7&&fixture_wait_path(FIXTURE_RELEASE))return 0;
    }
    if(LO_CASE==21&&packet->kind==FIXTURE_LAUNCHER) {
        LoProcessOwnershipEvent failure={0};failure.kind=LO_PROCESS_OWNER_FAILURE;failure.status=LO_PROCESS_OWNER_LOST;
        fixture_emit(owner,&failure,0);
    }
    if(!fixture_emit(owner,&event,1)) {
        /* The controller did not retain a reply obligation. Deny and release
         * this cooperative fixture request here, exactly once. */
        pthread_mutex_lock(&owner->response);
        owner->decision=LO_PROCESS_OWNER_DENY;
        owner->pending=0;
        pthread_mutex_unlock(&owner->response);
        fixture_log("D-provider\n");
        return 0;
    }
    if((LO_CASE==18||LO_CASE==19)&&packet->kind==FIXTURE_LAUNCHER) {
        LoProcessOwnershipEvent invalid={0};invalid.kind=LO_CASE==18?LO_PROCESS_OWNER_EXIT:LO_PROCESS_OWNER_OPEN;
        invalid.actor=event.target;
        if(LO_CASE==19){invalid.file=owner->configuration.marker;invalid.flags=LO_PROCESS_OWNER_OPEN_READ;}
        fixture_emit(owner,&invalid,1);
    }
    uint64_t deadline=fixture_now()+4000;
    for(;;) {
        pthread_mutex_lock(&owner->response);int decision=owner->decision;
        pthread_mutex_unlock(&owner->response);
        if(decision!=-1) {
            if(decision!=LO_PROCESS_OWNER_ALLOW) {
                int index=fixture_find(owner,audit_token_to_pid(packet->target));
                if(index>=0)owner->tokens[index]=previous;
                return 0;
            }
            if(LO_CASE==8&&packet->kind==FIXTURE_EXEC) {
                fixture_log("A-ready\n");
                if(fixture_wait_path(FIXTURE_RELEASE))return 0;
            }
            event.kind=LO_PROCESS_OWNER_EXEC;event.request=0;
            event.actor=event.target;event.target=(LoProcessExecutionToken){0};
            fixture_emit(owner,&event,1);return 1;
        }
        if(fixture_now()>=deadline||__atomic_load_n(&owner->stopping,__ATOMIC_SEQ_CST))return 0;
        struct timespec pause={0,1000000};nanosleep(&pause,NULL);
    }
}
static void fixture_accept(struct fixture_owner *owner) {
    int connection=accept(owner->listener,NULL,NULL);if(connection<0)return;
    struct fixture_packet packet;uint32_t accepted=0;
    if(fixture_io(connection,&packet,sizeof(packet),0)||packet.magic!=FIXTURE_MAGIC
       ||!memchr(packet.path,0,sizeof(packet.path)))goto finished;
    pid_t pid=audit_token_to_pid(packet.target);
    int index=fixture_find(owner,pid);audit_token_t previous={0};
    if(index>=0)previous=owner->tokens[index];
    if(fixture_track(owner,packet.target))goto finished;
    LoProcessOwnershipEvent event={0};
    if(packet.kind==FIXTURE_LAUNCHER||packet.kind==FIXTURE_FORK||packet.kind==FIXTURE_SPAWN) {
        event.kind=LO_PROCESS_OWNER_FORK;
        event.actor=packet.kind==FIXTURE_LAUNCHER?owner->configuration.supervisor:fixture_owned_token(packet.actor);
        previous=packet.target;
        if(packet.kind==FIXTURE_SPAWN || (packet.kind==FIXTURE_LAUNCHER&&LO_CASE!=16))previous.val[7]^=0x80000000U;
        if(LO_CASE==15&&packet.kind==FIXTURE_FORK) {
            fixture_log("F-pending\n");
            if(fixture_wait_path(FIXTURE_RELEASE))goto finished;
        }
        if(LO_CASE==23&&packet.kind==FIXTURE_FORK) {
            LoProcessOwnershipEvent consumed={0};consumed.kind=LO_PROCESS_OWNER_OPEN;consumed.actor=fixture_owned_token(packet.actor);
            consumed.file=owner->configuration.marker;consumed.flags=LO_PROCESS_OWNER_OPEN_READ;
            for(uint64_t count=0;count<=owner->configuration.max_events;count++)fixture_emit(owner,&consumed,1);
        }
        event.target=fixture_owned_token(previous);fixture_emit(owner,&event,1);
        if(LO_CASE==23&&packet.kind==FIXTURE_FORK)fixture_log("F-after-limit\n");
    }
    if((packet.kind==FIXTURE_LAUNCHER&&LO_CASE!=16)||packet.kind==FIXTURE_ENTER||packet.kind==FIXTURE_SPAWN||packet.kind==FIXTURE_EXEC) {
        if(packet.kind==FIXTURE_EXEC)previous=packet.actor;
        accepted=fixture_authorize(owner,&packet,previous);
        if(accepted&&packet.kind==FIXTURE_ENTER&&(LO_CASE==6||LO_CASE==11)) {
            event=(LoProcessOwnershipEvent){0};event.kind=LO_PROCESS_OWNER_OPEN;
            event.actor=fixture_owned_token(packet.target);event.file=owner->configuration.marker;
            event.flags=LO_PROCESS_OWNER_OPEN_READ;
            if(LO_CASE==6) {
                pthread_mutex_lock(&owner->serial);++owner->sequence;pthread_mutex_unlock(&owner->serial);
                fixture_emit(owner,&event,1);fixture_log("gap\n");
            } else {
                if(owner->configuration.max_events>100000)_exit(95);
                for(uint64_t count=0;count<=owner->configuration.max_events;++count)fixture_emit(owner,&event,1);
                fixture_log("limit\n");
            }
        }
    } else if(packet.kind==FIXTURE_LAUNCHER||packet.kind==FIXTURE_FORK)accepted=1;
finished:
    fixture_io(connection,&accepted,sizeof(accepted),1);close(connection);
}
static void *fixture_events(void *argument) {
    struct fixture_owner *owner=argument;
    while(!__atomic_load_n(&owner->stopping,__ATOMIC_SEQ_CST)) {
        struct pollfd pending[2]={{owner->listener,POLLIN,0},{owner->events,POLLIN,0}};
        int available=poll(pending,2,10);if(available<0&&errno==EINTR)continue;
        if(available<0)break;
        if(pending[0].revents&POLLIN)fixture_accept(owner);
        if(pending[1].revents&POLLIN) {
            struct kevent events[FIXTURE_PROCESSES];struct timespec immediate={0,0};
            int count=kevent(owner->events,NULL,0,events,FIXTURE_PROCESSES,&immediate);
            for(int item=0;item<count;++item) {
                int index=fixture_find(owner,(pid_t)events[item].ident);
                if(index<0||!owner->alive[index])continue;
                owner->alive[index]=0;__atomic_sub_fetch(&owner->live,1,__ATOMIC_SEQ_CST);
                LoProcessOwnershipEvent event={0};event.kind=LO_PROCESS_OWNER_EXIT;
                event.actor=fixture_owned_token(owner->tokens[index]);
                fixture_emit(owner,&event,1);fixture_log("X\n");
            }
        }
    }
    return NULL;
}
static int fixture_marker(uintptr_t handle,uint64_t request) {
    struct fixture_owner *owner=(void *)handle;
    int descriptor=open("/dev/null",O_RDONLY|O_CLOEXEC|O_NOFOLLOW);if(descriptor<0)return -1;
    ++owner->marker_count;fixture_log("M\n");
    if(LO_CASE==17&&owner->marker_count==1) {
        LoProcessOwnershipEvent premature={0};premature.kind=LO_PROCESS_OWNER_SYNC;premature.request=request;
        fixture_emit(owner,&premature,0);
    }
    if(LO_CASE==9&&owner->marker_count>1&&__atomic_load_n(&owner->known,__ATOMIC_SEQ_CST)
       &&__atomic_load_n(&owner->live,__ATOMIC_SEQ_CST)==0)return descriptor;
    LoProcessOwnershipEvent event={0};event.kind=LO_PROCESS_OWNER_OPEN;
    event.actor=owner->configuration.supervisor;event.file=owner->configuration.marker;
    event.flags=LO_PROCESS_OWNER_OPEN_READ;event.path=(const unsigned char *)"/dev/null";event.path_size=9;
    (void)request;fixture_emit(owner,&event,1);return descriptor;
}
static int fixture_synchronize(uintptr_t handle,uint64_t request) {
    struct fixture_owner *owner=(void *)handle;
    LoProcessOwnershipEvent event={0};event.kind=LO_PROCESS_OWNER_SYNC;event.request=request;
    if(LO_CASE==10&&owner->marker_count>1&&__atomic_load_n(&owner->known,__ATOMIC_SEQ_CST)
       &&__atomic_load_n(&owner->live,__ATOMIC_SEQ_CST)==0) {
        pthread_mutex_lock(&owner->serial);event.generation=owner->configuration.generation+1;
        owner->emit(owner->destination,&event);pthread_mutex_unlock(&owner->serial);
    } else fixture_emit(owner,&event,0);
    fixture_log("S\n");return 0;
}
static int fixture_respond(uintptr_t handle,uint64_t request,uint32_t decision) {
    struct fixture_owner *owner=(void *)handle;
    pthread_mutex_lock(&owner->response);
    int failed=request!=owner->pending||owner->decision!=-1||decision>LO_PROCESS_OWNER_ALLOW;
    if(!failed)owner->decision=(int)decision;
    pthread_mutex_unlock(&owner->response);
    if(!failed)fixture_log(decision==LO_PROCESS_OWNER_ALLOW?"A\n":"D\n");
    return failed?EINVAL:0;
}
static int fixture_signal(uintptr_t handle,const LoProcessExecutionToken *token,int32_t number) {
    (void)handle;fixture_log("K\n");
    if((LO_CASE==5||LO_CASE==7||LO_CASE==8||LO_CASE==15)&&fixture_wait_path(FIXTURE_RELEASE))return ETIMEDOUT;
    audit_token_t native=fixture_native_token(*token);
    return proc_signal_with_audittoken(&native,number);
}
static int fixture_close(uintptr_t handle) {
    struct fixture_owner *owner=(void *)handle;
    if(LO_CASE==13){fixture_log("close-failed\n");return EIO;}
    __atomic_store_n(&owner->stopping,1,__ATOMIC_SEQ_CST);
    if(owner->thread_started&&pthread_join(owner->thread,NULL))return EIO;
    int failed=close(owner->listener)|close(owner->events);
    if(failed)return EIO;
    pthread_cond_destroy(&owner->changed);pthread_mutex_destroy(&owner->response);
    pthread_mutex_destroy(&owner->serial);fixture_log("C\n");free(owner);return 0;
}
/* The Go fixture owns this bound listener across activations. The provider
 * receives the same open description through a separate test-only broker;
 * command endpoint identity never changes after policy admission. */
static int fixture_listener(void) {
    int connection=socket(AF_UNIX,SOCK_STREAM,0);if(connection<0)return -1;
    struct sockaddr_un address={0};address.sun_family=AF_UNIX;
    if(strlen(FIXTURE_SOCKET ".owner")>=sizeof(address.sun_path)){close(connection);return -1;}
    strcpy(address.sun_path,FIXTURE_SOCKET ".owner");
    struct timeval timeout={1,0};
    if(setsockopt(connection,SOL_SOCKET,SO_RCVTIMEO,&timeout,sizeof(timeout))
        ||connect(connection,(struct sockaddr *)&address,sizeof(address))){close(connection);return -1;}
    unsigned char marker=0;struct iovec vector={&marker,1};
    union {struct cmsghdr align;unsigned char bytes[CMSG_SPACE(sizeof(int))];} control={0};
    struct msghdr message={0};message.msg_iov=&vector;message.msg_iovlen=1;
    message.msg_control=control.bytes;message.msg_controllen=sizeof(control.bytes);
    ssize_t received=recvmsg(connection,&message,0);int descriptor=-1;
    struct cmsghdr *rights=CMSG_FIRSTHDR(&message);
    if(received==1&&marker==1&&!(message.msg_flags&(MSG_CTRUNC|MSG_TRUNC))&&rights
        &&rights->cmsg_level==SOL_SOCKET&&rights->cmsg_type==SCM_RIGHTS
        &&rights->cmsg_len==CMSG_LEN(sizeof(int)))memcpy(&descriptor,CMSG_DATA(rights),sizeof(descriptor));
    int closed=close(connection);
    if(descriptor<0)return -1;
    if(closed||fcntl(descriptor,F_SETFD,FD_CLOEXEC)){close(descriptor);return -1;}
    return descriptor;
}
static uint32_t fixture_open(uintptr_t user,const LoProcessOwnershipConfiguration *configuration,
                             LoProcessOwnershipEmit emit,void *destination,LoProcessOwnershipClient *client) {
    if(user!=36||!configuration||!emit)return LO_PROCESS_OWNER_FAILED;
    struct fixture_owner *owner=calloc(1,sizeof(*owner));if(!owner)return LO_PROCESS_OWNER_FAILED;
    owner->configuration=*configuration;owner->emit=emit;owner->destination=destination;
    owner->listener=owner->events=-1;
    if(pthread_mutex_init(&owner->serial,NULL)||pthread_mutex_init(&owner->response,NULL)
       ||pthread_cond_init(&owner->changed,NULL))_exit(96);
    owner->listener=fixture_listener();owner->events=kqueue();
    if(owner->listener<0||owner->events<0)_exit(98);
    *client=(LoProcessOwnershipClient){(uintptr_t)owner,fixture_marker,fixture_synchronize,
                                      fixture_respond,fixture_signal,fixture_close};
    audit_token_t token;if(fixture_token(&token))_exit(99);
    if(memcmp(token.val,configuration->supervisor.word,sizeof(token.val)))_exit(99);
    fixture_record("supervisor",&token);
    if(pthread_create(&owner->thread,NULL,fixture_events,owner))return LO_PROCESS_OWNER_FAILED;
    owner->thread_started=1;fixture_log("O\n");return LO_PROCESS_OWNER_READY;
}
static void fixture_exit_caller(int signal_number){(void)signal_number;_exit(0);}
int lo_test_host_init(int argc,char **argv,char **environment) {
    (void)environment;alarm(15);signal(SIGPIPE,SIG_IGN);
    if(!lo_process_ownership_provider(fixture_open,36))return 1;
    if(argc==2&&!strcmp(argv[1],"--lo-process-launcher"))
        return fixture_report(FIXTURE_SOCKET,FIXTURE_LAUNCHER,NULL,argv[0]);
    if(argc>1&&(!strcmp(argv[1],"run")||!strcmp(argv[1],"serve"))) {
        audit_token_t token;if(fixture_token(&token))return 1;
        fixture_record("caller",&token);signal(SIGUSR1,fixture_exit_caller);
    }
    return 0;
}
extern int lo_cli_main(int,const char *const *,const char *const *);
int main(int argc,char **argv,char **environment) {
    return lo_cli_main(argc,(const char *const *)argv,(const char *const *)environment);
}
