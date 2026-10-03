// Unix-Monitor eBPF probe (CO-RE). Covers process/file/net/memory/kernel
// syscalls via a single tracepoint/raw_syscalls/sys_enter dispatcher, plus
// dedicated tracepoints for kernel module loads and block-device IO, and a
// couple of uprobes in the style of gojue/ecapture for application-level
// plaintext (DNS resolution, TLS payloads, Postgres queries).
//
// NOTE: syscall numbers below are x86_64-specific. The Go side only loads
// this program on linux/amd64 and falls back to the polling collector
// everywhere else (see internal/collector). Porting to arm64 would need a
// second syscall-number table.
#include "vmlinux.h"
#include "libbpf/bpf_helpers.h"
#include "libbpf/bpf_tracing.h"
#include "libbpf/bpf_core_read.h"

char LICENSE[] SEC("license") = "Dual MIT/GPL";

#define TASK_COMM_LEN 16
#define STR_LEN 256

// x86_64 syscall numbers we care about.
#define NR_read 0
#define NR_open 2
#define NR_mmap 9
#define NR_mprotect 10
#define NR_munmap 11
#define NR_brk 12
#define NR_connect 42
#define NR_bind 49
#define NR_listen 50
#define NR_execve 59
#define NR_exit 60
#define NR_kill 62
#define NR_rename 82
#define NR_unlink 87
#define NR_ptrace 101
#define NR_setuid 105
#define NR_exit_group 231
#define NR_openat 257
#define NR_unlinkat 263
#define NR_renameat 264
#define NR_process_vm_readv 310
#define NR_process_vm_writev 311
#define NR_renameat2 316
#define NR_execveat 322

enum ev_kind {
	EV_EXEC = 1,
	EV_EXIT,
	EV_OPEN,
	EV_UNLINK,
	EV_RENAME,
	EV_CONNECT,
	EV_LISTEN,
	EV_MMAP,
	EV_MPROTECT,
	EV_MUNMAP,
	EV_BRK,
	EV_PTRACE,
	EV_VM_READ,
	EV_VM_WRITE,
	EV_SETUID,
	EV_KILL,
	EV_MODULE_LOAD,
	EV_BLOCK_RQ,
	EV_DNS,
	EV_TLS,
	EV_SQL,
};

// Fixed-size event shipped to userspace via the ring buffer. Numeric
// syscall arguments go in arg[]; their meaning depends on `kind` and is
// documented in internal/collector/decode_linux.go next to the Go struct
// that mirrors this one byte-for-byte.
struct event {
	__u64 ts_ns;
	__u32 kind;
	__u32 pid;  // tgid
	__u32 tid;
	__u32 ppid;
	__u32 uid;
	char comm[TASK_COMM_LEN];
	__s64 arg[6];
	__u16 str1_len;
	__u16 str2_len;
	char str1[STR_LEN];
	char str2[STR_LEN];
};

// BPF_MAP_TYPE_RINGBUF (the obvious choice) doesn't exist on every kernel
// we need to run on: it's a 5.8+ type, and at least one BTF-enabled,
// CO-RE-capable vendor kernel we've hit in the field (5.4.265, a patched
// automotive kernel with real BTF but an older map-type set) rejects its
// creation outright (EINVAL) because the runtime never backported it -
// not a BTF/CO-RE encoding issue, the map type genuinely isn't implemented.
// perf_event_array has been there since kernel ~4.3 and needs nothing this
// project doesn't already have, so it's the one path that actually runs
// everywhere CO-RE does. See bind_perf_event_array's own comment for why
// a larger, lock-free ring loses out to "runs on the kernel in front of
// us" here.
struct {
	__uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
	__uint(key_size, sizeof(__u32));
	__uint(value_size, sizeof(__u32));
	// max_entries deliberately left unset: cilium/ebpf resizes a
	// PerfEventArray to the host's possible-CPU count at load time.
} events SEC(".maps");

// struct event is >512B (two 256B string fields alone), over the BPF stack
// limit, so it can't live on the stack between "reserve" and "submit" the
// way a ringbuf record can. One per-CPU scratch slot holds the
// in-progress event instead; per-CPU is the key part - two tracepoints
// firing on different CPUs each get their own slot, a tracepoint firing
// on the same CPU that's still using its slot can't happen (BPF programs
// run with preemption/IRQs handled such that this one never reenters
// itself on one CPU), which is the same assumption every eBPF tool using
// this pattern for over-sized records relies on.
struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, struct event);
} event_scratch SEC(".maps");

