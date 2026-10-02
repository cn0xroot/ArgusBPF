//go:build linux && amd64

package collector

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cilium/ebpf/link"
)

// findLib locates a shared library/binary by scanning `ldconfig -p` output
// for names matching any of candidates, falling back to a fixed list of
// common install paths (including glob patterns like
// "/usr/lib/postgresql/*/bin" for versioned install layouts). Best-effort:
// returns "" if nothing is found.
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
		"/usr/lib/postgresql/*/bin", "/usr/pgsql-*/bin", "/opt/mysql/*/bin", "/usr/sbin",
	}
	for _, dir := range fixed {
		for _, c := range candidates {
			if strings.Contains(dir, "*") {
				matches, _ := filepath.Glob(dir + "/" + c)
				if len(matches) > 0 {
					return matches[len(matches)-1] // highest version sorts last
				}
				continue
			}
			p := dir + "/" + c
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}

// attachUprobes wires the eCapture-style application-level probes
// (DNS/TLS/Postgres). Each is independently best-effort: the target
// library/binary simply not being installed (e.g. no Postgres on this
// box) is the normal case, not a problem, and is skipped silently; only a
// library that IS present but fails to attach (permission, symbol
// mismatch, …) is surfaced as a warning.
func attachUprobes(progs *MonitorPrograms) (links []io.Closer, caps []string, warnings []string) {
	try := func(label, path, symbol string, attach func(ex *link.Executable) (link.Link, error)) bool {
		if path == "" {
			return false // optional capability, nothing installed to hook — not a warning
		}
		ex, err := link.OpenExecutable(path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: 无法打开 %s (%v)", label, path, err))
			return false
		}
		l, err := attach(ex)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: 挂载到 %s:%s 失败 (%v)", label, path, symbol, err))
			return false
		}
		links = append(links, l)
		caps = append(caps, label)
		return true
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
	if pg != "" && !attachPgQuery(pg, progs, &links, &caps, &warnings) {
		// Distro postgres builds are typically stripped AND LTO-compiled,
		// so exec_simple_query isn't in the binary's own symbol table and
		// isn't even named that in the LTO-mangled one — the normal
		// named-symbol path above fails on both counts. Resolve the
		// address ourselves from the matching dbgsym split-debug file
		// (same file, same code layout, just the symbols moved out to
		// /usr/lib/debug/.build-id/..) and attach by address instead,
		// which skips symbol lookup entirely.
		if addr, ok := symbolAddress(pg, "exec_simple_query"); ok {
			if ex, err := link.OpenExecutable(pg); err == nil {
				if l, err := ex.Uprobe("", progs.UprobePgQuery, &link.UprobeOptions{Address: addr}); err == nil {
					links = append(links, l)
					caps = append(caps, "sql")
					warnings = warnings[:len(warnings)-1] // drop the warning the failed attempt above just added
				} else {
					warnings = append(warnings, fmt.Sprintf("sql: 按调试符号地址挂载仍失败 (%v)", err))
				}
			}
		}
	}

	return
}

// attachPgQuery is the plain named-symbol attach path, split out so the
// dbgsym fallback above can tell whether it actually needs to run.
func attachPgQuery(pg string, progs *MonitorPrograms, links *[]io.Closer, caps *[]string, warnings *[]string) bool {
	ex, err := link.OpenExecutable(pg)
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("sql: 无法打开 %s (%v)", pg, err))
		return false
	}
	l, err := ex.Uprobe("exec_simple_query", progs.UprobePgQuery, nil)
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("sql: 挂载到 %s:exec_simple_query 失败 (%v)", pg, err))
		return false
	}
	*links = append(*links, l)
	*caps = append(*caps, "sql")
	return true
}
