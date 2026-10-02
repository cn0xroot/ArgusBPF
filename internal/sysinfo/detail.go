package sysinfo

import "github.com/shirou/gopsutil/v4/process"

// Detail assembles the full per-process view.
func Detail(pid int32) (ProcDetail, bool) {
	info, ok := ProcByPID(pid)
	if !ok {
		return ProcDetail{}, false
	}
	d := ProcDetail{Proc: info}
	d.Maps, d.MapsSummary = readMaps(pid)
	d.FDs = readFDs(pid)

	if p, err := process.NewProcess(pid); err == nil {
		if io, err := p.IOCounters(); err == nil && io != nil {
			d.IO = IOStat{
				ReadBytes: io.ReadBytes, WriteBytes: io.WriteBytes,
				Rchar: io.ReadBytes, Wchar: io.WriteBytes,
			}
		}
	}
	for _, c := range Connections() {
		if c.PID == pid {
			d.Conns = append(d.Conns, c)
		}
	}
	d.Plain, d.PlainEn = describeProc(info)
	return d, true
}

func describeProc(p ProcInfo) (zh, en string) {
	mb := humanBytes(p.RSS)
	return "进程 " + p.Comm + " 正在运行，占用内存约 " + mb + "。",
		"Process " + p.Comm + " is running, using about " + mb + " of memory."
}

func humanBytes(b uint64) string {
	const u = 1024
	if b < u {
		return itoa(int64(b)) + " B"
	}
	exp, n := 0, float64(b)
	for n >= u && exp < 4 {
		n /= u
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB"}
	return ftoa(n) + " " + units[exp-1]
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func ftoa(f float64) string {
	whole := int64(f)
	frac := int64((f - float64(whole)) * 10)
	if frac < 0 {
		frac = -frac
	}
	return itoa(whole) + "." + itoa(frac)
}
