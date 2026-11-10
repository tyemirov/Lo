#include <EndpointSecurity/EndpointSecurity.h>
#include <errno.h>
#include <signal.h>
#include <spawn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/event.h>
#include <sys/resource.h>
#include <sys/wait.h>
#include <unistd.h>
#include <sandbox.h>
#include <dlfcn.h>
#include <fcntl.h>
#include <sys/stat.h>

extern char **environ;

static void state(const char *name, int result, int error) {
    struct rlimit limit;
    if (getrlimit(RLIMIT_NPROC, &limit)) _exit(31);
    printf("{\"case\":\"%s\",\"result\":%d,\"errno\":%d,\"pid\":%d,\"group\":%d,\"session\":%d,\"ruid\":%u,\"euid\":%u,\"nproc_hard\":%llu}\n",
        name, result, error, getpid(), getpgrp(), getsid(0), getuid(), geteuid(), (unsigned long long)limit.rlim_max);
    fflush(stdout);
}

static int child_case(const char *executable, const char *scenario) {
    if (!strcmp(scenario, "nproc-bound")) {
        struct rlimit limit = {0, 0};
        if (setrlimit(RLIMIT_NPROC, &limit)) return 18;
        errno = 0;
        pid_t child = fork();
        if (!child) _exit(0);
        state("fork-at-bound", child, errno);
        if (child > 0 && waitpid(child, NULL, 0) != child) return 19;
        char *arguments[] = {(char *)executable, "state", NULL};
        int spawned = posix_spawn(&child, executable, NULL, NULL, arguments, environ);
        state("spawn-at-bound", spawned, spawned);
        if (!spawned && waitpid(child, NULL, 0) != child) return 20;
        posix_spawnattr_t attributes;
        if (posix_spawnattr_init(&attributes)) return 27;
        int (*set_fd_limit)(const posix_spawnattr_t *, uint32_t, uint32_t) = dlsym(RTLD_DEFAULT, "posix_spawnattr_set_filedesclimit_ext");
        if (!set_fd_limit || set_fd_limit(&attributes, 65536, 65536) ||
            posix_spawnattr_setflags(&attributes, POSIX_SPAWN_RESETIDS | POSIX_SPAWN_SETSID)) return 28;
        spawned = posix_spawn(&child, executable, NULL, &attributes, arguments, environ);
        state("spawn-attributes-at-bound", spawned, spawned);
        if (posix_spawnattr_destroy(&attributes)) return 29;
        if (!spawned && waitpid(child, NULL, 0) != child) return 30;
        return 0;
    }
    const char *credential_scope = "(deny syscall-unix (syscall-number SYS_setuid SYS_seteuid SYS_setreuid SYS_setgid SYS_setegid SYS_setregid SYS_setgroups SYS_initgroups SYS_settid SYS_settid_with_pid SYS_setsgroups SYS_setwgroups SYS_persona SYS_setrlimit SYS_proc_rlimit_control))";
    const char *profile = !strcmp(scenario, "spawn-denied")
        ? "(version 1)(allow default)(deny syscall-unix (syscall-number SYS_setsid SYS_setpgid SYS_posix_spawn))"
        : "(version 1)(allow default)(deny syscall-unix (syscall-number SYS_setsid SYS_setpgid))";
    char *error = NULL;
    char combined[2048];
    if (snprintf(combined, sizeof(combined), "%s%s", profile, credential_scope) >= (int)sizeof(combined)) return 21;
    int result = sandbox_init(combined, 0, &error);
    if (result) {
        fprintf(stderr, "sandbox_init: %s\n", error ? error : "unknown");
        if (error) sandbox_free_error(error);
        return 2;
    }
    if (!strcmp(scenario, "credential-change")) {
        errno = 0; result = setuid(getuid()); state(scenario, result, errno);
    } else if (!strcmp(scenario, "resource-change")) {
        struct rlimit limit;
        if (getrlimit(RLIMIT_NPROC, &limit)) return 22;
        errno = 0; result = setrlimit(RLIMIT_NPROC, &limit); state(scenario, result, errno);
    } else if (!strcmp(scenario, "setsid")) {
        errno=0; result=setsid(); state(scenario,result,errno);
    } else if (!strcmp(scenario, "setpgid")) {
        errno=0; result=setpgid(0,0); state(scenario,result,errno);
    } else if (!strcmp(scenario, "fork")) {
        errno=0; pid_t pid=fork();
        if (pid==0) { state("fork-child",0,0); _exit(0); }
        state(scenario,pid,errno);
        if (pid>0) {int status; if(waitpid(pid,&status,0)!=pid) return 3;}
    } else {
        posix_spawnattr_t attributes;
        if(posix_spawnattr_init(&attributes)) return 4;
        short flags = !strcmp(scenario, "spawn-group") ? POSIX_SPAWN_SETPGROUP :
            (!strcmp(scenario, "spawn-resetids") ? POSIX_SPAWN_RESETIDS : POSIX_SPAWN_SETSID);
        if(posix_spawnattr_setflags(&attributes,flags)) return 5;
        if(flags==POSIX_SPAWN_SETPGROUP && posix_spawnattr_setpgroup(&attributes,0)) return 6;
        if (!strcmp(scenario, "spawn-uid")) {
            int (*set_uid)(const posix_spawnattr_t *, uid_t) = dlsym(RTLD_DEFAULT, "posix_spawnattr_set_uid_np");
            if (!set_uid || set_uid(&attributes, 0)) return 24;
        }
        if (!strcmp(scenario, "spawn-persona")) {
            int (*set_persona)(const posix_spawnattr_t *, uid_t, uint32_t) = dlsym(RTLD_DEFAULT, "posix_spawnattr_set_persona_np");
            int (*set_uid)(const posix_spawnattr_t *, uid_t) = dlsym(RTLD_DEFAULT, "posix_spawnattr_set_persona_uid_np");
            if (!set_persona || !set_uid || set_persona(&attributes, 0, 1) || set_uid(&attributes, 0)) return 32;
        }
        char *arguments[]={(char *)executable,"state",NULL};
        pid_t pid=0;
        result=posix_spawn(&pid,executable,NULL,&attributes,arguments,environ);
        state(scenario,result,result);
        if(posix_spawnattr_destroy(&attributes)) return 7;
        if(!result) {int status; if(waitpid(pid,&status,0)!=pid || status) return 8;}
    }
    return 0;
}

