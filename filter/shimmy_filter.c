/*
 * shimmy_filter.c — DynamoRIO syscall-filter client for shimmy-sandbox
 *
 * Blocks (all configurable via argv):
 *   - Network:   socket, connect, bind, listen, sendto, sendmsg, recvfrom, recvmsg
 *   - Spawning:  fork, vfork, clone (non-thread), execve, execveat
 *   - Dangerous: ptrace, mount, chroot, pivot_root, unshare, setns
 *   - RWX mmap:  mmap when PROT_WRITE|PROT_EXEC both set
 *   - File paths: open/openat restricted to comma-separated -allowed_paths whitelist
 *
 * Command-line arguments (passed via dr_client_main argc/argv):
 *   -allowed_paths <csv>   comma-separated path prefixes allowed for open/openat
 *   -block_network <0|1>   block network syscalls (default: 1)
 *   -block_exec <0|1>      block fork/execve (default: 1)
 *   -block_ptrace <0|1>    block ptrace/mount/chroot etc (default: 1)
 *   -block_rwx <0|1>       block RWX mmap (default: 1)
 *   -extra_blocked <csv>   additional syscall numbers to block (comma-separated ints)
 *
 * Build: see CMakeLists.txt and build.sh in this directory.
 */

#include "dr_api.h"
#include "drmgr.h"
#include "drsyscall.h"

#include <string.h>
#include <stdlib.h>
#include <stdio.h>
#include <errno.h>

/* Linux syscall numbers (x86-64) */
#ifndef SYS_read
#  include <sys/syscall.h>
#endif

/* PROT flags */
#ifndef PROT_WRITE
#  define PROT_WRITE 0x2
#endif
#ifndef PROT_EXEC
#  define PROT_EXEC  0x4
#endif

/* CLONE_THREAD flag */
#ifndef CLONE_THREAD
#  define CLONE_THREAD 0x00010000
#endif

/* ------------------------------------------------------------------ */
/* Sandbox policy (populated from argv)                                */
/* ------------------------------------------------------------------ */

#define MAX_ALLOWED_PATHS  64
#define MAX_EXTRA_BLOCKED  256

typedef struct {
    /* Feature flags — all default ON */
    bool block_network;  /* block socket/connect/bind/listen/send*/recv */
    bool block_exec;     /* block fork/vfork/clone(non-thread)/execve/execveat */
    bool block_ptrace;   /* block ptrace/mount/chroot/pivot_root/unshare/setns */
    bool block_rwx;      /* block mmap with PROT_WRITE|PROT_EXEC */

    /* Allowed-path whitelist for open/openat */
    char *allowed_paths[MAX_ALLOWED_PATHS];
    int   num_allowed_paths;

    /* Extra syscall numbers to unconditionally block */
    int   extra_blocked[MAX_EXTRA_BLOCKED];
    int   num_extra_blocked;
} SandboxPolicy;

static SandboxPolicy g_policy = {
    .block_network = true,
    .block_exec    = true,
    .block_ptrace  = true,
    .block_rwx     = true,
    .num_allowed_paths = 0,
    .num_extra_blocked = 0,
};

/* ------------------------------------------------------------------ */
/* Argument parsing helpers                                             */
/* ------------------------------------------------------------------ */

static void
parse_allowed_paths(const char *csv)
{
    if (!csv || csv[0] == '\0')
        return;

    char *copy = strdup(csv);
    char *tok  = strtok(copy, ",");
    while (tok && g_policy.num_allowed_paths < MAX_ALLOWED_PATHS) {
        /* strip leading/trailing whitespace */
        while (*tok == ' ') tok++;
        char *end = tok + strlen(tok) - 1;
        while (end > tok && (*end == ' ' || *end == '\n')) { *end = '\0'; end--; }
        if (*tok != '\0') {
            g_policy.allowed_paths[g_policy.num_allowed_paths++] = strdup(tok);
        }
        tok = strtok(NULL, ",");
    }
    free(copy);
}

static void
parse_extra_blocked(const char *csv)
{
    if (!csv || csv[0] == '\0')
        return;

    char *copy = strdup(csv);
    char *tok  = strtok(copy, ",");
    while (tok && g_policy.num_extra_blocked < MAX_EXTRA_BLOCKED) {
        /* strip whitespace */
        while (*tok == ' ') tok++;
        if (*tok != '\0') {
            g_policy.extra_blocked[g_policy.num_extra_blocked++] = atoi(tok);
        }
        tok = strtok(NULL, ",");
    }
    free(copy);
}

static bool
parse_bool_arg(const char *val)
{
    return val && val[0] == '1';
}

