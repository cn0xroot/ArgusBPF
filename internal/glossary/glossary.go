// Package glossary serves the static plain/professional term explanations
// shown on the Web UI's "知识库" page, in both Chinese and English.
package glossary

// Term is one glossary entry. Term/Cat/Short/Plain/Pro are Chinese; the
// _en fields are their English counterparts for the UI's language toggle.
type Term struct {
	Term    string `json:"term"`
	Cat     string `json:"cat"`
	Short   string `json:"short"`
	Plain   string `json:"plain"`
	Pro     string `json:"pro"`
	TermEn  string `json:"term_en"`
	CatEn   string `json:"cat_en"`
	ShortEn string `json:"short_en"`
	PlainEn string `json:"plain_en"`
	ProEn   string `json:"pro_en"`
}

// Terms is the built-in glossary. It's small enough to keep as Go data
// rather than a separate embedded file.
var Terms = []Term{
	{"进程 (Process)", "进程", "一个正在运行的程序", "你电脑里每个正在跑的程序都是一个进程，有自己的身份证号（PID）。", "操作系统资源分配的基本单位，拥有独立的地址空间、文件描述符表等。",
		"Process", "Process", "A program that is currently running", "Every running program on your computer is a process, with its own ID number (PID).", "The basic unit of OS resource allocation, with its own address space, file descriptor table, etc."},
	{"线程 (Thread)", "进程", "进程内部的一条执行路线", "一个进程可以同时做好几件事，每件事就是一个线程，它们共享同一个进程的内存。", "轻量级执行单元，共享所属进程的地址空间，调度单位为 task_struct。",
		"Thread", "Process", "One line of execution inside a process", "A process can do several things at once — each one is a thread, and they all share the same process's memory.", "A lightweight execution unit sharing its owning process's address space; the kernel's scheduling unit is task_struct."},
	{"PID", "进程", "进程的身份证号", "系统给每个进程发的一个编号，用来区分谁是谁。", "Process ID，内核为每个进程分配的唯一整数标识。",
		"PID", "Process", "A process's ID number", "A number the system assigns to every process so it can tell them apart.", "Process ID — a unique integer the kernel assigns to each process."},
	{"文件描述符 (fd)", "文件", "程序手里拿着的一张“存取卡”", "程序打开文件、网络连接甚至管道时，系统都会发给它一张编号卡片，之后就凭卡号操作。", "进程打开资源（文件/socket/pipe）时内核返回的小整数索引，指向文件描述符表中的条目。",
		"File descriptor (fd)", "File", "An \"access card\" a program holds", "When a program opens a file, network connection, or even a pipe, the system hands it a numbered card — it uses that number from then on.", "A small integer index the kernel returns when a process opens a resource (file/socket/pipe), pointing into its file descriptor table."},
	{"系统调用 (syscall)", "内核", "程序向操作系统提的一次请求", "程序自己不能直接碰硬件，必须“喊”一声让操作系统帮忙，比如“帮我开个文件”。", "用户态程序请求内核服务的标准接口，如 open/read/write/connect 等。",
		"System call (syscall)", "Kernel", "A request a program makes to the OS", "A program can't touch hardware directly — it has to \"call out\" to the OS for help, like \"open this file for me\".", "The standard interface user-space programs use to request kernel services, e.g. open/read/write/connect."},
	{"内存页 (page)", "内存", "内存的最小“砖块”", "系统管理内存不是一个字节一个字节管，而是一块一块（通常4KB）地管，每块叫一页。", "虚拟/物理内存管理的最小单位，x86_64 常见页大小为 4KB。",
		"Memory page", "Memory", "Memory's smallest \"brick\"", "The system doesn't manage memory byte by byte — it manages it in fixed-size blocks (usually 4KB), each called a page.", "The smallest unit of virtual/physical memory management; 4KB is the common page size on x86_64."},
	{"堆 (Heap)", "内存", "程序运行时随手堆放东西的地方", "程序运行中临时需要多少空间就申请多少，像随手堆在地上的箱子。", "通过 malloc/brk/mmap 动态分配的内存区域，大小随运行时需求增长。",
		"Heap", "Memory", "Where a running program piles things up", "A program requests however much space it needs on the fly, like boxes stacked loosely on the floor.", "A memory region dynamically allocated via malloc/brk/mmap, growing with runtime demand."},
	{"栈 (Stack)", "内存", "函数调用时的便签纸叠", "每次调用一个函数，就在这叠纸上新加一张记笔记，函数结束就撕掉。", "后进先出结构，保存函数调用帧、局部变量和返回地址。",
		"Stack", "Memory", "A pad of sticky notes for function calls", "Every function call adds a new note to the pad; when the function ends, that note gets torn off.", "A last-in-first-out structure holding call frames, local variables, and return addresses."},
	{"虚拟内存 (Virtual Memory)", "内存", "每个程序以为自己独享一整块内存", "操作系统给每个程序画了一张“假地图”，实际物理内存被悄悄映射、共享和调配。", "进程看到的地址空间由 MMU 转换为物理地址，实现隔离与按需分配。",
		"Virtual memory", "Memory", "Every program thinks it owns all the memory to itself", "The OS hands each program a \"fake map\" — the real physical memory underneath gets quietly mapped, shared, and juggled around.", "The address space a process sees is translated to physical addresses by the MMU, giving isolation and on-demand allocation."},
	{"内存映射 (mmap)", "内存", "把一块仓库直接搬到工作台上", "可以把文件或一块空白内存直接“摆”到程序的内存里直接用，不用来回搬运。", "syscall mmap() 在进程地址空间建立文件或匿名内存的映射。",
		"Memory mapping (mmap)", "Memory", "Moving a warehouse shelf straight onto the workbench", "A file or a blank block of memory can be \"placed\" directly into a program's memory for immediate use, with no back-and-forth copying.", "The mmap() syscall establishes a mapping of a file or anonymous memory into a process's address space."},
	{"缺页中断 (Page Fault)", "内存", "翻书翻到了还没装订的页", "程序访问了一块还没准备好的内存，系统临时去准备好再给它。", "访问尚未建立页表映射或已换出的页面时触发的 CPU 异常，由内核处理后恢复执行。",
		"Page fault", "Memory", "Flipping to a page that hasn't been bound into the book yet", "A program accessed a block of memory that wasn't ready — the system scrambles to prepare it on the spot.", "A CPU exception triggered by accessing a page with no page-table mapping yet, or one that was swapped out; the kernel handles it and execution resumes."},
	{"Swap", "内存", "内存不够用时借的硬盘空间", "当内存实在不够用，系统会把暂时不用的数据临时放到硬盘上。", "将部分内存页换出到磁盘的机制，速度远慢于物理内存。",
		"Swap", "Memory", "Disk space borrowed when memory runs short", "When memory truly isn't enough, the system temporarily parks data it isn't using on disk.", "A mechanism that swaps some memory pages out to disk; far slower than physical memory."},
	{"Root 权限", "安全", "管理员的万能钥匙", "拿到 root 权限等于拿到了这台电脑的万能钥匙，能做任何事。", "uid=0 的超级用户权限，不受大多数权限检查限制。",
		"Root privileges", "Security", "The administrator's master key", "Having root is like holding the master key to the whole machine — you can do anything.", "Superuser privileges (uid=0), exempt from most permission checks."},
	{"ptrace", "安全", "给另一个程序装了个遥控器", "可以暂停、查看、甚至操控另一个程序的内部状态，调试器就是这么工作的。", "系统调用，允许一个进程检查并控制另一个进程的执行与内存，常用于调试器，也被注入技术滥用。",
		"ptrace", "Security", "Installing a remote control on another program", "Lets you pause, inspect, or even manipulate another program's internal state — this is how debuggers work.", "A syscall letting one process inspect and control another's execution and memory; used by debuggers, and abused by injection techniques."},
	{"进程注入", "安全", "往别人脑子里塞想法", "攻击者把自己的代码塞进另一个正常程序里运行，看起来还是那个正常程序在跑。", "将代码写入目标进程地址空间并劫持执行流的技术，常配合 ptrace/VM 写操作实现。",
		"Process injection", "Security", "Planting a thought in someone else's head", "An attacker slips their own code into a legitimate program so it runs under that program's cover.", "A technique that writes code into a target process's address space and hijacks its execution flow, typically via ptrace/VM write operations."},
	{"Rootkit", "安全", "能隐藏自己的入室工具", "一种专门用来长期潜伏、还能隐藏自己踪迹的恶意软件。", "以内核模块或底层钩子方式潜伏，篡改系统调用结果以隐藏自身的恶意软件。",
		"Rootkit", "Security", "A break-in tool that hides itself", "Malware built to lurk long-term while covering its own tracks.", "Malware that embeds itself as a kernel module or low-level hook, tampering with syscall results to hide its own presence."},
	{"内核模块", "内核", "给操作系统装的插件", "可以在系统运行时给内核“加装”一个功能插件，驱动程序通常就是这么加载的。", "动态加载到内核空间运行的代码，拥有与内核同等权限，由 insmod/modprobe 加载。",
		"Kernel module", "Kernel", "A plugin installed into the OS", "A functional plugin can be \"bolted onto\" the kernel while the system is running — this is usually how drivers get loaded.", "Code dynamically loaded to run in kernel space with kernel-equivalent privileges, loaded via insmod/modprobe."},
	{"TCP 连接", "网络", "打一个持续通话的电话", "双方先“打通电话”（三次握手），确认都在听，才开始说话。", "面向连接的可靠传输协议，需经过三次握手建立、四次挥手断开。",
		"TCP connection", "Network", "Making a sustained phone call", "Both sides \"get the call connected\" first (the three-way handshake) and confirm they're both listening before talking.", "A connection-oriented reliable transport protocol, established with a three-way handshake and torn down with a four-way close."},
	{"端口 (Port)", "网络", "一栋楼里的房间号", "一个 IP 地址就像一栋楼，端口号就是具体的房间号，不同服务用不同房间。", "传输层用于区分同一主机上不同服务/连接的 16 位数字标识。",
		"Port", "Network", "A room number in a building", "An IP address is like a building; a port number is a specific room — different services use different rooms.", "A 16-bit transport-layer number distinguishing different services/connections on the same host."},
	{"DNS", "网络", "查电话黄页", "把好记的名字（网址）换成电脑真正认识的号码（IP 地址）。", "域名系统，将域名解析为 IP 地址的分布式查询协议。",
		"DNS", "Network", "Looking up the phone book", "Turns an easy-to-remember name (a web address) into the number (IP address) the computer actually understands.", "The Domain Name System — a distributed lookup protocol that resolves domain names into IP addresses."},
	{"TLS / HTTPS", "网络", "带锁的信封", "数据被装进一个只有收发双方能打开的带锁信封，路上没人能偷看内容。", "基于公钥交换建立会话密钥、对传输内容加密认证的协议，HTTP over TLS 即 HTTPS。",
		"TLS / HTTPS", "Network", "A sealed envelope", "Data is sealed into an envelope only the sender and receiver can open — nobody along the way can peek inside.", "A protocol that establishes a session key via public-key exchange to encrypt and authenticate traffic; HTTP over TLS is HTTPS."},
	{"inode", "磁盘", "文件的档案号", "每个文件在文件系统里都有一张档案卡，记着它存在哪些“货架”（数据块）上。", "文件系统中描述文件元数据（权限、大小、数据块指针等）的数据结构，由编号索引。",
		"inode", "Disk", "A file's record-office number", "Every file has a filing card in the filesystem noting which \"shelves\" (data blocks) it's stored on.", "A filesystem data structure describing a file's metadata (permissions, size, data-block pointers, etc.), indexed by number."},
	{"扇区 (Sector)", "磁盘", "仓库里的一格货架", "硬盘被划分成很多很多小格子，每次读写都是以格子为单位进行的。", "磁盘寻址的最小物理单位，传统为 512 字节，现代硬盘常为 4096 字节。",
		"Sector", "Disk", "One shelf slot in the warehouse", "A hard disk is divided into many tiny slots; every read/write happens one slot at a time.", "The smallest physical unit of disk addressing — traditionally 512 bytes, often 4096 bytes on modern disks."},
	{"eBPF", "内核", "给内核装的安全监控摄像头", "可以在不改内核代码、不重启的前提下，往内核里插入一段安全沙箱里跑的小程序，用来观察/干预系统行为——本工具就是靠它工作的。", "一种内核内虚拟机技术，允许加载经过验证的沙箱化程序挂载到内核事件（tracepoint/kprobe/uprobe等）上。",
		"eBPF", "Kernel", "A security camera installed in the kernel", "Lets you insert a small, sandboxed program into the kernel — without changing kernel code or rebooting — to observe or intervene in system behaviour. This tool runs on exactly that.", "An in-kernel virtual-machine technology that lets verified, sandboxed programs attach to kernel events (tracepoints, kprobes, uprobes, etc.)."},
}
