// common.h — Shared constants and event structures between BPF C and Go.
//
// The Go side reads these structs from the perf buffer. Field order, sizes,
// and padding MUST stay in sync with the Go counterpart in ebpf.go.
// When changing this file, always update the Go processEvent struct to match.

#ifndef __COMMON_H__
#define __COMMON_H__

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

// ----- Constants -----

#define TASK_COMM_LEN  16
#define MAX_FILENAME   256
#define MAX_CMDLINE    512

// Event types — match event.EventType constants in Go.
#define EVENT_PROCESS_EXEC  1
#define EVENT_PROCESS_EXIT  2

// ----- Event structures -----

// process_event is emitted for both exec and exit events.
// For exec: all fields populated (filename, cmdline, comm).
// For exit: only pid/tgid/ppid/exit_code/start_ts meaningful.
struct process_event {
	__u8   event_type;               // EVENT_PROCESS_EXEC or EVENT_PROCESS_EXIT
	__u8   _pad[3];                  // alignment padding
	__u32  pid;                      // kernel pid (= userspace tid)
	__u32  tgid;                     // kernel tgid (= userspace pid)
	__u32  ppid;                     // parent tgid
	__u32  uid;
	__u32  gid;
	__s32  exit_code;                // only valid for exit events
	__u64  start_ts;                 // ktime_ns at event time
	__u8   in_container;             // 1 if pid_ns level > 0
	__u8   _pad2[7];                // alignment padding
	char   comm[TASK_COMM_LEN];      // 16 bytes
	char   filename[MAX_FILENAME];   // 256 bytes — exec path (exec only)
	char   cmdline[MAX_CMDLINE];     // 512 bytes — /proc/<pid>/cmdline (exec only)
};

// ----- BPF Maps -----

// Perf event array for kernel → userspace event delivery.
// Using perf buffer (not ring buffer) for kernel 5.4+ compatibility.
// Ring buffer requires kernel >= 5.8.
struct {
	__uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
	__uint(key_size, sizeof(__u32));
	__uint(value_size, sizeof(__u32));
} events SEC(".maps");

// Per-CPU scratch buffer for building events without exceeding the 512-byte
// BPF stack limit. process_event is ~812 bytes — way over the limit.
// Each BPF program does: lookup index 0 → fill fields → perf_event_output.
struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, struct process_event);
} event_scratch SEC(".maps");

// PID whitelist — LRU hash, keyed by tgid.
// When a PID is in this map, BPF programs skip it immediately.
// Populated from userspace (Go side) based on Agent config.
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 4096);
	__type(key, __u32);       // tgid
	__type(value, __u8);      // dummy value, presence = whitelisted
} whitelist_pids SEC(".maps");

// ----- Helpers -----

// get_event_buf returns a pointer to the per-CPU scratch buffer for building events.
// Returns NULL if the map lookup fails (should never happen).
// We zero only the header fields (not the large char arrays) because:
//   - Callers write filename/cmdline via bpf_probe_read which sets the data
//   - Exit events don't use filename/cmdline at all
//   - Avoiding __builtin_memset on 812 bytes prevents BPF instruction limit issues
static __always_inline struct process_event *get_event_buf(void) {
	__u32 zero = 0;
	struct process_event *evt = bpf_map_lookup_elem(&event_scratch, &zero);
	if (!evt)
		return NULL;

	// Zero header fields explicitly (avoids __builtin_memset on large struct)
	evt->event_type = 0;
	evt->pid = 0;
	evt->tgid = 0;
	evt->ppid = 0;
	evt->uid = 0;
	evt->gid = 0;
	evt->exit_code = 0;
	evt->start_ts = 0;
	evt->in_container = 0;
	evt->comm[0] = 0;
	evt->filename[0] = 0;
	evt->cmdline[0] = 0;

	return evt;
}

// Check if a tgid is whitelisted. Returns 1 if whitelisted (should skip).
static __always_inline int is_whitelisted(__u32 tgid) {
	return bpf_map_lookup_elem(&whitelist_pids, &tgid) != NULL;
}

#endif /* __COMMON_H__ */