/* Parse all supported flags from the client argv. */
static void
parse_policy_args(int argc, const char *argv[])
{
    for (int i = 1; i < argc - 1; i++) {
        if (strcmp(argv[i], "-allowed_paths") == 0) {
            parse_allowed_paths(argv[i + 1]);
            i++;
        } else if (strcmp(argv[i], "-block_network") == 0) {
            g_policy.block_network = parse_bool_arg(argv[i + 1]);
            i++;
        } else if (strcmp(argv[i], "-block_exec") == 0) {
            g_policy.block_exec = parse_bool_arg(argv[i + 1]);
            i++;
        } else if (strcmp(argv[i], "-block_ptrace") == 0) {
            g_policy.block_ptrace = parse_bool_arg(argv[i + 1]);
            i++;
        } else if (strcmp(argv[i], "-block_rwx") == 0) {
            g_policy.block_rwx = parse_bool_arg(argv[i + 1]);
            i++;
        } else if (strcmp(argv[i], "-extra_blocked") == 0) {
            parse_extra_blocked(argv[i + 1]);
            i++;
        }
    }
}

/* ------------------------------------------------------------------ */
/* Allowed-path whitelist                                               */
/* ------------------------------------------------------------------ */

static bool
path_is_allowed(const char *path)
{
    if (!path)
        return false;
    /* If no whitelist configured, allow all paths. */
    if (g_policy.num_allowed_paths == 0)
        return true;
    for (int i = 0; i < g_policy.num_allowed_paths; i++) {
        if (strncmp(path, g_policy.allowed_paths[i],
                    strlen(g_policy.allowed_paths[i])) == 0)
            return true;
    }
    return false;
}

/* ------------------------------------------------------------------ */
/* Helpers                                                             */
/* ------------------------------------------------------------------ */

static void
block_syscall(void *drcontext, drsyscall_arg_t *arg_array,
              const char *reason)
{
    (void)arg_array;
    dr_fprintf(STDERR, "[shimmy] BLOCKED: %s\n", reason);
    drsyscall_set_result(drcontext, (reg_t)(ptr_int_t)(-EPERM));
    drsyscall_skip_syscall(drcontext);
}

/* Read a string argument from a syscall parameter (copies up to max bytes). */
static bool
get_str_arg(void *drcontext, uint param_ordinal, char *buf, size_t bufsz)
{
    drsyscall_arg_t arg;
    if (drsyscall_get_arg(drcontext, param_ordinal, &arg) != DRMF_SUCCESS)
        return false;
    if (arg.value == (reg_t)NULL)
        return false;
    /* Safe copy from app memory */
    size_t copied = dr_safe_read((void *)(ptr_uint_t)arg.value, bufsz - 1, buf, NULL);
    buf[copied] = '\0';
    return (copied > 0);
}

/* ------------------------------------------------------------------ */
/* Pre-syscall event                                                   */
/* ------------------------------------------------------------------ */

