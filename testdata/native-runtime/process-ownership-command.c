#include "process-ownership-fixture.h"
#include <spawn.h>
#include <sys/wait.h>

static int child_work(const char *socket_path,const char *operation,const char *ready,
                      const char *gate) {
    if(fixture_touch(ready,"ready"))return 72;
    if(!strcmp(operation,"output"))return fixture_io(1,"child-output",12,1);
    if(!strcmp(operation,"finish"))return 0;
    if(!strcmp(operation,"gate"))return fixture_wait_path(gate);
    for(;;)pause();
}

int main(int argc,char **argv) {
    alarm(12);
    if(argc==4&&!strcmp(argv[1],"--host-signal")) {
        audit_token_t token;if(fixture_parse_token(argv[2],&token))return 64;
        int signal_number=atoi(argv[3]);
        if(signal_number!=SIGKILL&&signal_number!=SIGTERM&&signal_number!=SIGUSR1)return 65;
        int result=proc_signal_with_audittoken(&token,signal_number);
        return result==0||result==ESRCH?0:66;
    }
    if(argc==3&&!strcmp(argv[1],"--host-alive")) {
        audit_token_t token;if(fixture_parse_token(argv[2],&token))return 64;
        char path[4096];int result=proc_pidpath_audittoken(&token,path,sizeof(path));
        return result>0?0:1;
    }
    if(argc<6)return 64;
    const char *socket_path=argv[1],*operation=argv[2],*ready=argv[3],*gate=argv[4];
    audit_token_t parent={0};
    uint32_t kind=FIXTURE_ENTER;
    if(!strcmp(argv[5],"spawn")||!strcmp(argv[5],"exec")) {
        if(argc!=7||fixture_parse_token(argv[6],&parent))return 65;
        kind=!strcmp(argv[5],"spawn")?FIXTURE_SPAWN:FIXTURE_EXEC;
    }
    if(fixture_report(socket_path,kind,&parent,argv[0]))return 66;
    if(!strcmp(operation,"fork")||!strcmp(operation,"leader")||!strcmp(operation,"detach")) {
        audit_token_t actor;if(fixture_token(&actor))return 67;
        pid_t child=fork();if(child<0)return 68;
        if(!child) {
            alarm(10);
            if(fixture_report(socket_path,FIXTURE_FORK,&actor,NULL))_exit(69);
            if(!strcmp(operation,"detach")&&setsid()<0)_exit(70);
            _exit(child_work(socket_path,!strcmp(operation,"fork")?"finish":"pause",ready,gate));
        }
        if(!strcmp(operation,"fork")) {
            int status;if(waitpid(child,&status,0)!=child)return 71;
            return WIFEXITED(status)?WEXITSTATUS(status):72;
        }
        return fixture_wait_path(ready);
    }
    if(!strcmp(operation,"spawn")) {
        audit_token_t actor;if(fixture_token(&actor))return 67;
        char token[65];fixture_token_text(&actor,token);
        char *arguments[]={argv[0],argv[1],"finish",argv[3],argv[4],"spawn",token,NULL};
        char *environment[]={NULL};pid_t child=0;
        int spawned=posix_spawn(&child,argv[0],NULL,NULL,arguments,environment);
        if(spawned)return 73;
        int status;if(waitpid(child,&status,0)!=child)return 74;
        return WIFEXITED(status)?WEXITSTATUS(status):75;
    }
    if(!strcmp(operation,"exec")||!strcmp(operation,"exec-pause")) {
        audit_token_t actor;if(fixture_token(&actor))return 67;
        char token[65];fixture_token_text(&actor,token);
        char *arguments[]={argv[0],argv[1],!strcmp(operation,"exec")?"finish":"pause",
                           argv[3],argv[4],"exec",token,NULL};
        char *environment[]={NULL};execve(argv[0],arguments,environment);return 76;
    }
    return child_work(socket_path,operation,ready,gate);
}
