//go:build linux

package sysinfo

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// readMaps parses /proc/<pid>/maps into typed regions with a plain-language
// classification (code/heap/stack/lib/anon/file/vdso/shm) and a byte-size
// summary per kind.
func readMaps(pid int32) ([]MemMap, map[string]uint64) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return nil, nil
	}
	defer f.Close()

	var maps []MemMap
	summary := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		// addr perms offset dev inode pathname
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		addrs := strings.SplitN(fields[0], "-", 2)
		if len(addrs) != 2 {
			continue
		}
		start, _ := strconv.ParseUint(addrs[0], 16, 64)
		end, _ := strconv.ParseUint(addrs[1], 16, 64)
		path := ""
		if len(fields) >= 6 {
			path = strings.Join(fields[5:], " ")
		}
		m := MemMap{
			Start: "0x" + addrs[0], End: "0x" + addrs[1], Size: end - start,
			Perms: fields[1], Offset: "0x" + fields[2], Dev: fields[3], Inode: fields[4],
			Path: path,
		}
		m.Kind = classifyMap(m)
		m.Plain, m.PlainEn = explainMapKind(m)
		summary[m.Kind] += m.Size
		maps = append(maps, m)
	}
	return maps, summary
}

func classifyMap(m MemMap) string {
	switch {
	case m.Path == "[heap]":
		return "heap"
	case m.Path == "[stack]" || strings.HasPrefix(m.Path, "[stack:"):
		return "stack"
	case m.Path == "[vdso]" || m.Path == "[vvar]" || m.Path == "[vsyscall]":
		return "vdso"
	case strings.HasPrefix(m.Path, "/memfd:") || strings.Contains(m.Path, "SYSV") || strings.HasPrefix(m.Path, "/dev/shm"):
		return "shm"
	case strings.Contains(m.Path, ".so"):
		return "lib"
	case m.Path == "":
		return "anon"
	case strings.Contains(m.Perms, "x") && strings.HasPrefix(m.Path, "/"):
		return "code"
	case strings.HasPrefix(m.Path, "/"):
		return "file"
	default:
		return "anon"
	}
}

func explainMapKind(m MemMap) (zh, en string) {
	switch m.Kind {
	case "heap":
		return "程序动态申请的「堆」内存，存放运行时创建的数据", "The program's dynamically-allocated \"heap\" memory, holding data created at runtime"
	case "stack":
		return "线程的「栈」，存放函数调用的临时变量", "A thread's \"stack\", holding temporary variables from function calls"
	case "lib":
		return "加载的共享库（.so），像是借用的公共工具书", "A loaded shared library (.so) — like a borrowed reference book"
	case "code":
		return "程序自身的可执行代码段", "The program's own executable code segment"
	case "vdso":
		return "内核提供的加速访问区域（vDSO），不是程序自己的内存", "A kernel-provided fast-access region (vDSO) — not the program's own memory"
	case "shm":
		return "与其它进程共享的内存区域", "A memory region shared with other processes"
	case "file":
		return "把文件直接映射进内存使用（内存映射文件）", "A file mapped directly into memory for use (memory-mapped file)"
	default:
		if strings.Contains(m.Perms, "w") && strings.Contains(m.Perms, "x") {
			return "⚠️ 一块同时可写又可执行的匿名内存，常见于高级攻击手法（代码注入）",
				"⚠️ A block of anonymous memory that's both writable and executable — common in advanced attack techniques (code injection)"
		}
		return "匿名内存区域，常用于临时缓冲区", "An anonymous memory region, commonly used as a temporary buffer"
	}
}

// readFDs parses /proc/<pid>/fd into typed descriptors.
func readFDs(pid int32) []FD {
	dir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []FD
	for _, e := range entries {
		n, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		target, _ := os.Readlink(dir + "/" + e.Name())
		kind := "other"
		switch {
		case strings.HasPrefix(target, "socket:"):
			kind = "socket"
		case strings.HasPrefix(target, "pipe:"):
			kind = "pipe"
		case strings.HasPrefix(target, "anon_inode:"):
			kind = "anon"
		case strings.HasPrefix(target, "/dev/"):
			kind = "dev"
		case strings.HasPrefix(target, "/"):
			kind = "file"
		}
		zh, en := explainFDKind(kind, target)
		out = append(out, FD{FD: n, Target: target, Kind: kind, Plain: zh, PlainEn: en})
	}
	return out
}

func explainFDKind(kind, target string) (zh, en string) {
	switch kind {
	case "socket":
		return "一个网络连接的句柄", "A network connection handle"
	case "pipe":
		return "进程间通信的管道", "An inter-process communication pipe"
	case "file":
		return "打开的文件：" + target, "An open file: " + target
	case "dev":
		return "打开的设备文件：" + target, "An open device file: " + target
	default:
		return "内核对象句柄", "A kernel object handle"
	}
}