static bool
event_pre_syscall(void *drcontext, int sysnum)
{
    char path_buf[4096];

    /* Check extra_blocked list first */
    for (int i = 0; i < g_policy.num_extra_blocked; i++) {
        if (sysnum == g_policy.extra_blocked[i]) {
            char reason[64];
            dr_snprintf(reason, sizeof(reason), "syscall %d blocked (extra_blocked)", sysnum);
            block_syscall(drcontext, NULL, reason);
            return false;
        }
    }

    switch (sysnum) {

    /* ---- Network ---- */
    case SYS_socket:
        if (!g_policy.block_network) return true;
        block_syscall(drcontext, NULL, "socket() network syscall blocked");
        return false;
    case SYS_connect:
        if (!g_policy.block_network) return true;
        block_syscall(drcontext, NULL, "connect() network syscall blocked");
        return false;
    case SYS_bind:
        if (!g_policy.block_network) return true;
        block_syscall(drcontext, NULL, "bind() network syscall blocked");
        return false;
    case SYS_listen:
        if (!g_policy.block_network) return true;
        block_syscall(drcontext, NULL, "listen() network syscall blocked");
        return false;
    case SYS_sendto:
        if (!g_policy.block_network) return true;
        block_syscall(drcontext, NULL, "sendto() network syscall blocked");
        return false;
    case SYS_sendmsg:
        if (!g_policy.block_network) return true;
        block_syscall(drcontext, NULL, "sendmsg() network syscall blocked");
        return false;
    case SYS_recvfrom:
        if (!g_policy.block_network) return true;
        block_syscall(drcontext, NULL, "recvfrom() network syscall blocked");
        return false;
    case SYS_recvmsg:
        if (!g_policy.block_network) return true;
        block_syscall(drcontext, NULL, "recvmsg() network syscall blocked");
        return false;

    /* ---- Process spawning ---- */
    case SYS_fork:
        if (!g_policy.block_exec) return true;
        block_syscall(drcontext, NULL, "fork() process spawn blocked");
        return false;
    case SYS_vfork:
        if (!g_policy.block_exec) return true;
        block_syscall(drcontext, NULL, "vfork() process spawn blocked");
        return false;
    case SYS_clone: {
        if (!g_policy.block_exec) return true;
        /* Allow clone when CLONE_THREAD is set (thread creation); block otherwise. */
        drsyscall_arg_t flags_arg;
        if (drsyscall_get_arg(drcontext, 0, &flags_arg) == DRMF_SUCCESS) {
            unsigned long flags = (unsigned long)flags_arg.value;
            if (!(flags & CLONE_THREAD)) {
                block_syscall(drcontext, NULL,
                              "clone() without CLONE_THREAD (process spawn) blocked");
                return false;
            }
        }
        return true;
    }
    case SYS_execve:
        if (!g_policy.block_exec) return true;
        block_syscall(drcontext, NULL, "execve() process spawn blocked");
        return false;
    case SYS_execveat:
        if (!g_policy.block_exec) return true;
        block_syscall(drcontext, NULL, "execveat() process spawn blocked");
        return false;

    /* ---- Dangerous ops ---- */
    case SYS_ptrace:
        if (!g_policy.block_ptrace) return true;
        block_syscall(drcontext, NULL, "ptrace() blocked");
        return false;
    case SYS_mount:
        if (!g_policy.block_ptrace) return true;
        block_syscall(drcontext, NULL, "mount() blocked");
        return false;
    case SYS_chroot:
        if (!g_policy.block_ptrace) return true;
        block_syscall(drcontext, NULL, "chroot() blocked");
        return false;
    case SYS_pivot_root:
        if (!g_policy.block_ptrace) return true;
        block_syscall(drcontext, NULL, "pivot_root() blocked");
        return false;
    case SYS_unshare:
        if (!g_policy.block_ptrace) return true;
        block_syscall(drcontext, NULL, "unshare() blocked");
        return false;
    case SYS_setns:
        if (!g_policy.block_ptrace) return true;
        block_syscall(drcontext, NULL, "setns() blocked");
        return false;

    /* ---- RWX mmap ---- */
    case SYS_mmap: {
        if (!g_policy.block_rwx) return true;
        drsyscall_arg_t prot_arg;
        if (drsyscall_get_arg(drcontext, 2, &prot_arg) == DRMF_SUCCESS) {
            int prot = (int)prot_arg.value;
            if ((prot & PROT_WRITE) && (prot & PROT_EXEC)) {
                block_syscall(drcontext, NULL,
                              "mmap() with PROT_WRITE|PROT_EXEC blocked");
                return false;
            }
        }
        return true;
    }

    /* ---- File path enforcement (open) ---- */
    case SYS_open: {
        if (g_policy.num_allowed_paths == 0)
            return true;
        if (get_str_arg(drcontext, 0, path_buf, sizeof(path_buf))) {
            if (!path_is_allowed(path_buf)) {
                char reason[4200];
                dr_snprintf(reason, sizeof(reason),
                            "open(\"%s\") path not in allowed_paths", path_buf);
                block_syscall(drcontext, NULL, reason);
                return false;
            }
        }
        return true;
    }
    case SYS_openat: {
        if (g_policy.num_allowed_paths == 0)
            return true;
        /* arg 0 = dirfd, arg 1 = pathname */
        if (get_str_arg(drcontext, 1, path_buf, sizeof(path_buf))) {
            if (!path_is_allowed(path_buf)) {
                char reason[4200];
                dr_snprintf(reason, sizeof(reason),
                            "openat(\"%s\") path not in allowed_paths", path_buf);
                block_syscall(drcontext, NULL, reason);
                return false;
            }
        }
        return true;
    }

    default:
        return true;
    }
}

/* ------------------------------------------------------------------ */
/* DynamoRIO client init/exit                                          */
/* ------------------------------------------------------------------ */

DR_EXPORT void
dr_client_main(client_id_t id, int argc, const char *argv[])
{
    /* Parse policy flags from argv; defaults are already set in g_policy initializer. */
    parse_policy_args(argc, argv);

    drmgr_init();
    drsyscall_init();

    drsyscall_register_presyscall_event(event_pre_syscall);

    dr_fprintf(STDERR,
               "[shimmy] filter loaded; network=%d exec=%d ptrace=%d rwx=%d "
               "allowed_paths=%d extra_blocked=%d\n",
               g_policy.block_network, g_policy.block_exec,
               g_policy.block_ptrace, g_policy.block_rwx,
               g_policy.num_allowed_paths, g_policy.num_extra_blocked);
}

DR_EXPORT void
dr_client_exit(void)
{
    drsyscall_exit();
    drmgr_exit();

    for (int i = 0; i < g_policy.num_allowed_paths; i++)
        free(g_policy.allowed_paths[i]);
    g_policy.num_allowed_paths = 0;
}