int main(int argc,char **argv) {
    alarm(8);
    if(argc==2 && !strcmp(argv[1],"state")) {state("spawn-child",0,0); return 0;}
    struct rlimit no_core={0,0};
    if(setrlimit(RLIMIT_CORE,&no_core)) return 10;
    if(argc!=2) return 11;
    const char *cases[] = {"note-track", "es-admission", "coalition-admission", "setsid", "setpgid", "fork", "spawn-session", "spawn-group", "spawn-denied", "nproc-bound", "credential-change", "resource-change", "spawn-uid", "spawn-resetids", "spawn-persona", "open-marker"};
    int recognized = 0;
    for (size_t index = 0; index < sizeof(cases) / sizeof(cases[0]); index++) recognized |= !strcmp(argv[1], cases[index]);
    if (!recognized) return 23;
    if (!strcmp(argv[1], "open-marker")) {
        int retained = open("/dev/null", O_RDONLY | O_NOFOLLOW | O_CLOEXEC);
        if (retained < 0) return 25;
        int opened = open("/dev/null", O_RDONLY | O_NOFOLLOW | O_CLOEXEC);
        if (opened < 0) {close(retained); return 26;}
        struct stat first, second;
        int valid = !fstat(retained, &first) && !fstat(opened, &second) &&
            first.st_dev == second.st_dev && first.st_ino == second.st_ino;
        int closed = !close(opened);
        closed &= !close(retained);
        printf("{\"case\":\"open-marker\",\"same_identity\":%s,\"closed\":%s,\"es_delivery_tested\":false}\n", valid ? "true" : "false", closed ? "true" : "false");
        return !valid || !closed;
    }
    if(!strcmp(argv[1],"note-track")) {
        int queue=kqueue();
        if(queue<0) return 12;
        struct kevent change,event;
        EV_SET(&change,getpid(),EVFILT_PROC,EV_ADD|EV_RECEIPT,NOTE_FORK|NOTE_TRACK|NOTE_EXIT,0,NULL);
        struct timespec zero={0,0};
        int count=kevent(queue,&change,1,&event,1,&zero);
        printf("{\"case\":\"note-track\",\"count\":%d,\"flags\":%u,\"data\":%lld,\"errno\":%d}\n",count,count>0?event.flags:0,count>0?(long long)event.data:0,errno);
        return close(queue)!=0;
    }
    if(!strcmp(argv[1],"es-admission")) {
        es_client_t *client=NULL;
        es_new_client_result_t result=es_new_descendants_client(&client, ^(es_client_t *c,const es_message_t *message){
            if(message->action_type==ES_ACTION_TYPE_AUTH) es_respond_auth_result(c,message,ES_AUTH_RESULT_DENY,false);
        });
        printf("{\"case\":\"es-admission\",\"result\":%d,\"not_entitled\":%d,\"client_created\":%s}\n",result,ES_NEW_CLIENT_RESULT_ERR_NOT_ENTITLED,client?"true":"false");
        if(client && es_delete_client(client)!=ES_RETURN_SUCCESS) return 13;
        return 0;
    }
    if(!strcmp(argv[1],"coalition-admission")) {
        int (*create)(uint64_t *,uint32_t)=dlsym(RTLD_DEFAULT,"coalition_create");
        int (*terminate)(uint64_t,uint32_t)=dlsym(RTLD_DEFAULT,"coalition_terminate");
        int (*reap)(uint64_t,uint32_t)=dlsym(RTLD_DEFAULT,"coalition_reap");
        if(!create || !terminate || !reap) {
            printf("{\"case\":\"coalition-admission\",\"symbols_available\":false}\n");
            return 0;
        }
        uint64_t cid=0;
        errno=0;
        int result=create(&cid,0),error=errno;
        printf("{\"case\":\"coalition-admission\",\"symbols_available\":true,\"result\":%d,\"errno\":%d,\"created\":%s}\n",result,error,result==0?"true":"false");
        if(!result) {
            if(terminate(cid,0)) return 16;
            if(reap(cid,0)) return 17;
        }
        return 0;
    }
    pid_t pid=fork();
    if(pid<0) return 14;
    if(pid==0) {alarm(5); return child_case(argv[0],argv[1]);}
    int status=0;
    if(waitpid(pid,&status,0)!=pid) return 15;
    printf("{\"case\":\"supervisor\",\"scenario\":\"%s\",\"child_status\":%d}\n",argv[1],status);
    return status!=0;
}
