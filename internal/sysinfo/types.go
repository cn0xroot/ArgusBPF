// Package sysinfo provides cross-platform system snapshots, process details,
// memory maps, file descriptors and socket listings.
package sysinfo

// Snapshot is a point-in-time view of system resource usage.
type Snapshot struct {
	TS     int64     `json:"ts"`
	CPU    CPUStat   `json:"cpu"`
	Load   []float64 `json:"load"`
	Mem    MemStat   `json:"mem"`
	Disks  []DiskIO  `json:"disks"`
	Nets   []NetIO   `json:"nets"`
	Procs  int       `json:"procs"`
	Uptime int64     `json:"uptime"`
}

type CPUStat struct {
	Total float64   `json:"total"`
	Per   []float64 `json:"per"`
}

type MemStat struct {
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	Free      uint64 `json:"free"`
	Available uint64 `json:"available"`
	Buffers   uint64 `json:"buffers"`
	Cached    uint64 `json:"cached"`
	SwapTotal uint64 `json:"swap_total"`
	SwapUsed  uint64 `json:"swap_used"`
}

type DiskIO struct {
	Name       string  `json:"name"`
	ReadBps    float64 `json:"read_bps"`
	WriteBps   float64 `json:"write_bps"`
	ReadIOPS   float64 `json:"read_iops"`
	WriteIOPS  float64 `json:"write_iops"`
	Util       float64 `json:"util"`
	ReadTotal  uint64  `json:"read_total"`
	WriteTotal uint64  `json:"write_total"`
}

type NetIO struct {
	Name  string  `json:"name"`
	RxBps float64 `json:"rx_bps"`
	TxBps float64 `json:"tx_bps"`
}

// ProcInfo describes one process.
type ProcInfo struct {
	PID     int32   `json:"pid"`
	PPID    int32   `json:"ppid"`
	Comm    string  `json:"comm"`
	Exe     string  `json:"exe"`
	Cmdline string  `json:"cmdline"`
	User    string  `json:"user"`
	UID     uint32  `json:"uid"`
	State   string  `json:"state"`
	RSS     uint64  `json:"rss"`
	VMS     uint64  `json:"vms"`
	CPU     float64 `json:"cpu"`
	Threads int32   `json:"threads"`
	Start   int64   `json:"start"`
}

// MemMap is one region of a process address space (/proc/pid/maps).
type MemMap struct {
	Start  string `json:"start"`
	End    string `json:"end"`
	Size   uint64 `json:"size"`
	Perms  string `json:"perms"`
	Offset string `json:"offset"`
	Dev    string `json:"dev"`
	Inode  string `json:"inode"`
	Path   string `json:"path"`
	Kind    string `json:"kind"` // code|data|heap|stack|lib|anon|vdso|file|shm
	Plain   string `json:"plain"`
	PlainEn string `json:"plain_en"`
}

// FD is one open file descriptor.
type FD struct {
	FD      int    `json:"fd"`
	Target  string `json:"target"`
	Kind    string `json:"kind"` // file|socket|pipe|anon|dev|other
	Plain   string `json:"plain"`
	PlainEn string `json:"plain_en"`
}

// Conn is one network socket.
type Conn struct {
	Proto   string `json:"proto"`
	Local   string `json:"local"`
	Remote  string `json:"remote"`
	State   string `json:"state"`
	PID     int32  `json:"pid"`
	Comm    string `json:"comm"`
	Host      string `json:"host"`
	Service   string `json:"service"`
	ServiceEn string `json:"service_en"`
	Plain     string `json:"plain"`
	PlainEn   string `json:"plain_en"`
}

// ProcDetail is the full per-process view.
type ProcDetail struct {
	Proc        ProcInfo          `json:"proc"`
	Maps        []MemMap          `json:"maps"`
	MapsSummary map[string]uint64 `json:"maps_summary"`
	FDs         []FD              `json:"fds"`
	IO          IOStat            `json:"io"`
	Conns       []Conn            `json:"conns"`
	Plain       string            `json:"plain"`
	PlainEn     string            `json:"plain_en"`
}

type IOStat struct {
	ReadBytes  uint64 `json:"read_bytes"`
	WriteBytes uint64 `json:"write_bytes"`
	Rchar      uint64 `json:"rchar"`
	Wchar      uint64 `json:"wchar"`
	Syscr      uint64 `json:"syscr"`
	Syscw      uint64 `json:"syscw"`
}
