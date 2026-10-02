BINARY  := unix-monitor
PKG     := ./internal/collector
BPF_SRC := bpf/monitor.c
VMLINUX := bpf/vmlinux.h

.PHONY: build run bpf vmlinux clean cross

## Plain build using the already-committed eBPF object (no clang/bpftool
## needed). This is what CI / most contributors should use.
build:
	go build -o $(BINARY) .

run: build
	./$(BINARY)

## Regenerate bpf/vmlinux.h from this machine's running kernel BTF. Only
## needed once per kernel version, or after editing bpf/monitor.c.
vmlinux:
	bpftool btf dump file /sys/kernel/btf/vmlinux format c > $(VMLINUX)

## Recompile bpf/monitor.c -> internal/collector/monitor_x86_bpfel.{o,go}.
## Requires: clang, bpftool, llvm-strip, and bpf/vmlinux.h (run `make
## vmlinux` first if it's missing or stale for this kernel).
bpf: $(VMLINUX)
	cd $(PKG) && GOPACKAGE=collector go run github.com/cilium/ebpf/cmd/bpf2go@v0.22.0 \
		-target amd64 -go-package collector -output-dir . \
		-strip $$(command -v llvm-strip || command -v llvm-strip-18 || echo strip) \
		-cflags "-O2 -g -Wall -D__TARGET_ARCH_x86 -I../../bpf" \
		Monitor ../../$(BPF_SRC)

## Sanity-check cross-compilation for the poll-only fallback platforms.
cross:
	GOOS=darwin  GOARCH=arm64 go build -o /tmp/$(BINARY)-darwin-arm64  .
	GOOS=darwin  GOARCH=amd64 go build -o /tmp/$(BINARY)-darwin-amd64  .
	GOOS=windows GOARCH=amd64 go build -o /tmp/$(BINARY)-windows.exe   .
	GOOS=linux   GOARCH=arm64 go build -o /tmp/$(BINARY)-linux-arm64   .

clean:
	rm -f $(BINARY) $(VMLINUX)
