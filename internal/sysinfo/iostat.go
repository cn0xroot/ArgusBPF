package sysinfo

import (
	"sort"

	"github.com/shirou/gopsutil/v4/process"
)

// ProcIO is one process's cumulative disk IO, for the "top processes by
// IO" table on the disk page.
type ProcIO struct {
	PID        int32  `json:"pid"`
	Comm       string `json:"comm"`
	ReadBytes  uint64 `json:"read_bytes"`
	WriteBytes uint64 `json:"write_bytes"`
}

// TopProcsByIO returns the n processes with the most cumulative disk IO
// (read+write bytes since process start, per /proc/<pid>/io).
func TopProcsByIO(n int) []ProcIO {
	procs, err := process.Processes()
	if err != nil {
		return nil
	}
	var out []ProcIO
	for _, p := range procs {
		io, err := p.IOCounters()
		if err != nil || io == nil || (io.ReadBytes == 0 && io.WriteBytes == 0) {
			continue
		}
		name, _ := p.Name()
		out = append(out, ProcIO{PID: p.Pid, Comm: name, ReadBytes: io.ReadBytes, WriteBytes: io.WriteBytes})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ReadBytes+out[i].WriteBytes > out[j].ReadBytes+out[j].WriteBytes
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
