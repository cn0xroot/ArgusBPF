// Package explain turns an enriched event.Event into a professional
// one-liner (syscall-like form) and a plain-language explanation with an
// everyday analogy — the "通俗模式 / 专业模式" pair described in DESIGN.md —
// in both Chinese and English, so the web UI's language toggle has real
// bilingual text to switch between instead of just translating its own
// chrome. It is pure template/lookup logic, no LLM involved, so it works
// fully offline and deterministically.
package explain

import (
	"fmt"
	"strings"

	"argusbpf/internal/event"
)

// Apply fills ev.Title/Pro/Plain/Analogy (+ their _en twins) based on
// ev.Cat/Type/Fields. It must run after the collector has set Fields but
// before rules.Evaluate overwrites RuleTitle (Apply never touches
// Risk/Rule). Pro is syscall-form text and is already language-neutral,
// so it has no English twin.
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
		ev.Title, ev.TitleEn = ev.Type, ev.Type
		ev.Pro = ev.Type
		ev.Plain = "发生了一个系统事件：" + ev.Type
		ev.PlainEn = "A system event occurred: " + ev.Type
	}
}

func actor(ev *event.Event) string {
	if ev.Comm != "" {
		return ev.Comm
	}
	return fmt.Sprintf("pid %d", ev.PID)
}

// set is a small helper so every case below reads as one call per
// language instead of four separate field assignments.
func set(ev *event.Event, titleZh, titleEn, pro, plainZh, plainEn, analogyZh, analogyEn string) {
	ev.Title, ev.TitleEn = titleZh, titleEn
	ev.Pro = pro
	ev.Plain, ev.PlainEn = plainZh, plainEn
	ev.Analogy, ev.AnalogyEn = analogyZh, analogyEn
}

func setDefault(ev *event.Event, plainZh, plainEn string) {
	ev.Title, ev.TitleEn = ev.Type, ev.Type
	ev.Pro = ev.Type
	ev.Plain, ev.PlainEn = plainZh, plainEn
}

// ---- process ---------------------------------------------------------------

func explainProcess(ev *event.Event) {
	switch ev.Type {
	case "exec":
		path := ev.Str("path")
		args := ev.Str("args")
		set(ev, "进程启动", "Process started",
			fmt.Sprintf("execve(%q, [%s])", path, args),
			fmt.Sprintf("启动了一个新程序：%s", shortPath(path, false)),
			fmt.Sprintf("Launched a new program: %s", shortPath(path, true)),
			"就像按下了一个新 App 的开机键", "Like tapping the icon to launch a new app")
	case "fork":
		set(ev, "进程派生", "Process forked",
			fmt.Sprintf("fork() -> child pid=%d", ev.PID),
			fmt.Sprintf("%s 创建了一个子进程，用来并行处理任务", actor(ev)),
			fmt.Sprintf("%s created a child process to handle something in parallel", actor(ev)),
			"像是分出了一个帮手去做另一件事", "Like splitting off a helper to go handle something else")
	case "exit":
		code := ev.Str("code")
		set(ev, "进程退出", "Process exited",
			fmt.Sprintf("exit(%s)", code),
			fmt.Sprintf("%s 结束运行了", actor(ev)),
			fmt.Sprintf("%s finished running", actor(ev)),
			"程序干完活，下班了", "The program finished its work and clocked out")
	case "setuid":
		from, to := ev.Str("from_uid"), ev.Str("to_uid")
		pro := fmt.Sprintf("setuid(%s) [from=%s]", to, from)
		if to == "0" {
			set(ev, "切换用户身份", "Switched user identity", pro,
				fmt.Sprintf("%s 把自己的权限提升为了系统管理员(root)", actor(ev)),
				fmt.Sprintf("%s escalated its own privileges to system administrator (root)", actor(ev)),
				"相当于一名普通员工突然拿到了总管理员的万能钥匙", "Like an ordinary employee suddenly getting the master key to every door")
		} else {
			set(ev, "切换用户身份", "Switched user identity", pro,
				fmt.Sprintf("%s 切换成了另一个用户身份运行", actor(ev)),
				fmt.Sprintf("%s switched to running as a different user", actor(ev)),
				"像是换了个工作证继续干活", "Like swapping ID badges and carrying on with the job")
		}
	default:
		setDefault(ev, actor(ev)+" 触发了进程相关事件："+ev.Type, actor(ev)+" triggered a process event: "+ev.Type)
	}
}

