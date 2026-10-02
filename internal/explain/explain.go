// Package explain turns an enriched event.Event into a professional
// one-liner (syscall-like form) and a plain-language explanation with an
// everyday analogy — the "小白模式 / 专业模式" pair described in DESIGN.md.
// It is pure template/lookup logic, no LLM involved, so it works fully
// offline and deterministically.
package explain

import (
	"fmt"
	"strings"

	"unix-monitor/internal/event"
)

// Apply fills ev.Title/Pro/Plain/Analogy based on ev.Cat/Type/Fields.
// It must run after the collector has set Fields but before rules.Evaluate
// overwrites RuleTitle (Apply never touches Risk/Rule).
func Apply(ev *event.Event) {
	switch ev.Cat {
	case event.CatProcess:
		explainProcess(ev)
	case event.CatFile:
		explainFile(ev)
	case event.CatNet:
		explainNet(ev)
	case event.CatMemory:
		explainMemory(ev)
	case event.CatDisk:
		explainDisk(ev)
	case event.CatKernel:
		explainKernel(ev)
	case event.CatSecurity:
		explainSecurity(ev)
	default:
		ev.Title = ev.Type
		ev.Pro = ev.Type
		ev.Plain = "发生了一个系统事件：" + ev.Type
	}
}

func actor(ev *event.Event) string {
	if ev.Comm != "" {
		return ev.Comm
	}
	return fmt.Sprintf("pid %d", ev.PID)
}

// ---- process ---------------------------------------------------------------

func explainProcess(ev *event.Event) {
	switch ev.Type {
	case "exec":
		path := ev.Str("path")
		args := ev.Str("args")
		ev.Title = "进程启动"
		ev.Pro = fmt.Sprintf("execve(%q, [%s])", path, args)
		ev.Plain = fmt.Sprintf("启动了一个新程序：%s", shortPath(path))
		ev.Analogy = "就像按下了一个新 App 的开机键"
	case "fork":
		ev.Title = "进程派生"
		ev.Pro = fmt.Sprintf("fork() -> child pid=%d", ev.PID)
		ev.Plain = fmt.Sprintf("%s 创建了一个子进程，用来并行处理任务", actor(ev))
		ev.Analogy = "像是分出了一个帮手去做另一件事"
	case "exit":
		code := ev.Str("code")
		ev.Title = "进程退出"
		ev.Pro = fmt.Sprintf("exit(%s)", code)
		ev.Plain = fmt.Sprintf("%s 结束运行了", actor(ev))
		ev.Analogy = "程序干完活，下班了"
	case "setuid":
		from, to := ev.Str("from_uid"), ev.Str("to_uid")
		ev.Title = "切换用户身份"
		ev.Pro = fmt.Sprintf("setuid(%s) [from=%s]", to, from)
		if to == "0" {
			ev.Plain = fmt.Sprintf("%s 把自己的权限提升为了系统管理员(root)", actor(ev))
			ev.Analogy = "相当于一名普通员工突然拿到了总管理员的万能钥匙"
		} else {
			ev.Plain = fmt.Sprintf("%s 切换成了另一个用户身份运行", actor(ev))
			ev.Analogy = "像是换了个工作证继续干活"
		}
	default:
		ev.Title = ev.Type
		ev.Pro = ev.Type
		ev.Plain = actor(ev) + " 触发了进程相关事件：" + ev.Type
	}
}

// ---- file -------------------------------------------------------------------

