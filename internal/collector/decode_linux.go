//go:build linux && amd64

package collector

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"argusbpf/internal/event"
	"argusbpf/internal/sysinfo"
)

// rawEvent mirrors `struct event` in bpf/monitor.c byte-for-byte on amd64:
// Go's natural field alignment matches the C compiler's on this
// architecture, so a raw perf_event_array record's RawSample can be
// reinterpreted directly via unsafe.Pointer without an explicit codec.
type rawEvent struct {
	TsNs    uint64
	Kind    uint32
	Pid     uint32
	Tid     uint32
	Ppid    uint32
	Uid     uint32
	Comm    [16]byte
	Arg     [6]int64
	Str1Len uint16
	Str2Len uint16
	Str1    [256]byte
	Str2    [256]byte
}

// eBPF event kinds, mirroring `enum ev_kind` in bpf/monitor.c.
const (
	evExec = 1 + iota
	evExit
	evOpen
	evUnlink
	evRename
	evConnect
	evListen
	evMmap
	evMprotect
	evMunmap
	evBrk
	evPtrace
	evVMRead
	evVMWrite
	evSetuid
	evKill
	evModuleLoad
	evBlockRq
	evDNS
	evTLS
	evSQL
)

func decodeRaw(b []byte) (*rawEvent, bool) {
	if len(b) < int(unsafe.Sizeof(rawEvent{})) {
		return nil, false
	}
	return (*rawEvent)(unsafe.Pointer(&b[0])), true
}

func cstr(b []byte, n uint16) string {
	if int(n) > len(b) {
		n = uint16(len(b))
	}
	s := b[:n]
	if i := indexZero(s); i >= 0 {
		s = s[:i]
	}
	return string(s)
}
func indexZero(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}

var userCache sync.Map // uid -> name

func uidName(uid uint32) string {
	if v, ok := userCache.Load(uid); ok {
		return v.(string)
	}
	name := fmt.Sprintf("uid:%d", uid)
	if u, err := user.LookupId(fmt.Sprint(uid)); err == nil {
		name = u.Username
	}
	userCache.Store(uid, name)
	return name
}

// resolveCwdPath turns a possibly-relative path from a syscall argument
// into an absolute one using the calling process's cwd, best-effort.
func resolveCwdPath(pid uint32, path string) string {
	if path == "" || strings.HasPrefix(path, "/") {
		return path
	}
	if link, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid)); err == nil {
		return filepath.Join(link, path)
	}
	return path
}

