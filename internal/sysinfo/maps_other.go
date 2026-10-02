//go:build !linux

package sysinfo

// readMaps and readFDs have no portable implementation outside Linux's
// /proc filesystem; other platforms fall back to an empty memory map and
// fd list (process summary, IO counters and connections still work via
// gopsutil).
func readMaps(pid int32) ([]MemMap, map[string]uint64) { return nil, nil }

func readFDs(pid int32) []FD { return nil }