func explainFile(ev *event.Event) {
	path := ev.Str("path")
	sem, analogy := pathSemantics(path)
	switch ev.Type {
	case "open":
		flags := ev.Str("flags")
		ev.Title = "打开文件"
		ev.Pro = fmt.Sprintf("openat(%q, %s)", path, flags)
		ev.Plain = fmt.Sprintf("%s 打开了%s", actor(ev), sem)
		ev.Analogy = analogy
	case "read":
		n := ev.Str("bytes")
		ev.Title = "读取文件"
		ev.Pro = fmt.Sprintf("read(%q) -> %s bytes", path, n)
		ev.Plain = fmt.Sprintf("%s 读取了%s的内容（约 %s 字节）", actor(ev), sem, n)
		ev.Analogy = analogy
	case "write":
		n := ev.Str("bytes")
		ev.Title = "写入文件"
		ev.Pro = fmt.Sprintf("write(%q) -> %s bytes", path, n)
		ev.Plain = fmt.Sprintf("%s 向%s写入了内容（约 %s 字节）", actor(ev), sem, n)
		ev.Analogy = analogy
	case "unlink":
		ev.Title = "删除文件"
		ev.Pro = fmt.Sprintf("unlink(%q)", path)
		ev.Plain = fmt.Sprintf("%s 删除了%s", actor(ev), sem)
		ev.Analogy = "就像把一份文件扔进了碎纸机"
	case "rename":
		dst := ev.Str("dst")
		ev.Title = "重命名/移动文件"
		ev.Pro = fmt.Sprintf("rename(%q -> %q)", path, dst)
		ev.Plain = fmt.Sprintf("%s 把文件从 %s 移动/改名为 %s", actor(ev), shortPath(path), shortPath(dst))
		ev.Analogy = "像给文件换了个名字或搬到了另一个文件夹"
	default:
		ev.Title = ev.Type
		ev.Pro = ev.Type + " " + path
		ev.Plain = actor(ev) + " 对文件做了操作：" + shortPath(path)
	}
}

// ---- net --------------------------------------------------------------------

func explainNet(ev *event.Event) {
	switch ev.Type {
	case "connect":
		host, port := ev.Str("host"), ev.Str("port")
		svc := ev.Str("service")
		ev.Title = "发起网络连接"
		ev.Pro = fmt.Sprintf("connect(%s:%s)", host, port)
		if svc != "" {
			ev.Plain = fmt.Sprintf("%s 连接到了 %s（端口 %s，%s）", actor(ev), host, port, svc)
		} else {
			ev.Plain = fmt.Sprintf("%s 连接到了 %s（端口 %s）", actor(ev), host, port)
		}
		ev.Analogy = "像是拨通了一个电话，对方是 " + host
	case "accept":
		remote := ev.Str("remote")
		ev.Title = "接受网络连接"
		ev.Pro = fmt.Sprintf("accept() <- %s", remote)
		ev.Plain = fmt.Sprintf("%s 接受了来自 %s 的连接请求", actor(ev), remote)
		ev.Analogy = "像是接起了一个打进来的电话"
	case "listen":
		port := ev.Str("port")
		ev.Title = "开始监听端口"
		ev.Pro = fmt.Sprintf("listen(:%s)", port)
		ev.Plain = fmt.Sprintf("%s 开始监听端口 %s，等待外部连接", actor(ev), port)
		ev.Analogy = "相当于在门口挂了个牌子，等人来敲门"
	case "dns":
		name := ev.Str("name")
		ev.Title = "域名解析"
		ev.Pro = fmt.Sprintf("getaddrinfo(%q)", name)
		ev.Plain = fmt.Sprintf("%s 查询了域名 %s 对应的 IP 地址", actor(ev), name)
		ev.Analogy = "像查电话黄页，把名字换成号码"
	case "close":
		ev.Title = "关闭网络连接"
		ev.Pro = "close(socket)"
		ev.Plain = fmt.Sprintf("%s 关闭了一个网络连接", actor(ev))
		ev.Analogy = "挂了电话"
	default:
		ev.Title = ev.Type
		ev.Pro = ev.Type
		ev.Plain = actor(ev) + " 触发了网络事件：" + ev.Type
	}
}

// ---- memory -----------------------------------------------------------------

