//go:build linux && amd64

package collector

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/cilium/ebpf/link"
)

// findLib locates a shared library/binary by scanning `ldconfig -p` output
// for names matching any of candidates, falling back to a fixed list of
// common install paths. Best-effort: returns "" if nothing is found.
func findLib(candidates ...string) string {
	if out, err := exec.Command("ldconfig", "-p").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			for _, c := range candidates {
				if strings.Contains(line, c) {
					if i := strings.Index(line, "=> "); i >= 0 {
						return strings.TrimSpace(line[i+3:])
					}
				}
			}
		}
	}
	fixed := []string{
		"/usr/lib/x86_64-linux-gnu", "/lib/x86_64-linux-gnu", "/usr/lib64", "/usr/lib", "/lib",
		"/usr/bin", "/bin", "/usr/local/bin",
	}
	for _, dir := range fixed {
		for _, c := range candidates {
			p := dir + "/" + c
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}

// attachUprobes wires the eCapture-style application-level probes
// (DNS/TLS/Postgres). Each is independently best-effort: a missing
// library/symbol just skips that probe and adds a warning, it never fails
// the whole collector.
func attachUprobes(progs *MonitorPrograms) (links []io.Closer, caps []string, warnings []string) {
	try := func(label, path, symbol string, attach func(ex *link.Executable) (link.Link, error)) {
		if path == "" {
			warnings = append(warnings, label+": 未找到目标库/程序，已跳过")
			return
		}
		ex, err := link.OpenExecutable(path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: 无法打开 %s (%v)", label, path, err))
			return
		}
		l, err := attach(ex)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: 挂载到 %s:%s 失败 (%v)", label, path, symbol, err))
			return
		}
		links = append(links, l)
		caps = append(caps, label)
	}

	libc := findLib("libc.so.6", "libc.so")
	try("dns", libc, "getaddrinfo", func(ex *link.Executable) (link.Link, error) {
		return ex.Uprobe("getaddrinfo", progs.UprobeGetaddrinfo, nil)
	})

	libssl := findLib("libssl.so.3", "libssl.so.1.1", "libssl.so")
	try("tls", libssl, "SSL_write", func(ex *link.Executable) (link.Link, error) {
		return ex.Uprobe("SSL_write", progs.UprobeSslWrite, nil)
	})
	if libssl != "" {
		if ex, err := link.OpenExecutable(libssl); err == nil {
			if l, err := ex.Uprobe("SSL_read", progs.UprobeSslReadEnter, nil); err == nil {
				links = append(links, l)
				if l2, err := ex.Uretprobe("SSL_read", progs.UretprobeSslReadRet, nil); err == nil {
					links = append(links, l2)
				}
			} else {
				warnings = append(warnings, fmt.Sprintf("tls: 挂载 SSL_read 失败 (%v)", err))
			}
		}
	}

	pg := findLib("postgres")
	try("sql", pg, "exec_simple_query", func(ex *link.Executable) (link.Link, error) {
		return ex.Uprobe("exec_simple_query", progs.UprobePgQuery, nil)
	})

	return
}
