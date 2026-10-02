#!/usr/bin/env bash
# Unix-Monitor installer.
# Builds the binary (no clang/bpftool needed — the eBPF object is
# pre-built and committed) and installs it, with an optional systemd
# service for always-on root/eBPF collection.
#
# Usage: ./install.sh [--prefix /usr/local/bin] [--systemd] [--no-build]
#   --systemd   also install + enable a systemd unit (requires root)
#   --no-build  skip `go build`, just (re)install an existing ./unix-monitor
set -euo pipefail

PREFIX="/usr/local/bin"
WITH_SYSTEMD=0
DO_BUILD=1

while [ $# -gt 0 ]; do
	case "$1" in
	--prefix) PREFIX="$2"; shift 2 ;;
	--systemd) WITH_SYSTEMD=1; shift ;;
	--no-build) DO_BUILD=0; shift ;;
	-h | --help)
		sed -n '2,10p' "$0"
		exit 0
		;;
	*)
		echo "unknown option: $1" >&2
		exit 1
		;;
	esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

say() { printf '%s\n' "$1"; }         # English (default)
sayzh() { printf '  %s\n' "$1"; }     # 中文（附加说明）

if [ "$DO_BUILD" -eq 1 ]; then
	if ! command -v go >/dev/null 2>&1; then
		say "error: Go toolchain not found in PATH — install Go 1.22+ first."
		sayzh "错误：未找到 Go 工具链，请先安装 Go 1.22 及以上版本。"
		exit 1
	fi
	say "Building unix-monitor..."
	sayzh "正在编译 unix-monitor…"
	go build -o unix-monitor .
fi

if [ ! -x ./unix-monitor ]; then
	say "error: ./unix-monitor not found — run without --no-build, or build it manually first."
	sayzh "错误：未找到 ./unix-monitor，请去掉 --no-build 重新运行，或先手动编译。"
	exit 1
fi

if [ "$(id -u)" -ne 0 ] && [ -w "$PREFIX" ] 2>/dev/null; then
	INSTALL_CMD="install"
elif [ "$(id -u)" -eq 0 ]; then
	INSTALL_CMD="install"
else
	INSTALL_CMD="sudo install"
fi

say "Installing to $PREFIX/unix-monitor ..."
sayzh "正在安装到 $PREFIX/unix-monitor …"
mkdir -p "$PREFIX" 2>/dev/null || true
$INSTALL_CMD -m 0755 ./unix-monitor "$PREFIX/unix-monitor"

if [ "$WITH_SYSTEMD" -eq 1 ]; then
	if [ "$(id -u)" -ne 0 ]; then
		say "error: --systemd requires running this script as root (sudo)."
		sayzh "错误：--systemd 需要以 root（sudo）运行本脚本。"
		exit 1
	fi
	UNIT=/etc/systemd/system/unix-monitor.service
	say "Installing systemd unit at $UNIT ..."
	sayzh "正在安装 systemd 服务到 $UNIT …"
	cat >"$UNIT" <<EOF
[Unit]
Description=Unix-Monitor — system activity dashboard (eBPF)
After=network.target

[Service]
ExecStart=$PREFIX/unix-monitor --listen 127.0.0.1:9900
Restart=on-failure
RestartSec=2
# eBPF needs root (or CAP_BPF+CAP_PERFMON+CAP_SYS_ADMIN depending on kernel).
User=root

[Install]
WantedBy=multi-user.target
EOF
	systemctl daemon-reload
	systemctl enable --now unix-monitor.service
	say "Started. Check status with: systemctl status unix-monitor"
	sayzh "已启动。可用以下命令查看状态：systemctl status unix-monitor"
else
	say ""
	say "Install complete. Run it with:"
	sayzh "安装完成。运行方式："
	say "  sudo $PREFIX/unix-monitor          # root enables the eBPF collector"
	sayzh "  sudo $PREFIX/unix-monitor          # root 权限才能启用 eBPF 采集"
	say "  $PREFIX/unix-monitor                # without root, falls back to polling"
	sayzh "  $PREFIX/unix-monitor                # 非 root 会自动降级为轮询模式"
	say ""
	say "Then open: http://127.0.0.1:9900"
	sayzh "然后打开：http://127.0.0.1:9900"
	say ""
	say "Re-run with --systemd to install it as an always-on root service."
	sayzh "加上 --systemd 参数可安装为常驻的 root 系统服务。"
fi