func explainMemory(ev *event.Event) {
	switch ev.Type {
	case "mmap":
		size, prot := ev.Str("size"), ev.Str("prot")
		addr := ev.Str("addr")
		ev.Title = "申请内存映射"
		ev.Pro = fmt.Sprintf("mmap(addr=%s, size=%s, prot=%s)", addr, size, prot)
		ev.Plain = fmt.Sprintf("%s 向系统申请了一块新内存（约 %s）", actor(ev), humanSize(size))
		ev.Analogy = "像是向仓库申请了一块新的货架空间"
	case "mprotect":
		addr, prot := ev.Str("addr"), ev.Str("prot")
		ev.Title = "修改内存权限"
		ev.Pro = fmt.Sprintf("mprotect(addr=%s, prot=%s)", addr, prot)
		if strings.Contains(prot, "W") && strings.Contains(prot, "X") {
			ev.Plain = fmt.Sprintf("%s 把一块内存同时设成了可写+可执行", actor(ev))
			ev.Analogy = "⚠️ 相当于把一张纸既能随意涂写、又能当作指令直接执行——这是代码注入常用的手法"
		} else {
			ev.Plain = fmt.Sprintf("%s 修改了一块内存的访问权限为 %s", actor(ev), prot)
			ev.Analogy = "像是给仓库的某个区域重新贴了“只读/可写”的标签"
		}
	case "munmap":
		ev.Title = "释放内存映射"
		ev.Pro = fmt.Sprintf("munmap(addr=%s)", ev.Str("addr"))
		ev.Plain = fmt.Sprintf("%s 释放了一块不再使用的内存", actor(ev))
		ev.Analogy = "把用完的货架清空还给仓库"
	case "brk":
		ev.Title = "调整堆内存大小"
		ev.Pro = fmt.Sprintf("brk(%s)", ev.Str("addr"))
		ev.Plain = fmt.Sprintf("%s 调整了自己“堆”内存的大小", actor(ev))
		ev.Analogy = "像是把自己的仓库边界往外挪了一点"
	case "ptrace":
		target := ev.Str("target_pid")
		ev.Title = "附加调试/控制其它进程"
		ev.Pro = fmt.Sprintf("ptrace(ATTACH, pid=%s)", target)
		ev.Plain = fmt.Sprintf("%s 附加到了进程 %s 上，可以读写它的内存、控制它的执行", actor(ev), target)
		ev.Analogy = "像是有人拿到了另一个人大脑的直接控制权"
	case "vm_read":
		target := ev.Str("target_pid")
		ev.Title = "读取另一进程内存"
		ev.Pro = fmt.Sprintf("process_vm_readv(pid=%s)", target)
		ev.Plain = fmt.Sprintf("%s 直接读取了进程 %s 的内存内容", actor(ev), target)
		ev.Analogy = "相当于偷看了别人脑子里在想什么"
	case "vm_write":
		target := ev.Str("target_pid")
		ev.Title = "写入另一进程内存"
		ev.Pro = fmt.Sprintf("process_vm_writev(pid=%s)", target)
		ev.Plain = fmt.Sprintf("%s 直接向进程 %s 的内存写入了数据", actor(ev), target)
		ev.Analogy = "⚠️ 相当于直接往别人脑子里塞了一个想法——这是进程注入的典型手法"
	case "pagefault":
		ev.Title = "缺页中断"
		ev.Pro = fmt.Sprintf("page_fault(addr=%s)", ev.Str("addr"))
		ev.Plain = fmt.Sprintf("%s 访问了一块还没准备好的内存，系统自动帮它补上了", actor(ev))
		ev.Analogy = "像翻书翻到了还没装订的页，系统临时补了一页"
	default:
		ev.Title = ev.Type
		ev.Pro = ev.Type
		ev.Plain = actor(ev) + " 触发了内存相关事件：" + ev.Type
	}
}

// ---- disk -------------------------------------------------------------------

func explainDisk(ev *event.Event) {
	dev := ev.Str("dev")
	sector := ev.Str("sector")
	bytes := ev.Str("bytes")
	switch ev.Type {
	case "read":
		ev.Title = "磁盘读取"
		ev.Pro = fmt.Sprintf("block_read(dev=%s, sector=%s, len=%s)", dev, sector, bytes)
		ev.Plain = fmt.Sprintf("%s 从硬盘(%s)读取了约 %s 数据", actor(ev), dev, humanSize(bytes))
		ev.Analogy = "像从书架上取下一本书来看"
	case "write":
		ev.Title = "磁盘写入"
		ev.Pro = fmt.Sprintf("block_write(dev=%s, sector=%s, len=%s)", dev, sector, bytes)
		ev.Plain = fmt.Sprintf("%s 向硬盘(%s)写入了约 %s 数据", actor(ev), dev, humanSize(bytes))
		ev.Analogy = "像往书架上新放了一本书"
	default:
		ev.Title = ev.Type
		ev.Pro = ev.Type
		ev.Plain = actor(ev) + " 触发了磁盘事件：" + ev.Type
	}
}

