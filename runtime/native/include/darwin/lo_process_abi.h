#ifndef LO_PROCESS_ABI_H
#define LO_PROCESS_ABI_H
/* Lo-owned declarations for its selected Darwin arm64 C ABI surface. */
#include <stddef.h>
#include <stdint.h>
#include <limits.h>
typedef int pid_t;
typedef long ssize_t;
typedef unsigned int nfds_t;
struct timespec { long tv_sec; long tv_nsec; };
struct pollfd { int fd; short events; short revents; };
struct kevent { uintptr_t ident; int16_t filter; uint16_t flags; uint32_t fflags; intptr_t data; void *udata; };
struct rlimit { uint64_t rlim_cur; uint64_t rlim_max; };
typedef void *posix_spawn_file_actions_t;
typedef void *posix_spawnattr_t;
#define POSIX_SPAWN_CLOEXEC_DEFAULT 0x4000
#define CLOCK_MONOTONIC 6
#define POLLIN 1
#define POLLOUT 4
#define F_GETFL 3
#define F_SETFL 4
#define F_SETFD 2
#define F_DUPFD_CLOEXEC 67
#define FD_CLOEXEC 1
#define O_NONBLOCK 4
#define RLIMIT_CORE 4
#define RLIMIT_NOFILE 8
#define EVFILT_PROC (-5)
#define EV_ADD 1
#define EV_ENABLE 4
#define EV_CLEAR 0x20
#define EV_ERROR 0x4000
#define NOTE_EXIT 0x80000000U
#define NOTE_EXEC 0x20000000U
#define WNOHANG 1
#define SIGKILL 9
#define WIFEXITED(s) (((s)&0177)==0)
#define WEXITSTATUS(s) (((s)>>8)&0xff)
#define WIFSIGNALED(s) (((s)&0177)!=0 && ((s)&0177)!=0177)
#define WTERMSIG(s) ((s)&0177)
#define EV_SET(e,i,f,fl,ff,d,u) (*(e)=(struct kevent){(i),(f),(fl),(ff),(d),(u)})
#ifndef errno
extern int *__error(void);
#define errno (*__error())
#endif
#define EPERM 1
#define ESRCH 3
#define EINTR 4
#define EINVAL 22
#define EAGAIN 35
extern int clock_gettime(int,struct timespec *);
extern int nanosleep(const struct timespec *,struct timespec *);
extern int pipe(int[2]);
extern int close(int);
extern ssize_t read(int,void *,size_t);
extern ssize_t write(int,const void *,size_t);
extern int fcntl(int,int,...);
extern int chdir(const char *);
extern void _exit(int) __attribute__((noreturn));
extern int execve(const char *,char *const *,char *const *);
extern int setrlimit(int,const struct rlimit *);
extern int kqueue(void);
extern int kevent(int,const struct kevent *,int,struct kevent *,int,const struct timespec *);
extern int poll(struct pollfd *,nfds_t,int);
extern int kill(pid_t,int);
extern pid_t waitpid(pid_t,int *,int);
extern int posix_spawn_file_actions_init(posix_spawn_file_actions_t *);
extern int posix_spawn_file_actions_destroy(posix_spawn_file_actions_t *);
extern int posix_spawn_file_actions_addchdir_np(posix_spawn_file_actions_t *,const char *);
extern int posix_spawn_file_actions_adddup2(posix_spawn_file_actions_t *,int,int);
extern int posix_spawnattr_init(posix_spawnattr_t *);
extern int posix_spawnattr_setflags(posix_spawnattr_t *,short);
extern int posix_spawnattr_destroy(posix_spawnattr_t *);
extern int posix_spawn(pid_t *,const char *,const posix_spawn_file_actions_t *,const posix_spawnattr_t *,char *const *,char *const *);
extern void *malloc(size_t);
extern void free(void *);
extern size_t strlen(const char *);
extern void *memcpy(void *,const void *,size_t);
extern void *memset(void *,int,size_t);
extern int memcmp(const void *,const void *,size_t);
extern int strcmp(const char *,const char *);
extern int strncmp(const char *,const char *,size_t);
extern char *strcpy(char *,const char *);
extern char *strchr(const char *,int);
extern int sandbox_init(const char *,uint64_t,char **);
extern void sandbox_free_error(char *);
typedef unsigned int mach_port_t;
typedef mach_port_t *mach_port_array_t;
typedef unsigned int mach_msg_type_number_t;
typedef unsigned int mach_port_urefs_t;
typedef int mach_port_delta_t;
typedef int kern_return_t;
typedef unsigned int exception_mask_t;
typedef int exception_behavior_t;
typedef int thread_state_flavor_t;
typedef uintptr_t vm_address_t;
#define MACH_PORT_NULL 0U
#define MACH_PORT_VALID(p) ((p)!=0U && (p)!=~0U)
#define KERN_SUCCESS 0
#define MACH_PORT_RIGHT_SEND 0
#define TASK_BOOTSTRAP_PORT 4
#define EXC_TYPES_COUNT 14
#define EXC_MASK_ALL 0x1bfeU
#define EXCEPTION_DEFAULT 1
#define THREAD_STATE_NONE 5
extern mach_port_t mach_task_self_;
#define mach_task_self() mach_task_self_
extern mach_port_t bootstrap_port;
extern mach_port_t mach_thread_self(void);
extern mach_port_t mach_host_self(void);
extern kern_return_t mach_port_deallocate(mach_port_t,mach_port_t);
extern kern_return_t vm_deallocate(mach_port_t,vm_address_t,uintptr_t);
extern kern_return_t mach_port_mod_refs(mach_port_t,mach_port_t,unsigned int,mach_port_delta_t);
extern kern_return_t mach_port_get_refs(mach_port_t,mach_port_t,unsigned int,mach_port_urefs_t *);
extern kern_return_t mach_ports_lookup(mach_port_t,mach_port_array_t *,mach_msg_type_number_t *);
extern kern_return_t mach_ports_register(mach_port_t,mach_port_array_t,mach_msg_type_number_t);
extern kern_return_t task_get_special_port(mach_port_t,int,mach_port_t *);
extern kern_return_t task_set_special_port(mach_port_t,int,mach_port_t);
extern kern_return_t thread_get_exception_ports(mach_port_t,exception_mask_t,exception_mask_t *,mach_msg_type_number_t *,mach_port_t *,exception_behavior_t *,thread_state_flavor_t *);
extern kern_return_t task_get_exception_ports(mach_port_t,exception_mask_t,exception_mask_t *,mach_msg_type_number_t *,mach_port_t *,exception_behavior_t *,thread_state_flavor_t *);
extern kern_return_t thread_set_exception_ports(mach_port_t,exception_mask_t,mach_port_t,exception_behavior_t,thread_state_flavor_t);
extern kern_return_t task_set_exception_ports(mach_port_t,exception_mask_t,mach_port_t,exception_behavior_t,thread_state_flavor_t);
#endif