// ---- file -------------------------------------------------------------------

func explainFile(ev *event.Event) {
	path := ev.Str("path")
	s := pathSemantics(path)
	switch ev.Type {
	case "open":
		flags := ev.Str("flags")
		set(ev, "打开文件", "Opened a file", fmt.Sprintf("openat(%q, %s)", path, flags),
			fmt.Sprintf("%s 打开了%s", actor(ev), s.semZh), fmt.Sprintf("%s opened %s", actor(ev), s.semEn),
			s.analogyZh, s.analogyEn)
	case "read":
		n := ev.Str("bytes")
		set(ev, "读取文件", "Read a file", fmt.Sprintf("read(%q) -> %s bytes", path, n),
			fmt.Sprintf("%s 读取了%s的内容（约 %s 字节）", actor(ev), s.semZh, n),
			fmt.Sprintf("%s read the contents of %s (~%s bytes)", actor(ev), s.semEn, n),
			s.analogyZh, s.analogyEn)
	case "write":
		n := ev.Str("bytes")
		set(ev, "写入文件", "Wrote a file", fmt.Sprintf("write(%q) -> %s bytes", path, n),
			fmt.Sprintf("%s 向%s写入了内容（约 %s 字节）", actor(ev), s.semZh, n),
			fmt.Sprintf("%s wrote data into %s (~%s bytes)", actor(ev), s.semEn, n),
			s.analogyZh, s.analogyEn)
	case "unlink":
		set(ev, "删除文件", "Deleted a file", fmt.Sprintf("unlink(%q)", path),
			fmt.Sprintf("%s 删除了%s", actor(ev), s.semZh), fmt.Sprintf("%s deleted %s", actor(ev), s.semEn),
			"就像把一份文件扔进了碎纸机", "Like feeding a document into the shredder")
	case "rename":
		dst := ev.Str("dst")
		set(ev, "重命名/移动文件", "Renamed/moved a file", fmt.Sprintf("rename(%q -> %q)", path, dst),
			fmt.Sprintf("%s 把文件从 %s 移动/改名为 %s", actor(ev), shortPath(path, false), shortPath(dst, false)),
			fmt.Sprintf("%s moved/renamed a file from %s to %s", actor(ev), shortPath(path, true), shortPath(dst, true)),
			"像给文件换了个名字或搬到了另一个文件夹", "Like giving a file a new name or moving it to another folder")
	default:
		ev.Title, ev.TitleEn = ev.Type, ev.Type
		ev.Pro = ev.Type + " " + path
		ev.Plain = actor(ev) + " 对文件做了操作：" + shortPath(path, false)
		ev.PlainEn = actor(ev) + " performed a file operation: " + shortPath(path, true)
	}
}

// ---- net --------------------------------------------------------------------