struct cfg {
	__u32 self_pid;
};
struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, struct cfg);
} monitor_cfg SEC(".maps");

// (tgid<<32|fd) -> last bind() sockaddr, so a later listen() can report the
// port it was bound to (listen(2) itself only takes a backlog argument).
struct bind_addr {
	__u8 buf[28];
	__u16 len;
};
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 4096);
	__type(key, __u64);
	__type(value, struct bind_addr);
} bind_addrs SEC(".maps");

// tid -> SSL_read's buf pointer, stashed at probe entry and consumed by the
// matching uretprobe once the buffer has been filled.
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 1024);
	__type(key, __u64);
	__type(value, __u64);
} ssl_read_bufs SEC(".maps");

static __always_inline bool is_self(__u32 tgid) {
	__u32 key = 0;
	struct cfg *c = bpf_map_lookup_elem(&monitor_cfg, &key);
	return c && c->self_pid != 0 && c->self_pid == tgid;
}

// reserve_event/submit_event replace ringbuf's reserve/submit pair for the
// perf_event_array path: "reserve" is a lookup into this CPU's scratch
// slot (never fails once the map itself exists), "submit" is
// bpf_perf_event_output, which - unlike bpf_ringbuf_submit - needs the
// program's own ctx, so every call site below passes its own `ctx`
// through unchanged.
static __always_inline struct event *reserve_event(void) {
	__u32 key = 0;
	return bpf_map_lookup_elem(&event_scratch, &key);
}

static __always_inline void submit_event(void *ctx, struct event *e) {
	bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, e, sizeof(*e));
}

static __always_inline void fill_common(struct event *e, __u32 kind) {
	__u64 pidtgid = bpf_get_current_pid_tgid();
	e->ts_ns = bpf_ktime_get_ns();
	e->kind = kind;
	e->pid = pidtgid >> 32;
	e->tid = (__u32)pidtgid;
	e->uid = bpf_get_current_uid_gid() & 0xffffffff;
	bpf_get_current_comm(&e->comm, sizeof(e->comm));
	struct task_struct *task = (struct task_struct *)bpf_get_current_task();
	e->ppid = BPF_CORE_READ(task, real_parent, tgid);
}

static __always_inline void read_data_loc_str(void *ctx, __u32 loc, char *dst, int dstlen) {
	__u16 offset = loc & 0xFFFF;
	bpf_probe_read_kernel_str(dst, dstlen, (char *)ctx + offset);
}

