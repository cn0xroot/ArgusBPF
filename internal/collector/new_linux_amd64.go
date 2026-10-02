//go:build linux && amd64

package collector

import "fmt"

// New picks the best available backend: eBPF when we can load it (root +
// kernel BTF), the cross-platform poller otherwise.
func New() Collector {
	c, err := NewEBPF()
	if err != nil {
		return NewPoller(fmt.Sprintf("eBPF 采集不可用，已自动切换为轮询模式：%v", err))
	}
	return c
}
