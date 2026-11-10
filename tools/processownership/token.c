#include <bsm/libbsm.h>
#include <errno.h>
#include <libproc.h>
#include <mach/mach.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/resource.h>
#include <sys/wait.h>
#include <unistd.h>

static int write_token(int fd) {
    audit_token_t token;
    mach_msg_type_number_t count=TASK_AUDIT_TOKEN_COUNT;
    if(task_info(mach_task_self(),TASK_AUDIT_TOKEN,(task_info_t)&token,&count)!=KERN_SUCCESS) return 1;
    return write(fd,&token,sizeof(token))!=sizeof(token);
}

int main(int argc,char **argv) {
    struct rlimit no_core={0,0};
    if(setrlimit(RLIMIT_CORE,&no_core)) return 10;
    alarm(8);
    if(argc==2) {
        if(write_token(atoi(argv[1]))) return 11;
        for(;;) pause();
    }
    int pipefd[2];
    if(pipe(pipefd)) return 12;
    pid_t child=fork();
    if(child<0) return 13;
    if(!child) {
        alarm(5);
        close(pipefd[0]);
        if(write_token(pipefd[1])) _exit(14);
        char fd[16];
        snprintf(fd,sizeof(fd),"%d",pipefd[1]);
        execl(argv[0],argv[0],fd,NULL);
        _exit(15);
    }
    close(pipefd[1]);
    audit_token_t before,after;
    /* Direct-child failure signals are safe while wait ownership retains its PID. */
    if(read(pipefd[0],&before,sizeof(before))!=sizeof(before) || read(pipefd[0],&after,sizeof(after))!=sizeof(after)) {
        kill(child,SIGKILL); waitpid(child,NULL,0); return 16;
    }
    int before_result=proc_signal_with_audittoken(&before,SIGSTOP);
    int after_result=proc_signal_with_audittoken(&after,SIGSTOP);
    int stopped=0;
    if(!after_result && (waitpid(child,&stopped,WUNTRACED)!=child || !WIFSTOPPED(stopped))) {
        kill(child,SIGKILL); waitpid(child,NULL,0); return 18;
    }
    int continued=proc_signal_with_audittoken(&after,SIGCONT);
    int stop_result=proc_signal_with_audittoken(&after,SIGTERM);
    if(stop_result) kill(child,SIGKILL);
    int status=0;
    if(waitpid(child,&status,0)!=child) return 17;
    printf("{\"case\":\"token-signal-across-exec\",\"pid_same\":%s,\"execution_changed\":%s,\"stale_result\":%d,\"current_result\":%d,\"termination_result\":%d,\"signal\":%d}\n",audit_token_to_pid(before)==audit_token_to_pid(after)?"true":"false",audit_token_to_pidversion(before)!=audit_token_to_pidversion(after)?"true":"false",before_result,after_result,stop_result,WIFSIGNALED(status)?WTERMSIG(status):0);
    close(pipefd[0]);
    return before_result!=ESRCH || after_result || continued || stop_result || !WIFSIGNALED(status) || WTERMSIG(status)!=SIGTERM;
}