func explainNet(ev *event.Event) {
	switch ev.Type {
	case "connect":
		host, port := ev.Str("host"), ev.Str("port")
		svc := ev.Str("service")
		plainZh := fmt.Sprintf("%s 连接到了 %s（端口 %s）", actor(ev), host, port)
		plainEn := fmt.Sprintf("%s connected to %s (port %s)", actor(ev), host, port)
		if svc != "" {
			plainZh = fmt.Sprintf("%s 连接到了 %s（端口 %s，%s）", actor(ev), host, port, svc)
			plainEn = fmt.Sprintf("%s connected to %s (port %s, %s)", actor(ev), host, port, svc)
		}
		set(ev, "发起网络连接", "Made a network connection", fmt.Sprintf("connect(%s:%s)", host, port),
			plainZh, plainEn,
			"像是拨通了一个电话，对方是 "+host, "Like dialing a phone call to "+host)
	case "accept":
		remote := ev.Str("remote")
		set(ev, "接受网络连接", "Accepted a network connection", fmt.Sprintf("accept() <- %s", remote),
			fmt.Sprintf("%s 接受了来自 %s 的连接请求", actor(ev), remote),
			fmt.Sprintf("%s accepted an incoming connection from %s", actor(ev), remote),
			"像是接起了一个打进来的电话", "Like picking up an incoming phone call")
	case "listen":
		port := ev.Str("port")
		set(ev, "开始监听端口", "Started listening on a port", fmt.Sprintf("listen(:%s)", port),
			fmt.Sprintf("%s 开始监听端口 %s，等待外部连接", actor(ev), port),
			fmt.Sprintf("%s started listening on port %s, waiting for incoming connections", actor(ev), port),
			"相当于在门口挂了个牌子，等人来敲门", "Like hanging a sign on the door and waiting for a knock")
	case "dns":
		name := ev.Str("name")
		set(ev, "域名解析", "DNS lookup", fmt.Sprintf("getaddrinfo(%q)", name),
			fmt.Sprintf("%s 查询了域名 %s 对应的 IP 地址", actor(ev), name),
			fmt.Sprintf("%s looked up the IP address for %s", actor(ev), name),
			"像查电话黄页，把名字换成号码", "Like looking a name up in the phone book to get its number")
	case "close":
		set(ev, "关闭网络连接", "Closed a network connection", "close(socket)",
			fmt.Sprintf("%s 关闭了一个网络连接", actor(ev)), fmt.Sprintf("%s closed a network connection", actor(ev)),
			"挂了电话", "Hung up the phone")
	default:
		setDefault(ev, actor(ev)+" 触发了网络事件："+ev.Type, actor(ev)+" triggered a network event: "+ev.Type)
	}
}

// ---- memory -----------------------------------------------------------------

func explainMemory(ev *event.Event) {
	switch ev.Type {
	case "mmap":
		size, prot := ev.Str("size"), ev.Str("prot")
		addr := ev.Str("addr")
		set(ev, "申请内存映射", "Mapped new memory", fmt.Sprintf("mmap(addr=%s, size=%s, prot=%s)", addr, size, prot),
			fmt.Sprintf("%s 向系统申请了一块新内存（约 %s）", actor(ev), humanSize(size)),
			fmt.Sprintf("%s asked the system for a new block of memory (~%s)", actor(ev), humanSize(size)),
			"像是向仓库申请了一块新的货架空间", "Like requesting a new shelf of space in a warehouse")
	case "mprotect":
		addr, prot := ev.Str("addr"), ev.Str("prot")
		pro := fmt.Sprintf("mprotect(addr=%s, prot=%s)", addr, prot)
		if strings.Contains(prot, "W") && strings.Contains(prot, "X") {
			set(ev, "修改内存权限", "Changed memory permissions", pro,
				fmt.Sprintf("%s 把一块内存同时设成了可写+可执行", actor(ev)),
				fmt.Sprintf("%s marked a block of memory both writable and executable", actor(ev)),
				"⚠️ 相当于把一张纸既能随意涂写、又能当作指令直接执行——这是代码注入常用的手法",
				"⚠️ Like a sheet of paper you can both scribble on freely AND run as a set of instructions — a classic code-injection technique")
		} else {
			set(ev, "修改内存权限", "Changed memory permissions", pro,
				fmt.Sprintf("%s 修改了一块内存的访问权限为 %s", actor(ev), prot),
				fmt.Sprintf("%s changed a block of memory's access permissions to %s", actor(ev), prot),
				"像是给仓库的某个区域重新贴了“只读/可写”的标签", "Like re-labelling a warehouse area as \"read-only\" or \"read-write\"")
		}
	case "munmap":
		set(ev, "释放内存映射", "Freed mapped memory", fmt.Sprintf("munmap(addr=%s)", ev.Str("addr")),
			fmt.Sprintf("%s 释放了一块不再使用的内存", actor(ev)), fmt.Sprintf("%s freed a block of memory it no longer needs", actor(ev)),
			"把用完的货架清空还给仓库", "Clearing out a shelf and handing it back to the warehouse")
	case "brk":
		set(ev, "调整堆内存大小", "Resized the heap", fmt.Sprintf("brk(%s)", ev.Str("addr")),
			fmt.Sprintf("%s 调整了自己“堆”内存的大小", actor(ev)), fmt.Sprintf("%s resized its own \"heap\" memory", actor(ev)),
			"像是把自己的仓库边界往外挪了一点", "Like nudging your own warehouse's boundary wall outward a bit")
	case "ptrace":
		target := ev.Str("target_pid")
		set(ev, "附加调试/控制其它进程", "Attached to / controlled another process", fmt.Sprintf("ptrace(ATTACH, pid=%s)", target),
			fmt.Sprintf("%s 附加到了进程 %s 上，可以读写它的内存、控制它的执行", actor(ev), target),
			fmt.Sprintf("%s attached to process %s, able to read/write its memory and control its execution", actor(ev), target),
			"像是有人拿到了另一个人大脑的直接控制权", "Like someone gaining direct control over another person's brain")
	case "vm_read":
		target := ev.Str("target_pid")
		set(ev, "读取另一进程内存", "Read another process's memory", fmt.Sprintf("process_vm_readv(pid=%s)", target),
			fmt.Sprintf("%s 直接读取了进程 %s 的内存内容", actor(ev), target),
			fmt.Sprintf("%s directly read the memory contents of process %s", actor(ev), target),
			"相当于偷看了别人脑子里在想什么", "Like peeking directly into what's going on in someone else's head")
	case "vm_write":
		target := ev.Str("target_pid")
		set(ev, "写入另一进程内存", "Wrote another process's memory", fmt.Sprintf("process_vm_writev(pid=%s)", target),
			fmt.Sprintf("%s 直接向进程 %s 的内存写入了数据", actor(ev), target),
			fmt.Sprintf("%s directly wrote data into process %s's memory", actor(ev), target),
			"⚠️ 相当于直接往别人脑子里塞了一个想法——这是进程注入的典型手法",
			"⚠️ Like planting a thought directly in someone else's head — a classic process-injection technique")
	case "pagefault":
		set(ev, "缺页中断", "Page fault", fmt.Sprintf("page_fault(addr=%s)", ev.Str("addr")),
			fmt.Sprintf("%s 访问了一块还没准备好的内存，系统自动帮它补上了", actor(ev)),
			fmt.Sprintf("%s accessed a block of memory that wasn't ready yet, and the system filled it in automatically", actor(ev)),
			"像翻书翻到了还没装订的页，系统临时补了一页", "Like flipping to a page that hadn't been bound into the book yet, so the system printed one on the spot")
	default:
		setDefault(ev, actor(ev)+" 触发了内存相关事件："+ev.Type, actor(ev)+" triggered a memory event: "+ev.Type)
	}
}

