/* ArgusBPF — bilingual UI strings (zh default, en toggle).
   Covers the static chrome (nav, page titles/intros, toolbars, table
   headers) applied via applyI18n() walking [data-i18n]/-ph/-title, plus a
   t(key) lookup app.js's own dynamic dictionaries (category/risk labels,
   table headers, stat-card labels, button text toggles) call into
   directly. Event content itself (plain/pro/analogy/rule_title) is
   generated server-side with its own _en twin per field (see
   internal/explain, internal/rules, internal/glossary, internal/sysinfo)
   and picked per-language by app.js's evText() helper, not this file. */
(function (global) {
  'use strict';

  var DICT = {
    'nav.overview': ['总览', 'Overview'],
    'nav.live': ['实时事件', 'Live Events'],
    'nav.timeline': ['时间线', 'Timeline'],
    'nav.net': ['网络', 'Network'],
    'nav.disk': ['磁盘', 'Disk'],
    'nav.mem': ['内存', 'Memory'],
    'nav.proc': ['进程', 'Processes'],
    'nav.alerts': ['告警', 'Alerts'],
    'nav.glossary': ['知识库', 'Glossary'],

    'top.backend': ['采集后端', 'Collector backend'],
    'top.connecting': ['连接中…', 'Connecting…'],
    'top.timeRange': ['时间范围', 'Time range'],
    'top.customRange': ['自定义绝对时间', 'Custom absolute range'],
    'top.from': ['From', 'From'],
    'top.to': ['To', 'To'],
    'top.apply': ['应用', 'Apply'],
    'top.refreshNow': ['立即刷新', 'Refresh now'],
    'top.refreshInterval': ['自动刷新间隔', 'Auto-refresh interval'],
    'top.toggleMode': ['切换显示模式', 'Toggle display mode'],
    'top.modePlain': ['通俗', 'Plain'],
    'top.modePro': ['专业', 'Pro'],
    'top.toggleTheme': ['切换深/浅主题', 'Toggle dark/light theme'],
    'top.toggleLang': ['切换中文/English', 'Switch 中文/English'],

    'common.refresh': ['刷新', 'Refresh'],
    'common.close': ['关闭', 'Close'],

    'ov.title': ['系统总览', 'Overview'],
    'ov.intro.plain': ['这里是电脑的"体检仪表盘"：CPU 是大脑有多忙，内存是工作台占了多少，磁盘是仓库进出货多快，网络是和外界通信的流量。',
      'Think of this as a health check for your machine: CPU is how busy the brain is, memory is how full the workbench is, disk is how fast goods move in/out of the warehouse, network is traffic to/from the outside world.'],
    'ov.intro.pro': ['实时系统指标（1s 采样）与事件管线统计。曲线保留最近 600 个采样点。', 'Live system metrics (1s sampling) and pipeline stats. Charts keep the last 600 samples.'],
    'ov.cpu.plain': ['大脑忙碌程度（CPU）', 'Brain busyness (CPU)'],
    'ov.cpu.pro': ['CPU 使用率', 'CPU usage'],
    'ov.mem.plain': ['工作台占用（内存）', 'Workbench usage (Memory)'],
    'ov.mem.pro': ['内存使用', 'Memory usage'],
    'ov.disk.plain': ['仓库进出货（磁盘读写）', 'Warehouse traffic (Disk IO)'],
    'ov.disk.pro': ['磁盘吞吐 (B/s)', 'Disk throughput (B/s)'],
    'ov.net.plain': ['对外通信量（网络）', 'Outside traffic (Network)'],
    'ov.net.pro': ['网络吞吐 (B/s)', 'Network throughput (B/s)'],
    'ov.rate.plain': ['每秒发生的动作数', 'Actions happening per second'],
    'ov.rate.pro': ['事件速率 (ev/s, 60s)', 'Event rate (ev/s, 60s)'],
    'ov.top.plain': ['最活跃的程序', 'Most active programs'],
    'ov.top.pro': ['Top 进程（事件数）', 'Top processes (by event count)'],
    'ov.alerts.plain': ['最近值得注意的动作', 'Recent things worth noticing'],
    'ov.alerts.pro': ['最近中/高风险事件', 'Recent medium/high-risk events'],
    'ov.alerts.all': ['全部 →', 'All →'],

    'live.title': ['实时事件', 'Live Events'],
    'live.intro.plain': ['电脑里每个程序正在做的事情，会像流水账一样一条条出现在这里。点击任意一条可以看到详细解释。',
      'Everything every program on your machine is doing shows up here as a running log. Click any row for a full explanation.'],
    'live.intro.pro': ['内核/轮询采集的原始事件流（WebSocket 推送，最多保留 2000 行）。高频读写/缺页/块 IO 已按 1s 合并（count）。',
      'Raw kernel/poll event stream (pushed over WebSocket, capped at 2000 rows). High-frequency write/pagefault/block-IO events are folded per second (count).'],
    'live.allCat': ['全部类别', 'All categories'],
    'live.allRisk': ['全部风险', 'All risk levels'],
    'live.lowUp': ['低及以上', 'Low & up'],
    'live.medUp': ['中及以上', 'Medium & up'],
    'live.highOnly': ['仅高风险', 'High only'],
    'live.qph': ['关键词（进程/路径/地址…）', 'Keyword (process/path/address…)'],
    'live.pause': ['⏸ 暂停', '⏸ Pause'],
    'live.resume': ['▶ 继续', '▶ Resume'],
    'live.pausedBanner': ['已暂停 · 有 {n} 条新事件未显示，点击恢复', 'Paused · {n} new events hidden, click to resume'],
    'live.autoscroll': ['自动滚动', 'Auto-scroll'],
    'live.clear': ['清屏', 'Clear'],
    'live.waiting': ['等待事件…', 'Waiting for events…'],
    'live.loadMore': ['加载更早的事件', 'Load older events'],

    'tl.title': ['操作时间线', 'Timeline'],
    'tl.intro.plain': ['把一段时间里发生的事按"类别"或"程序"排成一条条泳道，颜色越深表示那一刻越忙，红色/黄色表示有值得注意的动作。点格子查看那一刻发生了什么。',
      'Lays events from a time range out as swimlanes by category or process — the brighter a cell, the busier that moment; red/yellow means something worth noticing happened. Click a cell to see it.'],
    'tl.intro.pro': ['按时间桶聚合的事件密度（亮度 ∝ log(n) 经感知 gamma 校正），颜色为桶内最高风险，高亮度处向暖白色过渡。点击桶下钻 /api/events。',
      'Event density aggregated per time bucket (brightness ∝ log(n), gamma-corrected), coloured by the bucket\'s highest risk, blooming toward warm white at high intensity. Click a bucket to drill into /api/events.'],
    'tl.byCat': ['按类别', 'By category'],
    'tl.byProc': ['按进程', 'By process'],
    'tl.byAgent': ['按 AI Agent', 'By AI agent'],
    'tl.clickHint': ['点击上方格子查看明细', 'Click a cell above to see details'],

    'net.title': ['网络活动', 'Network'],
    'net.intro.plain': ['电脑和外界的每一条"电话线"：哪个程序在和谁通话、用的是什么服务（比如加密网页、远程登录）。',
      'Every "phone line" between this machine and the outside: which program is talking to whom, and over what kind of service (encrypted web, remote login, …).'],
    'net.intro.pro': ['当前 socket 表（/proc/net/{tcp,udp}{,6} 或 gopsutil）与 connect/accept/dns 事件。', 'Current socket table (/proc/net/{tcp,udp}{,6} or gopsutil) plus connect/accept/dns events.'],
    'net.qph': ['过滤进程/地址/域名', 'Filter process/address/domain'],
    'net.allState': ['全部状态', 'All states'],
    'net.conns.plain': ['正在进行的连接', 'Active connections'],
    'net.conns.pro': ['Sockets', 'Sockets'],
    'net.events.plain': ['最近的联网动作', 'Recent network activity'],
    'net.events.pro': ['最近网络事件 (cat=net)', 'Recent network events (cat=net)'],

    'disk.title': ['磁盘读写', 'Disk IO'],
    'disk.intro.plain': ['磁盘就像仓库：读就是取货，写就是存货。下面的散点图显示仓库里哪一排货架（扇区）正在被访问。',
      'A disk is a warehouse: reading is picking goods up, writing is stocking them. The scatter plot below shows which shelf row (sector) is being touched.'],
    'disk.intro.pro': ['块设备吞吐 (/proc/diskstats)、block_rq_issue 扇区散点（x=时间, y=sector）、文件/进程 IO Top。',
      'Block device throughput (/proc/diskstats), block_rq_issue sector scatter (x=time, y=sector), top file/process IO.'],
    'disk.scatter.plain': ['货架访问分布（磁盘扇区）', 'Shelf access pattern (disk sectors)'],
    'disk.scatter.pro': ['Block request scatter (sector vs time)', 'Block request scatter (sector vs time)'],
    'disk.read': ['读', 'Read'],
    'disk.write': ['写', 'Write'],
    'disk.other': ['其他', 'Other'],
    'disk.files.plain': ['读写最多的文件', 'Busiest files'],
    'disk.files.pro': ['Top files', 'Top files'],
    'disk.procs.plain': ['读写最多的程序', 'Busiest programs'],
    'disk.procs.pro': ['Top processes by IO', 'Top processes by IO'],
    'disk.thFile': ['文件', 'File'],
    'disk.thProc': ['进程', 'Process'],

    'mem.title': ['内存', 'Memory'],
    'mem.intro.plain': ['内存就像工作台：程序干活时要把东西摊在台面上。这里能看到整张台面怎么分配的，以及某个程序把台面划成了哪些区域（代码区、草稿区、临时笔记区…）。',
      'Memory is a workbench: a running program spreads its stuff out on it. See how the whole bench is divided up, and how one program carves its share into regions (code, scratch space, notes, …).'],
    'mem.intro.pro': ['系统内存构成 (/proc/meminfo) 与进程虚拟地址空间 (/proc/&lt;pid&gt;/maps)，以及 mmap/mprotect/brk 等事件。',
      'System memory composition (/proc/meminfo) and a process\'s virtual address space (/proc/&lt;pid&gt;/maps), plus mmap/mprotect/brk events.'],
    'mem.qph': ['搜索进程名/PID', 'Search process name/PID'],
    'mem.view': ['查看', 'View'],
    'mem.sys.plain': ['整张工作台（物理内存）', 'The whole bench (physical memory)'],
    'mem.sys.pro': ['System memory', 'System memory'],
    'mem.swap.plain': ['备用区（Swap，硬盘上的临时台面）', 'Overflow space (swap — a temporary bench on disk)'],
    'mem.swap.pro': ['Swap', 'Swap'],
    'mem.chooseProc': ['选择一个进程查看它的内存地图', 'Pick a process to see its memory map'],
    'mem.addrLayout': ['虚拟地址空间布局（按地址排序，区域宽度 ∝ log(size)，灰色缝隙为未映射空洞）',
      'Virtual address space layout (sorted by address, region width ∝ log(size), grey gaps are unmapped holes)'],
    'mem.events.plain': ['最近的内存动作（申请、释放、改权限）', 'Recent memory activity (allocate, free, permission change)'],
    'mem.events.pro': ['memory events', 'memory events'],

    'proc.title': ['进程', 'Processes'],
    'proc.intro.plain': ['每个正在运行的程序都是一个"进程"。它们像家谱一样有父子关系：一个程序可以启动别的程序。点击任何一个查看它在做什么。',
      'Every running program is a "process". They have parent/child relationships like a family tree — one program can start another. Click any one to see what it\'s doing.'],
    'proc.intro.pro': ['进程树（ppid 关系），详情含 /proc/&lt;pid&gt;/{status,io,fd,maps} 与 socket。', 'Process tree (ppid relationships); detail includes /proc/&lt;pid&gt;/{status,io,fd,maps} and sockets.'],
    'proc.qph': ['搜索进程名/PID/命令行', 'Search process name/PID/command line'],
    'proc.expandAll': ['全部展开', 'Expand all'],
    'proc.collapseAll': ['全部折叠', 'Collapse all'],
    'proc.thProc': ['进程', 'Process'],
    'proc.thMem': ['内存', 'Memory'],
    'proc.selectHint': ['← 选择一个进程', '← Select a process'],

    'alerts.title': ['告警', 'Alerts'],
    'alerts.intro.plain': ['这里只列出"值得留意"的动作，比如有程序偷看密码文件、修改系统设置、读写别的程序的内存。不一定是坏事，但值得看一眼。',
      'Only the things "worth a look" show up here — a program peeking at password files, changing system settings, reading/writing another program\'s memory. Not necessarily bad, but worth a glance.'],
    'alerts.intro.pro': ['risk ≥ medium 的事件及命中规则（rules.json）。', 'Events with risk ≥ medium, and the rule each one matched (rules.json).'],
    'alerts.medUp': ['中 + 高', 'Medium + High'],
    'alerts.highOnly': ['仅高', 'High only'],
    'alerts.rules': ['检测规则', 'Detection rules'],
    'alerts.thRisk': ['风险', 'Risk'],
    'alerts.thRule': ['规则', 'Rule'],

    'gloss.title': ['知识库', 'Glossary'],
    'gloss.intro.plain': ['看不懂的术语都在这里，用通俗易懂的语言解释。', 'Every confusing term, explained in plain language.'],
    'gloss.intro.pro': ['术语表：通俗解释 + 技术定义。', 'Glossary: plain explanation + technical definition.'],
    'gloss.qph': ['搜索术语', 'Search terms'],
    'gloss.all': ['全部', 'All'],

    'card.totalEvents': ['总事件数', 'Total events'],
    'card.highRisk': ['高风险', 'High risk'],
    'card.mediumRisk': ['中风险', 'Medium risk'],
    'card.runningProcs': ['运行进程数', 'Running processes'],
    'card.totalConns': ['总连接数', 'Total connections'],
    'card.established': ['已建立', 'Established'],
    'card.listening': ['监听中', 'Listening'],
    'card.remoteHosts': ['远程主机数', 'Remote hosts'],

    'risk.info': ['信息', 'Info'],
    'risk.low': ['低', 'Low'],
    'risk.medium': ['中', 'Medium'],
    'risk.high': ['高', 'High'],

    'cat.file': ['文件', 'File'],
    'cat.process': ['进程', 'Process'],
    'cat.net': ['网络', 'Network'],
    'cat.memory': ['内存', 'Memory'],
    'cat.disk': ['磁盘', 'Disk'],
    'cat.kernel': ['内核', 'Kernel'],
    'cat.security': ['安全', 'Security'],

    'memkind.code': ['代码', 'Code'],
    'memkind.heap': ['堆', 'Heap'],
    'memkind.stack': ['栈', 'Stack'],
    'memkind.lib': ['共享库', 'Shared lib'],
    'memkind.anon': ['匿名', 'Anonymous'],
    'memkind.vdso': ['vDSO', 'vDSO'],
    'memkind.file': ['文件映射', 'File-backed'],
    'memkind.shm': ['共享内存', 'Shared mem'],

    'th.time': ['时间', 'Time'],
    'th.risk': ['风险', 'Risk'],
    'th.cat': ['类别', 'Category'],
    'th.process': ['进程', 'Process'],
    'th.desc': ['说明', 'Description'],
    'th.proto': ['协议', 'Proto'],
    'th.localAddr': ['本地地址', 'Local addr'],
    'th.remoteAddr': ['远程地址', 'Remote addr'],
    'th.state': ['状态', 'State'],
    'th.service': ['服务', 'Service'],

    'top.settings': ['外观设置：主题/字体/字号', 'Appearance: theme/font/size'],
    'settings.title': ['外观设置', 'Appearance'],
    'settings.themeLabel': ['主题配色', 'Theme'],
    'settings.fontLabel': ['界面字体', 'UI font'],
    'settings.font.system': ['系统默认', 'System default'],
    'settings.font.mono': ['等宽字体', 'Monospace'],
    'settings.font.serif': ['衬线字体', 'Serif'],
    'settings.font.kaiti': ['楷体', 'Kaiti'],
    'settings.font.heiti': ['黑体', 'Heiti'],
    'settings.font.songti': ['宋体', 'Songti'],
    'settings.sizeLabel': ['界面字号', 'UI font size'],
    'settings.previewText': ['ArgusBPF 正在通过 eBPF 实时监控这台机器的文件读写、网络连接与内存活动。',
      'ArgusBPF is watching this machine\'s file IO, network connections and memory activity live, via eBPF.'],
    'settings.reset': ['恢复默认', 'Reset to default'],
    'modal.close': ['关闭', 'Close'],

    'theme.dark': ['深色', 'Dark'],
    'theme.light': ['浅色', 'Light'],
    'theme.dracula': ['Dracula', 'Dracula'],
    'theme.nord': ['Nord', 'Nord'],
    'theme.midnight': ['Midnight', 'Midnight'],
    'theme.ocean': ['Ocean', 'Ocean'],
    'theme.forest': ['Forest', 'Forest'],
    'theme.sunset': ['Sunset', 'Sunset'],
    'theme.rose': ['Rose', 'Rose'],
    'theme.brand': ['标准配色', 'Brand'],

    'drawer.plain': ['通俗解释', 'Plain-language explanation'],
    'drawer.procInfo': ['进程信息', 'Process info'],
    'drawer.process': ['进程', 'Process'],
    'drawer.user': ['用户', 'User'],
    'drawer.exePath': ['程序路径', 'Executable path'],
    'drawer.ruleHit': ['命中规则', 'Rule matched'],
    'drawer.ruleId': ['规则 ID', 'Rule ID'],
    'drawer.techDetail': ['技术细节', 'Technical detail'],

    'chart.now': ['现在', 'now'],
    'chart.used': ['已用', 'Used'],
    'chart.read': ['读', 'Read'],
    'chart.write': ['写', 'Write'],
    'chart.rx': ['收', 'RX'],
    'chart.tx': ['发', 'TX'],
    'chart.evPerSec': ['事件/s', 'events/s'],

    'time.secAgo': ['{n} 秒前', '{n}s ago'],
    'time.minAgo': ['{n} 分钟前', '{n}m ago'],
    'time.hourAgo': ['{n} 小时前', '{n}h ago'],
    'time.dayAgo': ['{n} 天前', '{n}d ago'],

    'top.connected': ['已连接', 'Connected'],
    'top.disconnected': ['连接已断开', 'Disconnected'],
    'top.backendEbpf': ['eBPF 采集', 'eBPF collection'],
    'top.backendPoll': ['轮询采集', 'Polling collection'],

    'chart.waitingData': ['等待数据…', 'Waiting for data…'],
    'chart.noEventsPeriod': ['该时间段没有事件', 'No events in this period'],
    'chart.eventsCount': ['{n} 个事件', '{n} events'],
    'chart.highestRiskSuffix': ['，最高风险 ', ', highest risk '],

    'unit.entries': ['{n} 条', '{n}'],
    'unit.countSuffix': ['{n} 个', '{n}'],

    'empty.noData': ['暂无数据', 'No data'],
    'empty.noAlerts': ['暂无告警，一切正常', 'No alerts, all clear'],
    'empty.noConnections': ['暂无连接', 'No connections'],
    'empty.noBlockIoSamples': ['暂无块设备 IO 采样', 'No block I/O samples'],
    'empty.noDiskDevices': ['暂无磁盘设备', 'No disk devices'],
    'empty.memMapUnsupported': ['该平台暂不支持内存地图', 'Memory map not supported on this platform'],
    'empty.noProcesses': ['暂无进程', 'No processes'],
    'empty.loading': ['加载中…', 'Loading…'],
    'empty.noTerms': ['没有找到相关术语', 'No matching terms found'],
    'empty.procExited': ['该进程已退出或无权限查看', 'This process has exited or you lack permission to view it'],

    'mem.buffers': ['缓冲(buffers)', 'Buffers'],
    'mem.cached': ['缓存(cached)', 'Cached'],
    'mem.free': ['空闲', 'Free'],
    'mem.mapTitle': ['内存地图 — ', 'Memory Map — '],
    'mem.mapPlainDesc': ['{name} 把自己的内存台面划分成了下面这些区域：', '{name} divides its memory into the following regions:'],

    'proc.cmdline': ['命令行', 'Command line'],
    'proc.threadsSuffix': [' 线程', ' threads'],
    'proc.memory': ['内存', 'Memory'],
    'proc.diskIo': ['磁盘 IO', 'Disk I/O'],
    'proc.handles': ['句柄', 'Handles'],
    'proc.netConns': ['网络连接', 'Network connections'],
    'proc.viewFullMemMap': ['查看完整内存地图 →', 'View full memory map →'],
    'punct.listSep': ['，', ', '],
    'th.remote': ['远程', 'Remote'],
  };

  // Cached in memory, not re-read from localStorage on every t() call —
  // t()/catLabel()/riskLabel() etc. run per table row, so with a 2000-row
  // live feed that's thousands of calls per render; localStorage.getItem
  // that often is needless overhead for a value that only ever changes
  // via setLang().
  var curLang = (function () {
    try { return localStorage.getItem('umon.lang') === 'en' ? 'en' : 'zh'; } catch (e) { return 'zh'; }
  })();
  function lang() { return curLang; }
  function t(key) {
    var pair = DICT[key];
    if (!pair) return key;
    return pair[curLang === 'en' ? 1 : 0];
  }
  function setLang(l, persist) {
    curLang = l === 'en' ? 'en' : 'zh';
    if (persist !== false) { try { localStorage.setItem('umon.lang', curLang); } catch (e) {} }
    document.documentElement.setAttribute('lang', curLang === 'en' ? 'en' : 'zh-CN');
    applyI18n();
  }
  function applyI18n() {
    document.querySelectorAll('[data-i18n]').forEach(function (el) { el.textContent = t(el.getAttribute('data-i18n')); });
    document.querySelectorAll('[data-i18n-ph]').forEach(function (el) { el.placeholder = t(el.getAttribute('data-i18n-ph')); });
    document.querySelectorAll('[data-i18n-title]').forEach(function (el) { el.title = t(el.getAttribute('data-i18n-title')); });
    document.dispatchEvent(new CustomEvent('i18nchange'));
  }

  global.I18N = { t: t, lang: lang, setLang: setLang, apply: applyI18n };
})(window);
