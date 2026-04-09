/*
 * shimmy_filter.c — DynamoRIO syscall-filter client for shimmy-sandbox
 *
 * Blocks:
 *   - Network:   socket, connect, bind, listen, sendto, sendmsg, recvfrom, recvmsg
 *   - Spawning:  fork, vfork, clone (non-thread), execve, execveat
 *   - Dangerous: ptrace, mount, chroot, pivot_root, unshare, setns
 *   - RWX mmap:  mmap when PROT_WRITE|PROT_EXEC both set
 *   - File paths: open/openat restricted to comma-separated -allowed_paths whitelist
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
/* Allowed-path whitelist                                               */
/* ------------------------------------------------------------------ */
#define MAX_ALLOWED_PATHS 64

static char  *g_allowed_paths[MAX_ALLOWED_PATHS];
static int    g_num_allowed_paths = 0;

static void
parse_allowed_paths(const char *csv)
{
    if (!csv || csv[0] == '\0')
        return;

    char *copy = strdup(csv);
    char *tok  = strtok(copy, ",");
    while (tok && g_num_allowed_paths < MAX_ALLOWED_PATHS) {
        /* strip leading/trailing whitespace */
        while (*tok == ' ') tok++;
        char *end = tok + strlen(tok) - 1;
        while (end > tok && (*end == ' ' || *end == '\n')) { *end = '\0'; end--; }
        if (*tok != '\0') {
            g_allowed_paths[g_num_allowed_paths++] = strdup(tok);
        }
        tok = strtok(NULL, ",");
    }
    free(copy);
}

static bool
path_is_allowed(const char *path)
{
    if (!path)
        return false;
    /* If no whitelist configured, allow all paths. */
    if (g_num_allowed_paths == 0)
        return true;
    for (int i = 0; i < g_num_allowed_paths; i++) {
        if (strncmp(path, g_allowed_paths[i], strlen(g_allowed_paths[i])) == 0)
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

    switch (sysnum) {

    /* ---- Network ---- */
    case SYS_socket:
        block_syscall(drcontext, NULL, "socket() network syscall blocked");
        return false;
    case SYS_connect:
        block_syscall(drcontext, NULL, "connect() network syscall blocked");
        return false;
    case SYS_bind:
        block_syscall(drcontext, NULL, "bind() network syscall blocked");
        return false;
    case SYS_listen:
        block_syscall(drcontext, NULL, "listen() network syscall blocked");
        return false;
    case SYS_sendto:
        block_syscall(drcontext, NULL, "sendto() network syscall blocked");
        return false;
    case SYS_sendmsg:
        block_syscall(drcontext, NULL, "sendmsg() network syscall blocked");
        return false;
    case SYS_recvfrom:
        block_syscall(drcontext, NULL, "recvfrom() network syscall blocked");
        return false;
    case SYS_recvmsg:
        block_syscall(drcontext, NULL, "recvmsg() network syscall blocked");
        return false;

    /* ---- Process spawning ---- */
    case SYS_fork:
        block_syscall(drcontext, NULL, "fork() process spawn blocked");
        return false;
    case SYS_vfork:
        block_syscall(drcontext, NULL, "vfork() process spawn blocked");
        return false;
    case SYS_clone: {
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
        block_syscall(drcontext, NULL, "execve() process spawn blocked");
        return false;
    case SYS_execveat:
        block_syscall(drcontext, NULL, "execveat() process spawn blocked");
        return false;

    /* ---- Dangerous ops ---- */
    case SYS_ptrace:
        block_syscall(drcontext, NULL, "ptrace() blocked");
        return false;
    case SYS_mount:
        block_syscall(drcontext, NULL, "mount() blocked");
        return false;
    case SYS_chroot:
        block_syscall(drcontext, NULL, "chroot() blocked");
        return false;
    case SYS_pivot_root:
        block_syscall(drcontext, NULL, "pivot_root() blocked");
        return false;
    case SYS_unshare:
        block_syscall(drcontext, NULL, "unshare() blocked");
        return false;
    case SYS_setns:
        block_syscall(drcontext, NULL, "setns() blocked");
        return false;

    /* ---- RWX mmap ---- */
    case SYS_mmap: {
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
        if (g_num_allowed_paths == 0)
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
        if (g_num_allowed_paths == 0)
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
    /* Parse -allowed_paths <csv> from argv */
    for (int i = 1; i < argc - 1; i++) {
        if (strcmp(argv[i], "-allowed_paths") == 0) {
            parse_allowed_paths(argv[i + 1]);
            i++; /* skip value */
        }
    }

    drmgr_init();
    drsyscall_init();

    drsyscall_register_presyscall_event(event_pre_syscall);

    dr_fprintf(STDERR, "[shimmy] filter loaded; %d allowed path prefix(es)\n",
               g_num_allowed_paths);
}

DR_EXPORT void
dr_client_exit(void)
{
    drsyscall_exit();
    drmgr_exit();

    for (int i = 0; i < g_num_allowed_paths; i++)
        free(g_allowed_paths[i]);
    g_num_allowed_paths = 0;
}
