package sysinfo

import (
	"fmt"
	"sort"
	"strings"

	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// Processes lists all processes with summary info.
func Processes() []ProcInfo {
	var out []ProcInfo
	procs, err := process.Processes()
	if err != nil {
		return out
	}
	for _, p := range procs {
		out = append(out, procSummary(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

func procSummary(p *process.Process) ProcInfo {
	info := ProcInfo{PID: p.Pid}
	if v, err := p.Ppid(); err == nil {
		info.PPID = v
	}
	if v, err := p.Name(); err == nil {
		info.Comm = v
	}
	if v, err := p.Exe(); err == nil {
		info.Exe = v
	}
	if v, err := p.Cmdline(); err == nil {
		info.Cmdline = v
	}
	if v, err := p.Username(); err == nil {
		info.User = v
	}
	if uids, err := p.Uids(); err == nil && len(uids) > 0 {
		info.UID = uint32(uids[0])
	}
	if v, err := p.Status(); err == nil && len(v) > 0 {
		info.State = v[0]
	}
	if mi, err := p.MemoryInfo(); err == nil && mi != nil {
		info.RSS = mi.RSS
		info.VMS = mi.VMS
	}
	if v, err := p.CPUPercent(); err == nil {
		info.CPU = v
	}
	if v, err := p.NumThreads(); err == nil {
		info.Threads = v
	}
	if v, err := p.CreateTime(); err == nil {
		info.Start = v
	}
	return info
}

// ProcByPID returns a single process summary.
func ProcByPID(pid int32) (ProcInfo, bool) {
	p, err := process.NewProcess(pid)
	if err != nil {
		return ProcInfo{}, false
	}
	return procSummary(p), true
}

// Connections lists all network sockets with owning process info.
func Connections() []Conn {
	var out []Conn
	conns, err := gnet.Connections("all")
	if err != nil {
		return out
	}
	names := map[int32]string{}
	for _, c := range conns {
		proto := connProto(c.Type, c.Family)
		local := fmt.Sprintf("%s:%d", c.Laddr.IP, c.Laddr.Port)
		remote := ""
		if c.Raddr.IP != "" {
			remote = fmt.Sprintf("%s:%d", c.Raddr.IP, c.Raddr.Port)
		}
		comm := names[c.Pid]
		if comm == "" && c.Pid != 0 {
			if pi, ok := ProcByPID(c.Pid); ok {
				comm = pi.Comm
				names[c.Pid] = comm
			}
		}
		port := c.Raddr.Port
		if port == 0 {
			port = c.Laddr.Port
		}
		svcZh, svcEn := ServiceName(int(port))
		out = append(out, Conn{
			Proto: proto, Local: local, Remote: remote, State: c.Status,
			PID: c.Pid, Comm: comm, Service: svcZh, ServiceEn: svcEn,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].State != out[j].State {
			return out[i].State == "ESTABLISHED"
		}
		return out[i].Comm < out[j].Comm
	})
	return out
}

func connProto(sockType uint32, family uint32) string {
	t := "tcp"
	if sockType == 2 { // SOCK_DGRAM
		t = "udp"
	}
	if family == 10 { // AF_INET6
		t += "6"
	}
	return t
}

// ServiceName maps a well-known port to a human label, in Chinese and English.
func ServiceName(port int) (zh, en string) {
	if s, ok := wellKnownPorts[port]; ok {
		return s[0], s[1]
	}
	if port >= 49152 {
		return "临时端口", "Ephemeral port"
	}
	return "", ""
}

var wellKnownPorts = map[int][2]string{
	20: {"FTP数据", "FTP data"}, 21: {"FTP", "FTP"}, 22: {"SSH", "SSH"}, 23: {"Telnet", "Telnet"}, 25: {"SMTP邮件", "SMTP mail"},
	53: {"DNS域名", "DNS"}, 67: {"DHCP", "DHCP"}, 68: {"DHCP", "DHCP"}, 80: {"HTTP网页", "HTTP web"}, 110: {"POP3邮件", "POP3 mail"},
	123: {"NTP时间", "NTP time"}, 143: {"IMAP邮件", "IMAP mail"}, 161: {"SNMP", "SNMP"}, 443: {"HTTPS加密网页", "HTTPS web"},
	445: {"SMB文件共享", "SMB file sharing"}, 465: {"SMTPS", "SMTPS"}, 587: {"SMTP提交", "SMTP submission"}, 993: {"IMAPS", "IMAPS"}, 995: {"POP3S", "POP3S"},
	1080: {"SOCKS代理", "SOCKS proxy"}, 1433: {"SQLServer", "SQL Server"}, 1521: {"Oracle", "Oracle"}, 2049: {"NFS", "NFS"},
	3306: {"MySQL", "MySQL"}, 3389: {"远程桌面RDP", "Remote desktop (RDP)"}, 5432: {"PostgreSQL", "PostgreSQL"}, 5900: {"VNC", "VNC"},
	6379: {"Redis", "Redis"}, 8080: {"HTTP代理", "HTTP proxy"}, 8443: {"HTTPS备用", "HTTPS alt"}, 9200: {"Elasticsearch", "Elasticsearch"},
	11211: {"Memcached", "Memcached"}, 27017: {"MongoDB", "MongoDB"},
}

// splitHostPort returns the host part of "host:port".
func hostPart(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[:i]
	}
	return addr
}