// ---------------------------------------------------------------------------
// Main syscall dispatcher.
// ---------------------------------------------------------------------------
SEC("tracepoint/raw_syscalls/sys_enter")
int handle_sys_enter(struct trace_event_raw_sys_enter *ctx) {
	__u64 pidtgid = bpf_get_current_pid_tgid();
	__u32 tgid = pidtgid >> 32;
	if (is_self(tgid))
		return 0;

	long id = ctx->id;
	// raw_syscalls:sys_enter's args[6] is an array field; direct ctx->args[N]
	// dereferences trip the verifier's ctx-access check ("modified ctx
	// ptr") on this kernel/clang combination, so copy it out via a helper
	// (which is allowed to take a ctx-derived pointer as an argument) and
	// index the local copy instead.
	unsigned long args[6] = {};
	bpf_probe_read_kernel(args, sizeof(args), (void *)&ctx->args);
	__u32 kind;

	switch (id) {
	case NR_execve:
	case NR_execveat: {
		struct event *e = reserve_event();
		if (!e) return 0;
		fill_common(e, EV_EXEC);
		const char *path = (const char *)(id == NR_execve ? args[0] : args[1]);
		e->str1_len = bpf_probe_read_user_str(e->str1, STR_LEN, path);

		// str1 above is the resolved executable (e.g. "/usr/bin/git");
		// argv is what was actually typed (e.g. "git commit -m fix"),
		// which is the command line the UI shows. Captured into 6 fixed
		// 42-byte slots (6*42=252 <= STR_LEN) rather than one packed,
		// dynamically-offset string: every bpf_probe_read_user_str
		// destination below is a compile-time-constant offset into
		// e->str2, which the verifier can check statically - a
		// runtime-computed running offset into a 256B buffer is exactly
		// the kind of pointer arithmetic it tends to reject. The nested
		// ifs are an unrolled "stop at the first NULL argv slot" loop;
		// decode_linux.go reads each slot back with cstr(), which
		// already stops at the first NUL on its own.
		const char *const *argv = (const char *const *)(id == NR_execve ? args[1] : args[2]);
		__u16 argc = 0;
		const char *a0 = NULL; bpf_probe_read_user(&a0, sizeof(a0), &argv[0]);
		if (a0) {
			bpf_probe_read_user_str(e->str2 + 0, 42, a0); argc = 1;
			const char *a1 = NULL; bpf_probe_read_user(&a1, sizeof(a1), &argv[1]);
			if (a1) {
				bpf_probe_read_user_str(e->str2 + 42, 42, a1); argc = 2;
				const char *a2 = NULL; bpf_probe_read_user(&a2, sizeof(a2), &argv[2]);
				if (a2) {
					bpf_probe_read_user_str(e->str2 + 84, 42, a2); argc = 3;
					const char *a3 = NULL; bpf_probe_read_user(&a3, sizeof(a3), &argv[3]);
					if (a3) {
						bpf_probe_read_user_str(e->str2 + 126, 42, a3); argc = 4;
						const char *a4 = NULL; bpf_probe_read_user(&a4, sizeof(a4), &argv[4]);
						if (a4) {
							bpf_probe_read_user_str(e->str2 + 168, 42, a4); argc = 5;
							const char *a5 = NULL; bpf_probe_read_user(&a5, sizeof(a5), &argv[5]);
							if (a5) {
								bpf_probe_read_user_str(e->str2 + 210, 42, a5); argc = 6;
							}
						}
					}
				}
			}
		}
		e->str2_len = argc; // slot count, not a byte length - see decode_linux.go
		submit_event(ctx, e);
		return 0;
	}
	case NR_exit:
	case NR_exit_group: {
		struct event *e = reserve_event();
		if (!e) return 0;
		fill_common(e, EV_EXIT);
		e->arg[0] = args[0];
		e->str1_len = 0; e->str2_len = 0;
		submit_event(ctx, e);
		return 0;
	}
	case NR_open:
	case NR_openat: {
		struct event *e = reserve_event();
		if (!e) return 0;
		fill_common(e, EV_OPEN);
		const char *path = (const char *)(id == NR_open ? args[0] : args[1]);
		e->arg[0] = id == NR_open ? (long)args[1] : (long)args[2]; // flags
		e->str1_len = bpf_probe_read_user_str(e->str1, STR_LEN, path);
		e->str2_len = 0;
		submit_event(ctx, e);
		return 0;
	}
	case NR_unlink:
	case NR_unlinkat: {
		struct event *e = reserve_event();
		if (!e) return 0;
		fill_common(e, EV_UNLINK);
		const char *path = (const char *)(id == NR_unlink ? args[0] : args[1]);
		e->str1_len = bpf_probe_read_user_str(e->str1, STR_LEN, path);
		e->str2_len = 0;
		submit_event(ctx, e);
		return 0;
	}
	case NR_rename:
	case NR_renameat:
	case NR_renameat2: {
		struct event *e = reserve_event();
		if (!e) return 0;
		fill_common(e, EV_RENAME);
		const char *oldp, *newp;
		if (id == NR_rename) { oldp = (const char *)args[0]; newp = (const char *)args[1]; }
		else { oldp = (const char *)args[1]; newp = (const char *)args[3]; }
		e->str1_len = bpf_probe_read_user_str(e->str1, STR_LEN, oldp);
		e->str2_len = bpf_probe_read_user_str(e->str2, STR_LEN, newp);
		submit_event(ctx, e);
		return 0;
	}
	case NR_connect:
	case NR_bind: {
		struct event *e = reserve_event();
		if (!e) return 0;
		// bind(2) itself is never shipped as an event; it's only stashed so a
		// later listen(2) on the same fd can report the port it bound to.
		fill_common(e, id == NR_connect ? EV_CONNECT : 0);
		e->arg[0] = args[0]; // fd
		__u64 alen = args[2]; if (alen > sizeof(((struct bind_addr *)0)->buf)) alen = sizeof(((struct bind_addr *)0)->buf);
		e->str1_len = bpf_probe_read_user(e->str1, alen, (void *)args[1]) == 0 ? alen : 0;
		if (id == NR_bind) {
			struct bind_addr ba = {};
			ba.len = e->str1_len;
			__builtin_memcpy(ba.buf, e->str1, sizeof(ba.buf));
			__u64 k = ((__u64)tgid << 32) | (__u32)args[0];
			bpf_map_update_elem(&bind_addrs, &k, &ba, BPF_ANY);
			// Nothing to discard here (unlike ringbuf's reserve/discard
			// pair): the scratch slot above was never queued anywhere,
			// so just not calling submit_event is the whole story.
		} else {
			submit_event(ctx, e);
		}
		return 0;
	}
	case NR_listen: {
		struct event *e = reserve_event();
		if (!e) return 0;
		fill_common(e, EV_LISTEN);
		e->arg[0] = args[0]; // fd
		__u64 k = ((__u64)tgid << 32) | (__u32)args[0];
		struct bind_addr *ba = bpf_map_lookup_elem(&bind_addrs, &k);
		if (ba) { __builtin_memcpy(e->str1, ba->buf, sizeof(ba->buf)); e->str1_len = ba->len; }
		else e->str1_len = 0;
		e->str2_len = 0;
		submit_event(ctx, e);
		return 0;
	}
	case NR_mmap: {
		struct event *e = reserve_event();
		if (!e) return 0;
		fill_common(e, EV_MMAP);
		e->arg[0] = args[0]; e->arg[1] = args[1]; e->arg[2] = args[2];
		e->arg[3] = args[3]; e->arg[4] = args[4]; e->arg[5] = args[5];
		e->str1_len = 0; e->str2_len = 0;
		submit_event(ctx, e);
		return 0;
	}
	case NR_mprotect: {
		struct event *e = reserve_event();
		if (!e) return 0;
		fill_common(e, EV_MPROTECT);
		e->arg[0] = args[0]; e->arg[1] = args[1]; e->arg[2] = args[2];
		e->str1_len = 0; e->str2_len = 0;
		submit_event(ctx, e);
		return 0;
	}
	case NR_munmap: {
		struct event *e = reserve_event();
		if (!e) return 0;
		fill_common(e, EV_MUNMAP);
		e->arg[0] = args[0]; e->arg[1] = args[1];
		e->str1_len = 0; e->str2_len = 0;
		submit_event(ctx, e);
		return 0;
	}
	case NR_brk: {
		struct event *e = reserve_event();
		if (!e) return 0;
		fill_common(e, EV_BRK);
		e->arg[0] = args[0];
		e->str1_len = 0; e->str2_len = 0;
		submit_event(ctx, e);
		return 0;
	}
	case NR_ptrace: {
		struct event *e = reserve_event();
		if (!e) return 0;
		fill_common(e, EV_PTRACE);
		e->arg[0] = args[0]; e->arg[1] = args[1]; e->arg[2] = args[2]; e->arg[3] = args[3];
		e->str1_len = 0; e->str2_len = 0;
		submit_event(ctx, e);
		return 0;
	}
	case NR_process_vm_readv:
	case NR_process_vm_writev: {
		struct event *e = reserve_event();
		if (!e) return 0;
		fill_common(e, id == NR_process_vm_readv ? EV_VM_READ : EV_VM_WRITE);
		e->arg[0] = args[0]; // target pid
		e->str1_len = 0; e->str2_len = 0;
		submit_event(ctx, e);
		return 0;
	}
	case NR_setuid: {
		struct event *e = reserve_event();
		if (!e) return 0;
		fill_common(e, EV_SETUID);
		e->arg[0] = bpf_get_current_uid_gid() & 0xffffffff; // from
		e->arg[1] = args[0]; // to
		e->str1_len = 0; e->str2_len = 0;
		submit_event(ctx, e);
		return 0;
	}
	case NR_kill: {
		struct event *e = reserve_event();
		if (!e) return 0;
		fill_common(e, EV_KILL);
		e->arg[0] = args[0]; e->arg[1] = args[1];
		e->str1_len = 0; e->str2_len = 0;
		submit_event(ctx, e);
		return 0;
	}
	default:
		return 0;
	}
	(void)kind;
	return 0;
}

