//go:build !(linux && amd64)

package collector

import "runtime"

// New returns the cross-platform poller: the eBPF backend only exists for
// linux/amd64 (see new_linux_amd64.go).
func New() Collector {
	return NewPoller("当前平台 (" + runtime.GOOS + "/" + runtime.GOARCH + ") 暂不支持 eBPF 采集，已使用跨平台轮询模式")
}
