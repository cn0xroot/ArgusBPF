package sysinfo

import (
	"sort"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// Sampler produces periodic system Snapshots, computing rates from deltas.
type Sampler struct {
	lastDisk map[string]disk.IOCountersStat
	lastNet  map[string]net.IOCountersStat
	lastTS   time.Time
}

// NewSampler creates a Sampler and primes its CPU counters.
func NewSampler() *Sampler {
	s := &Sampler{lastDisk: map[string]disk.IOCountersStat{}, lastNet: map[string]net.IOCountersStat{}}
	_, _ = cpu.Percent(0, false) // prime
	return s
}

// Sample captures one Snapshot. It should be called roughly once per second.
func (s *Sampler) Sample() Snapshot {
	now := time.Now()
	dt := now.Sub(s.lastTS).Seconds()
	if s.lastTS.IsZero() || dt <= 0 {
		dt = 1
	}
	snap := Snapshot{TS: now.UnixMilli()}

	if tot, err := cpu.Percent(0, false); err == nil && len(tot) > 0 {
		snap.CPU.Total = tot[0]
	}
	if per, err := cpu.Percent(0, true); err == nil {
		snap.CPU.Per = per
	}
	if la, err := load.Avg(); err == nil {
		snap.Load = []float64{la.Load1, la.Load5, la.Load15}
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		snap.Mem = MemStat{
			Total: vm.Total, Used: vm.Used, Free: vm.Free, Available: vm.Available,
			Buffers: vm.Buffers, Cached: vm.Cached,
		}
	}
	if sw, err := mem.SwapMemory(); err == nil {
		snap.Mem.SwapTotal = sw.Total
		snap.Mem.SwapUsed = sw.Used
	}
	if up, err := host.Uptime(); err == nil {
		snap.Uptime = int64(up)
	}
	if pids, err := process.Pids(); err == nil {
		snap.Procs = len(pids)
	}

	// Disk IO rates.
	if cur, err := disk.IOCounters(); err == nil {
		for name, c := range cur {
			prev, ok := s.lastDisk[name]
			d := DiskIO{Name: name, ReadTotal: c.ReadBytes, WriteTotal: c.WriteBytes}
			if ok {
				d.ReadBps = float64(c.ReadBytes-prev.ReadBytes) / dt
				d.WriteBps = float64(c.WriteBytes-prev.WriteBytes) / dt
				d.ReadIOPS = float64(c.ReadCount-prev.ReadCount) / dt
				d.WriteIOPS = float64(c.WriteCount-prev.WriteCount) / dt
				d.Util = float64(c.IoTime-prev.IoTime) / (dt * 1000) * 100
				if d.Util > 100 {
					d.Util = 100
				}
			}
			snap.Disks = append(snap.Disks, d)
		}
		s.lastDisk = cur
		sort.Slice(snap.Disks, func(i, j int) bool {
			return snap.Disks[i].ReadBps+snap.Disks[i].WriteBps > snap.Disks[j].ReadBps+snap.Disks[j].WriteBps
		})
	}

	// Net IO rates.
	if cur, err := net.IOCounters(true); err == nil {
		for _, c := range cur {
			prev, ok := s.lastNet[c.Name]
			n := NetIO{Name: c.Name}
			if ok {
				n.RxBps = float64(c.BytesRecv-prev.BytesRecv) / dt
				n.TxBps = float64(c.BytesSent-prev.BytesSent) / dt
			}
			s.lastNet[c.Name] = c
			if n.Name == "lo" {
				continue
			}
			snap.Nets = append(snap.Nets, n)
		}
		sort.Slice(snap.Nets, func(i, j int) bool {
			return snap.Nets[i].RxBps+snap.Nets[i].TxBps > snap.Nets[j].RxBps+snap.Nets[j].TxBps
		})
	}

	s.lastTS = now
	return snap
}