// ---- disk -------------------------------------------------------------------

func explainDisk(ev *event.Event) {
	dev := ev.Str("dev")
	sector := ev.Str("sector")
	bytes := ev.Str("bytes")
	switch ev.Type {
	case "read":
		set(ev, "磁盘读取", "Disk read", fmt.Sprintf("block_read(dev=%s, sector=%s, len=%s)", dev, sector, bytes),
			fmt.Sprintf("%s 从硬盘(%s)读取了约 %s 数据", actor(ev), dev, humanSize(bytes)),
			fmt.Sprintf("%s read ~%s of data from disk (%s)", actor(ev), humanSize(bytes), dev),
			"像从书架上取下一本书来看", "Like pulling a book off the shelf to read it")
	case "write":
		set(ev, "磁盘写入", "Disk write", fmt.Sprintf("block_write(dev=%s, sector=%s, len=%s)", dev, sector, bytes),
			fmt.Sprintf("%s 向硬盘(%s)写入了约 %s 数据", actor(ev), dev, humanSize(bytes)),
			fmt.Sprintf("%s wrote ~%s of data to disk (%s)", actor(ev), humanSize(bytes), dev),
			"像往书架上新放了一本书", "Like putting a new book up on the shelf")
	default:
		setDefault(ev, actor(ev)+" 触发了磁盘事件："+ev.Type, actor(ev)+" triggered a disk event: "+ev.Type)
	}
}

// ---- kernel -----------------------------------------------------------------