// toEvent converts a decoded rawEvent into the shared event.Event model.
// Returns ok=false for kinds that produce no user-visible event (e.g. a
// stashed bind() with no corresponding listen() yet).
func toEvent(r *rawEvent) (*event.Event, bool) {
	comm := cstr(r.Comm[:], uint16(len(r.Comm)))
	ev := &event.Event{
		TS: time.Now().UnixMilli(),
		PID: int32(r.Pid), PPID: int32(r.Ppid), TID: int32(r.Tid),
		UID: r.Uid, User: uidName(r.Uid), Comm: comm,
	}
	switch r.Kind {
	case evExec:
		path := resolveCwdPath(r.Pid, cstr(r.Str1[:], r.Str1Len))
		ev.Cat, ev.Type, ev.Exe = event.CatProcess, "exec", path
		ev.SetField("path", path)
	case evExit:
		ev.Cat, ev.Type = event.CatProcess, "exit"
		ev.SetField("code", r.Arg[0])
	case evOpen:
		path := resolveCwdPath(r.Pid, cstr(r.Str1[:], r.Str1Len))
		flags := r.Arg[0]
		typ := "open"
		if flags&int64(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
			typ = "write"
		}
		ev.Cat, ev.Type = event.CatFile, typ
		ev.SetField("path", path)
		ev.SetField("flags", fmt.Sprintf("0x%x", flags))
	case evUnlink:
		ev.Cat, ev.Type = event.CatFile, "unlink"
		ev.SetField("path", resolveCwdPath(r.Pid, cstr(r.Str1[:], r.Str1Len)))
	case evRename:
		ev.Cat, ev.Type = event.CatFile, "rename"
		ev.SetField("path", resolveCwdPath(r.Pid, cstr(r.Str1[:], r.Str1Len)))
		ev.SetField("dst", resolveCwdPath(r.Pid, cstr(r.Str2[:], r.Str2Len)))
	case evConnect:
		ev.Cat, ev.Type = event.CatNet, "connect"
		host, port := decodeSockaddr(r.Str1[:], r.Str1Len)
		ev.SetField("host", host)
		ev.SetField("port", fmt.Sprint(port))
		svcZh, svcEn := serviceGuess(port)
		ev.SetField("service", svcZh)
		ev.SetField("service_en", svcEn)
		maybeSSH(ev, comm, host, port, "out")
	case evListen:
		ev.Cat, ev.Type = event.CatNet, "listen"
		_, port := decodeSockaddr(r.Str1[:], r.Str1Len)
		ev.SetField("port", fmt.Sprint(port))
		svcZh, svcEn := serviceGuess(port)
		ev.SetField("service", svcZh)
		ev.SetField("service_en", svcEn)
	case evMmap:
		ev.Cat, ev.Type = event.CatMemory, "mmap"
		ev.SetField("addr", fmt.Sprintf("0x%x", uint64(r.Arg[0])))
		ev.SetField("size", fmt.Sprint(r.Arg[1]))
		ev.SetField("prot", protString(r.Arg[2]))
	case evMprotect:
		ev.Cat, ev.Type = event.CatMemory, "mprotect"
		ev.SetField("addr", fmt.Sprintf("0x%x", uint64(r.Arg[0])))
		ev.SetField("size", fmt.Sprint(r.Arg[1]))
		prot := r.Arg[2]
		ev.SetField("prot", protString(prot))
		if prot&2 != 0 && prot&4 != 0 { // PROT_WRITE|PROT_EXEC
			ev.SetField("perm_wx", "1")
		} else {
			ev.SetField("perm_wx", "0")
		}
	case evMunmap:
		ev.Cat, ev.Type = event.CatMemory, "munmap"
		ev.SetField("addr", fmt.Sprintf("0x%x", uint64(r.Arg[0])))
	case evBrk:
		ev.Cat, ev.Type = event.CatMemory, "brk"
		ev.SetField("addr", fmt.Sprintf("0x%x", uint64(r.Arg[0])))
	case evPtrace:
		ev.Cat, ev.Type = event.CatMemory, "ptrace"
		ev.SetField("target_pid", fmt.Sprint(r.Arg[1]))
	case evVMRead:
		ev.Cat, ev.Type = event.CatMemory, "vm_read"
		ev.SetField("target_pid", fmt.Sprint(r.Arg[0]))
	case evVMWrite:
		ev.Cat, ev.Type = event.CatMemory, "vm_write"
		ev.SetField("target_pid", fmt.Sprint(r.Arg[0]))
	case evSetuid:
		ev.Cat, ev.Type = event.CatProcess, "setuid"
		ev.SetField("from_uid", fmt.Sprint(r.Arg[0]))
		ev.SetField("to_uid", fmt.Sprint(r.Arg[1]))
	case evKill:
		ev.Cat, ev.Type = event.CatKernel, "kill"
		ev.SetField("target_pid", fmt.Sprint(r.Arg[0]))
		ev.SetField("signal", fmt.Sprint(r.Arg[1]))
	case evModuleLoad:
		ev.Cat, ev.Type = event.CatKernel, "module_load"
		ev.SetField("name", cstr(r.Str1[:], uint16(len(r.Str1))))
	case evBlockRq:
		ev.Cat = event.CatDisk
		if r.Arg[3] == 'W' {
			ev.Type = "write"
		} else {
			ev.Type = "read"
		}
		ev.SetField("dev", fmt.Sprintf("%d:%d", r.Arg[0]>>20, r.Arg[0]&0xfffff))
		ev.SetField("sector", fmt.Sprint(r.Arg[1]))
		ev.SetField("bytes", fmt.Sprint(r.Arg[2]))
	case evDNS:
		ev.Cat, ev.Type = event.CatNet, "dns"
		ev.SetField("name", cstr(r.Str1[:], r.Str1Len))
	case evTLS:
		ev.Cat, ev.Type = event.CatSecurity, "tls"
		dir := "recv"
		if r.Arg[0] == 1 {
			dir = "send"
		}
		ev.SetField("direction", dir)
		ev.SetField("payload", previewText(r.Str1[:], r.Str1Len))
	case evSQL:
		ev.Cat, ev.Type = event.CatSecurity, "sql"
		ev.SetField("sql", cstr(r.Str1[:], r.Str1Len))
	default:
		return nil, false
	}
	return ev, true
}

// previewText renders a byte preview as text, replacing non-printable
// bytes so binary TLS payloads don't break the UI/JSON.
func previewText(b []byte, n uint16) string {
	s := cstr(b, n)
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\n' || r == '\t' || (r >= 0x20 && r < 0x7f) {
			out = append(out, r)
		} else {
			out = append(out, '.')
		}
	}
	return string(out)
}

func protString(p int64) string {
	s := ""
	if p&1 != 0 {
		s += "R"
	}
	if p&2 != 0 {
		s += "W"
	}
	if p&4 != 0 {
		s += "X"
	}
	if s == "" {
		s = "NONE"
	}
	return s
}

// decodeSockaddr reads a raw struct sockaddr_in/sockaddr_in6 copied from
// userspace and returns (ip, port). family is read from the first 2 bytes
// (little-endian u16, matching the kernel's in-memory representation).
func decodeSockaddr(buf []byte, n uint16) (string, int) {
	if n < 2 {
		return "", 0
	}
	family := binary.LittleEndian.Uint16(buf[0:2])
	switch family {
	case 2: // AF_INET
		if n < 8 {
			return "", 0
		}
		port := int(binary.BigEndian.Uint16(buf[2:4]))
		ip := fmt.Sprintf("%d.%d.%d.%d", buf[4], buf[5], buf[6], buf[7])
		return ip, port
	case 10: // AF_INET6
		if n < 24 {
			return "", 0
		}
		port := int(binary.BigEndian.Uint16(buf[2:4]))
		parts := make([]string, 8)
		for i := 0; i < 8; i++ {
			parts[i] = fmt.Sprintf("%x", binary.BigEndian.Uint16(buf[8+i*2:10+i*2]))
		}
		return strings.Join(parts, ":"), port
	default:
		return "", 0
	}
}

func serviceGuess(port int) (zh, en string) {
	return sysinfo.ServiceName(port)
}

// maybeSSH re-tags a net:connect/listen event as a security:ssh event when
// it looks like SSH traffic — no dedicated probe needed, this is purely
// semantic re-labelling of data we already captured.
func maybeSSH(ev *event.Event, comm, host string, port int, direction string) {
	if port != 22 {
		return
	}
	ev.Cat, ev.Type = event.CatSecurity, "ssh"
	ev.SetField("direction", direction)
	ev.SetField("remote", fmt.Sprintf("%s:%d", host, port))
	_ = comm
}