// ---------------------------------------------------------------------------
// Kernel module loads (name only available once resolved, not at the
// init_module/finit_module syscall entry).
// ---------------------------------------------------------------------------
SEC("tracepoint/module/module_load")
int handle_module_load(struct trace_event_raw_module_load *ctx) {
	struct event *e = reserve_event();
	if (!e) return 0;
	fill_common(e, EV_MODULE_LOAD);
	read_data_loc_str(ctx, ctx->__data_loc_name, e->str1, STR_LEN);
	e->str1_len = 1; e->str2_len = 0;
	submit_event(ctx, e);
	return 0;
}

// ---------------------------------------------------------------------------
// Block device IO (for the disk-sector scatter view).
// ---------------------------------------------------------------------------
SEC("tracepoint/block/block_rq_issue")
int handle_block_rq_issue(struct trace_event_raw_block_rq *ctx) {
	struct event *e = reserve_event();
	if (!e) return 0;
	fill_common(e, EV_BLOCK_RQ);
	// Note: fill_common's comm is the *current* task, which for block IO
	// is often a kworker/writeback thread rather than the app that
	// originally issued the write; ctx->comm (the real issuer, recorded
	// when the request was built) would be more accurate, but copying an
	// array field out of a tracepoint ctx trips the verifier's ctx-access
	// checks, so we accept the current-task comm as a simplification.
	e->arg[0] = ctx->dev;
	e->arg[1] = ctx->sector;
	e->arg[2] = ctx->bytes;
	// rwbs[0] is 'R' for read, 'W' for write (see Documentation/block/biodoc).
	e->arg[3] = ctx->rwbs[0];
	e->str1_len = 0; e->str2_len = 0;
	submit_event(ctx, e);
	return 0;
}