func explainKernel(ev *event.Event) {
	switch ev.Type {
	case "module_load":
		name := ev.Str("name")
		set(ev, "加载内核模块", "Loaded a kernel module", fmt.Sprintf("init_module(%q)", name),
			fmt.Sprintf("系统加载了内核模块 %s，它将获得和操作系统内核同等的权限", name),
			fmt.Sprintf("The system loaded kernel module %s, which gains the same privileges as the OS kernel itself", name),
			"相当于给整栋大楼的电梯系统装了个新插件，权限极大", "Like installing a new plugin into a whole building's elevator system — extremely privileged")
	case "kill":
		target, sig := ev.Str("target_pid"), ev.Str("signal")
		set(ev, "发送信号/终止进程", "Sent a signal / killed a process", fmt.Sprintf("kill(pid=%s, sig=%s)", target, sig),
			fmt.Sprintf("%s 向进程 %s 发送了信号 %s", actor(ev), target, sig),
			fmt.Sprintf("%s sent signal %s to process %s", actor(ev), sig, target),
			"像是按下了对方的紧急呼叫/终止按钮", "Like pressing someone else's emergency-stop button")
	default:
		setDefault(ev, "内核事件："+ev.Type, "Kernel event: "+ev.Type)
	}
}

// ---- security (eCapture-style uprobe captures) ------------------------------

func explainSecurity(ev *event.Event) {
	switch ev.Type {
	case "ssh":
		dir, remote := ev.Str("direction"), ev.Str("remote")
		pro := fmt.Sprintf("ssh %s %s", dir, remote)
		if dir == "in" {
			set(ev, "SSH 活动", "SSH activity", pro,
				fmt.Sprintf("有人从 %s 通过 SSH 登录了本机", remote), fmt.Sprintf("Someone logged into this machine via SSH from %s", remote),
				"有人用钥匙从外面开门进来了", "Someone used a key to come in through the front door")
		} else {
			set(ev, "SSH 活动", "SSH activity", pro,
				fmt.Sprintf("本机正在通过 SSH 连接到 %s", remote), fmt.Sprintf("This machine is connecting via SSH to %s", remote),
				"你拿着钥匙去开别人家的门", "You're using a key to go open someone else's door")
		}
	case "sql":
		q := ev.Str("sql")
		set(ev, "数据库查询", "Database query", fmt.Sprintf("SQL: %s", q),
			fmt.Sprintf("%s 执行了一条数据库查询：%s", actor(ev), truncate(q, 80)),
			fmt.Sprintf("%s ran a database query: %s", actor(ev), truncate(q, 80)),
			"像是向档案室提交了一张查询/修改申请单", "Like filing a request slip with the records office to look up or change something")
	case "tls":
		dir := ev.Str("direction")
		preview := ev.Str("payload")
		dirZh := map[string]string{"send": "发送", "recv": "接收"}[dir]
		dirEn := map[string]string{"send": "sent", "recv": "received"}[dir]
		set(ev, "加密流量明文", "Encrypted traffic plaintext", fmt.Sprintf("TLS %s payload[%dB]: %s", dir, len(preview), truncate(preview, 60)),
			fmt.Sprintf("%s 通过加密通道%s了一段内容（已解密还原供你查看）", actor(ev), dirZh),
			fmt.Sprintf("%s %s some content over an encrypted channel (decrypted here for you to inspect)", actor(ev), dirEn),
			"信件本身是密封的，但我们在寄出/拆开的那一刻看了一眼内容", "The envelope itself stays sealed in transit, but we peeked at the contents right as it was sent/opened")
	case "bash":
		cmd := ev.Str("cmdline")
		set(ev, "终端命令", "Terminal command", fmt.Sprintf("bash$ %s", cmd),
			fmt.Sprintf("有人在终端里输入并执行了命令：%s", truncate(cmd, 80)),
			fmt.Sprintf("Someone typed and ran a command in a terminal: %s", truncate(cmd, 80)),
			"有人在键盘上敲了一行指令", "Someone typed a line of instructions on the keyboard")
	default:
		setDefault(ev, actor(ev)+" 触发了安全相关事件："+ev.Type, actor(ev)+" triggered a security event: "+ev.Type)
	}
}

// ---- helpers ----------------------------------------------------------------

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func shortPath(p string, en bool) string {
	if p == "" {
		if en {
			return "(unknown path)"
		}
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