// ---- kernel -----------------------------------------------------------------

func explainKernel(ev *event.Event) {
	switch ev.Type {
	case "module_load":
		name := ev.Str("name")
		ev.Title = "加载内核模块"
		ev.Pro = fmt.Sprintf("init_module(%q)", name)
		ev.Plain = fmt.Sprintf("系统加载了内核模块 %s，它将获得和操作系统内核同等的权限", name)
		ev.Analogy = "相当于给整栋大楼的电梯系统装了个新插件，权限极大"
	case "kill":
		target, sig := ev.Str("target_pid"), ev.Str("signal")
		ev.Title = "发送信号/终止进程"
		ev.Pro = fmt.Sprintf("kill(pid=%s, sig=%s)", target, sig)
		ev.Plain = fmt.Sprintf("%s 向进程 %s 发送了信号 %s", actor(ev), target, sig)
		ev.Analogy = "像是按下了对方的紧急呼叫/终止按钮"
	default:
		ev.Title = ev.Type
		ev.Pro = ev.Type
		ev.Plain = "内核事件：" + ev.Type
	}
}

// ---- security (eCapture-style uprobe captures) ------------------------------

func explainSecurity(ev *event.Event) {
	switch ev.Type {
	case "ssh":
		dir, remote := ev.Str("direction"), ev.Str("remote")
		ev.Title = "SSH 活动"
		ev.Pro = fmt.Sprintf("ssh %s %s", dir, remote)
		if dir == "in" {
			ev.Plain = fmt.Sprintf("有人从 %s 通过 SSH 登录了本机", remote)
			ev.Analogy = "有人用钥匙从外面开门进来了"
		} else {
			ev.Plain = fmt.Sprintf("本机正在通过 SSH 连接到 %s", remote)
			ev.Analogy = "你拿着钥匙去开别人家的门"
		}
	case "sql":
		q := ev.Str("sql")
		ev.Title = "数据库查询"
		ev.Pro = fmt.Sprintf("SQL: %s", q)
		ev.Plain = fmt.Sprintf("%s 执行了一条数据库查询：%s", actor(ev), truncate(q, 80))
		ev.Analogy = "像是向档案室提交了一张查询/修改申请单"
	case "tls":
		dir := ev.Str("direction")
		preview := ev.Str("payload")
		ev.Title = "加密流量明文"
		ev.Pro = fmt.Sprintf("TLS %s payload[%dB]: %s", dir, len(preview), truncate(preview, 60))
		ev.Plain = fmt.Sprintf("%s 通过加密通道%s了一段内容（已解密还原供你查看）", actor(ev), map[string]string{"send": "发送", "recv": "接收"}[dir])
		ev.Analogy = "信件本身是密封的，但我们在寄出/拆开的那一刻看了一眼内容"
	case "bash":
		cmd := ev.Str("cmdline")
		ev.Title = "终端命令"
		ev.Pro = fmt.Sprintf("bash$ %s", cmd)
		ev.Plain = fmt.Sprintf("有人在终端里输入并执行了命令：%s", truncate(cmd, 80))
		ev.Analogy = "有人在键盘上敲了一行指令"
	default:
		ev.Title = ev.Type
		ev.Pro = ev.Type
		ev.Plain = actor(ev) + " 触发了安全相关事件：" + ev.Type
	}
}

// ---- helpers ----------------------------------------------------------------

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func shortPath(p string) string {
	if p == "" {
		return "(未知路径)"
	}
	return p
}

func humanSize(s string) string {
	var n float64
	fmt.Sscanf(s, "%f", &n)
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for n >= 1024 && i < len(units)-1 {
		n /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", n, units[i])
}