// ---------------------------------------------------------------------------
// Application-level plaintext, eCapture-style uprobes. Attached from Go
// only when the target library/binary is found on disk (see
// internal/collector/uprobes_linux.go); the SEC name is just a label here.
// ---------------------------------------------------------------------------

// getaddrinfo(const char *node, ...) in libc -> DNS query name.
SEC("uprobe/getaddrinfo")
int uprobe_getaddrinfo(struct pt_regs *ctx) {
	struct event *e = reserve_event();
	if (!e) return 0;
	fill_common(e, EV_DNS);
	const char *node = (const char *)PT_REGS_PARM1(ctx);
	e->str1_len = bpf_probe_read_user_str(e->str1, STR_LEN, node);
	e->str2_len = 0;
	submit_event(ctx, e);
	return 0;
}

// SSL_write(SSL *ssl, const void *buf, int num) -> outgoing TLS plaintext.
SEC("uprobe/ssl_write")
int uprobe_ssl_write(struct pt_regs *ctx) {
	struct event *e = reserve_event();
	if (!e) return 0;
	fill_common(e, EV_TLS);
	e->arg[0] = 1; // direction: send
	const char *buf = (const char *)PT_REGS_PARM2(ctx);
	long num = (long)PT_REGS_PARM3(ctx);
	if (num > STR_LEN - 1) num = STR_LEN - 1;
	if (num < 0) num = 0;
	e->str1_len = bpf_probe_read_user(e->str1, num, buf) == 0 ? num : 0;
	e->str2_len = 0;
	submit_event(ctx, e);
	return 0;
}

// SSL_read(SSL *ssl, void *buf, int num) returning -> incoming TLS plaintext.
// We read the buffer on the *return* probe, once SSL_read has filled it.
SEC("uprobe/ssl_read_enter")
int uprobe_ssl_read_enter(struct pt_regs *ctx) {
	// Stash buf pointer keyed by tid so the return probe can read it back.
	__u64 tid = bpf_get_current_pid_tgid();
	__u64 buf = PT_REGS_PARM2(ctx);
	bpf_map_update_elem(&ssl_read_bufs, &tid, &buf, BPF_ANY);
	return 0;
}
SEC("uretprobe/ssl_read_ret")
int uretprobe_ssl_read_ret(struct pt_regs *ctx) {
	__u64 tid = bpf_get_current_pid_tgid();
	__u64 *buf = bpf_map_lookup_elem(&ssl_read_bufs, &tid);
	long n = (long)PT_REGS_RC(ctx);
	if (!buf || n <= 0) return 0;
	struct event *e = reserve_event();
	if (!e) return 0;
	fill_common(e, EV_TLS);
	e->arg[0] = 0; // direction: recv
	if (n > STR_LEN - 1) n = STR_LEN - 1;
	e->str1_len = bpf_probe_read_user(e->str1, n, (void *)*buf) == 0 ? n : 0;
	e->str2_len = 0;
	submit_event(ctx, e);
	bpf_map_delete_elem(&ssl_read_bufs, &tid);
	return 0;
}

// Postgres exec_simple_query(const char *query_string) -> SQL text.
SEC("uprobe/pg_query")
int uprobe_pg_query(struct pt_regs *ctx) {
	struct event *e = reserve_event();
	if (!e) return 0;
	fill_common(e, EV_SQL);
	const char *q = (const char *)PT_REGS_PARM1(ctx);
	e->str1_len = bpf_probe_read_user_str(e->str1, STR_LEN, q);
	e->str2_len = 0;
	submit_event(ctx, e);
	return 0;
}
