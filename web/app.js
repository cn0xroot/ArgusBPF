/* Unix-Monitor — main page logic (no deps, loaded after charts.js) */
(function () {
  'use strict';

  var TOKEN = new URLSearchParams(location.search).get('token') || '';
  var EV = new Map();       // id -> event, shared cache for all tables + drawer
  var state = {
    mode: 'plain',
    theme: '',
    range: { sec: 3600, label: 'Last 1 hour', since: null, until: null },
    refreshMs: 5000,
    page: 'overview',
  };

  var CAT = {
    file: { zh: '文件', color: 'var(--cat-file)' },
    process: { zh: '进程', color: 'var(--cat-process)' },
    net: { zh: '网络', color: 'var(--cat-net)' },
    memory: { zh: '内存', color: 'var(--cat-memory)' },
    disk: { zh: '磁盘', color: 'var(--cat-disk)' },
    kernel: { zh: '内核', color: 'var(--cat-kernel)' },
    security: { zh: '安全', color: 'var(--cat-security)' },
  };
  var RISK_LABEL = { info: '信息', low: '低', medium: '中', high: '高' };
  var MEMKIND = {
    code: { zh: '代码', color: 'var(--cat-memory)' },
    heap: { zh: '堆', color: 'var(--cyan)' },
    stack: { zh: '栈', color: 'var(--accent)' },
    lib: { zh: '共享库', color: 'var(--green)' },
    anon: { zh: '匿名', color: 'var(--yellow)' },
    vdso: { zh: 'vDSO', color: 'var(--gray)' },
    file: { zh: '文件映射', color: 'var(--cat-file)' },
    shm: { zh: '共享内存', color: 'var(--cat-disk)' },
  };

  // ---------------- tiny utils ----------------
  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }
  function fmtBytes(n) {
    n = Number(n) || 0;
    var u = ['B', 'KB', 'MB', 'GB', 'TB'], i = 0;
    while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
    return (i === 0 ? n.toFixed(0) : n.toFixed(1)) + ' ' + u[i];
  }
  function fmtBps(n) { return fmtBytes(n) + '/s'; }
  function fmtPct(n) { return (Number(n) || 0).toFixed(0) + '%'; }
  function fmtTime(ts) {
    var d = new Date(ts);
    return [d.getHours(), d.getMinutes(), d.getSeconds()].map(function (x) { return String(x).padStart(2, '0'); }).join(':');
  }
  function fmtAgo(ms) {
    var s = Math.floor((Date.now() - ms) / 1000);
    if (s < 60) return s + ' 秒前';
    if (s < 3600) return Math.floor(s / 60) + ' 分钟前';
    if (s < 86400) return Math.floor(s / 3600) + ' 小时前';
    return Math.floor(s / 86400) + ' 天前';
  }
  function qs(obj) {
    var parts = [];
    for (var k in obj) if (obj[k] !== undefined && obj[k] !== null && obj[k] !== '') parts.push(encodeURIComponent(k) + '=' + encodeURIComponent(obj[k]));
    return parts.length ? '?' + parts.join('&') : '';
  }
  function api(path, params) {
    return fetch(path + qs(params || {}), { headers: TOKEN ? { 'X-Token': TOKEN } : {} })
      .then(function (r) { if (!r.ok) throw new Error('HTTP ' + r.status); return r.json(); });
  }
  function isPro() { return state.mode === 'pro'; }
  function debounce(fn, ms) { var t; return function () { clearTimeout(t); var a = arguments; t = setTimeout(function () { fn.apply(null, a); }, ms); }; }

  // ---------------- event rendering (shared across live/timeline/net/mem/alerts) ----------------
  function evHeadHTML() {
    return '<tr><th style="width:68px">时间</th><th style="width:56px">风险</th><th style="width:120px">类别</th><th style="width:140px">进程</th><th>说明</th></tr>';
  }
  function evRowHTML(ev) {
    EV.set(ev.id, ev);
    var risk = ev.risk || 'info';
    var cat = CAT[ev.cat] || { zh: ev.cat, color: 'var(--gray)' };
    var cnt = ev.count > 1 ? ' <span class="tag">×' + ev.count + '</span>' : '';
    return '<tr class="click risk-' + risk + '" data-id="' + ev.id + '">' +
      '<td class="nowrap mono" title="' + esc(new Date(ev.ts).toLocaleString()) + '">' + fmtTime(ev.ts) + '</td>' +
      '<td><span class="risk ' + risk + '">' + RISK_LABEL[risk] + '</span></td>' +
      '<td class="nowrap"><span class="cat" style="--c:' + cat.color + '">' + cat.zh + '</span> <span class="pro-only tag">' + esc(ev.type) + '</span></td>' +
      '<td class="nowrap">' + esc(ev.comm || ('pid ' + ev.pid)) + ' <span class="dim pro-only">#' + ev.pid + '</span></td>' +
      '<td>' +
      '<div class="plain-only ev-plain">' + esc(ev.plain || ev.title || '') + '</div>' +
      (ev.analogy ? '<div class="plain-only ev-analogy">' + esc(ev.analogy) + '</div>' : '') +
      '<div class="pro-only ev-pro">' + esc(ev.pro || ev.title || '') + '</div>' +
      (ev.rule_title ? '<div class="tag" style="margin-top:3px">⚑ ' + esc(ev.rule_title) + '</div>' : '') +
      cnt +
      '</td></tr>';
  }
  function renderEvents(tbody, events, emptyEl) {
    tbody.innerHTML = events.map(evRowHTML).join('');
    if (emptyEl) emptyEl.style.display = events.length ? 'none' : '';
  }
  // single delegated click handler opens the drawer for any row with data-id
  document.addEventListener('click', function (e) {
    var tr = e.target.closest('tr[data-id]');
    if (tr) openDrawer(Number(tr.getAttribute('data-id')));
  });

  // ---------------- drawer ----------------
  function openDrawer(id) {
    var ev = EV.get(id);
    if (!ev) return;
    var risk = ev.risk || 'info';
    document.getElementById('drTitle').textContent = ev.rule_title || ev.title || ev.type;
    document.getElementById('drSub').innerHTML =
      '<span class="risk ' + risk + '">' + RISK_LABEL[risk] + '</span> &nbsp;' +
      esc(ev.cat + ':' + ev.type) + ' &nbsp;' + esc(new Date(ev.ts).toLocaleString());
    var fieldsRows = '';
    if (ev.fields) for (var k in ev.fields) fieldsRows += '<tr><td>' + esc(k) + '</td><td>' + esc(ev.fields[k]) + '</td></tr>';
    var body =
      '<section>' +
      '<h4>通俗解释</h4>' +
      '<div class="plain-box">' + esc(ev.plain || '') + (ev.analogy ? '<div class="ev-analogy" style="margin-top:6px">' + esc(ev.analogy) + '</div>' : '') + '</div>' +
      '</section>' +
      '<section>' +
      '<h4>进程信息</h4>' +
      '<dl class="kv">' +
      '<dt>进程</dt><dd>' + esc(ev.comm) + ' (pid ' + ev.pid + (ev.ppid ? ', ppid ' + ev.ppid : '') + ')</dd>' +
      (ev.user ? '<dt>用户</dt><dd>' + esc(ev.user) + ' (uid ' + ev.uid + ')</dd>' : '') +
      (ev.exe ? '<dt>程序路径</dt><dd class="mono">' + esc(ev.exe) + '</dd>' : '') +
      '</dl>' +
      '</section>' +
      (ev.rule_title ? '<section><h4>命中规则</h4><div class="plain-box" style="border-left-color:var(--red)"><b>' + esc(ev.rule_title) + '</b><div class="dim" style="margin-top:4px">规则 ID: ' + esc(ev.rule) + '</div></div></section>' : '') +
      '<section>' +
      '<details class="alt" open><summary>技术细节</summary>' +
      '<div class="code-block mt">' + esc(ev.pro || '') + '</div>' +
      (fieldsRows ? '<table class="fields mt"><tbody>' + fieldsRows + '</tbody></table>' : '') +
      '</details>' +
      '</section>';
    document.getElementById('drBody').innerHTML = body;
    document.getElementById('drawer').classList.add('show');
    document.getElementById('drawerMask').classList.add('show');
    document.getElementById('drawer').setAttribute('aria-hidden', 'false');
  }
  function closeDrawer() {
    document.getElementById('drawer').classList.remove('show');
    document.getElementById('drawerMask').classList.remove('show');
    document.getElementById('drawer').setAttribute('aria-hidden', 'true');
  }

  // ---------------- global chrome: mode / theme / time range / refresh ----------------
  function applyMode(m) {
    state.mode = m;
    document.body.classList.toggle('mode-plain', m === 'plain');
    document.body.classList.toggle('mode-pro', m === 'pro');
    document.querySelectorAll('#modeSeg button').forEach(function (b) { b.classList.toggle('on', b.dataset.mode === m); });
    try { localStorage.setItem('umon.mode', m); } catch (e) {}
  }
  function applyTheme(t) {
    state.theme = t;
    if (t) document.documentElement.setAttribute('data-theme', t); else document.documentElement.removeAttribute('data-theme');
    try { localStorage.setItem('umon.theme', t); } catch (e) {}
  }
  function setRange(sec, label, since, until) {
    state.range = { sec: sec, label: label, since: since || null, until: until || null };
    document.getElementById('timeLabel').textContent = label;
    refreshCurrent(true);
  }
  function rangeParams() {
    if (state.range.since) return { since: state.range.since, until: state.range.until };
    return { since: Date.now() - state.range.sec * 1000, until: Date.now() };
  }

  function initChrome() {
    try {
      var m = localStorage.getItem('umon.mode');
      applyMode(m === 'pro' ? 'pro' : 'plain');
      var t = localStorage.getItem('umon.theme');
      if (t) applyTheme(t);
    } catch (e) { applyMode('plain'); }

    document.getElementById('modeSeg').addEventListener('click', function (e) {
      var b = e.target.closest('button[data-mode]'); if (b) applyMode(b.dataset.mode);
    });
    document.getElementById('themeBtn').addEventListener('click', function () {
      var cur = document.documentElement.getAttribute('data-theme');
      applyTheme(cur === 'light' ? '' : 'light');
    });
    document.getElementById('drClose').addEventListener('click', closeDrawer);
    document.getElementById('drawerMask').addEventListener('click', closeDrawer);
    document.addEventListener('keydown', function (e) { if (e.key === 'Escape') closeDrawer(); });

    // time range popover
    var timeBtn = document.getElementById('timeBtn'), timePop = document.getElementById('timePop');
    timeBtn.addEventListener('click', function (e) { e.stopPropagation(); timePop.hidden = !timePop.hidden; });
    document.addEventListener('click', function (e) { if (!timePop.hidden && !timePop.contains(e.target) && e.target !== timeBtn) timePop.hidden = true; });
    document.querySelectorAll('#timePop .tr-quick button').forEach(function (b) {
      b.addEventListener('click', function () {
        document.querySelectorAll('#timePop .tr-quick button').forEach(function (x) { x.classList.remove('on'); });
        b.classList.add('on');
        setRange(Number(b.dataset.range), b.textContent.trim());
        timePop.hidden = true;
      });
    });
    document.querySelector('#timePop .tr-quick button[data-range="3600"]').classList.add('on');
    document.getElementById('timeApply').addEventListener('click', function () {
      var f = document.getElementById('timeFrom').value, t2 = document.getElementById('timeTo').value;
      if (!f || !t2) return;
      var since = new Date(f).getTime(), until = new Date(t2).getTime();
      setRange(Math.round((until - since) / 1000), f + ' → ' + t2, since, until);
      timePop.hidden = true;
    });

    // refresh interval
    var timer = null;
    function setupTimer() {
      if (timer) clearInterval(timer);
      state.refreshMs = Number(document.getElementById('refreshSel').value);
      if (state.refreshMs > 0) timer = setInterval(function () { refreshCurrent(false); }, state.refreshMs);
    }
    document.getElementById('refreshSel').addEventListener('change', setupTimer);
    document.getElementById('refreshNow').addEventListener('click', function () { refreshCurrent(true); });
    setupTimer();

    // side nav routing
    window.addEventListener('hashchange', route);
  }

  var PAGE_TITLE = { overview: '总览', live: '实时事件', timeline: '操作时间线', net: '网络活动', disk: '磁盘读写', mem: '内存', proc: '进程', alerts: '告警', glossary: '知识库' };
  var refreshFns = {};
  function route() {
    var page = (location.hash || '#overview').slice(1);
    if (!PAGE_TITLE[page]) page = 'overview';
    state.page = page;
    document.querySelectorAll('.page').forEach(function (p) { p.classList.toggle('active', p.id === 'page-' + page); });
    document.querySelectorAll('.nav a').forEach(function (a) { a.classList.toggle('active', a.dataset.page === page); });
    document.getElementById('crumbTitle').textContent = PAGE_TITLE[page];
    refreshCurrent(true);
  }
  function refreshCurrent(force) {
    if (refreshFns[state.page]) refreshFns[state.page](force);
    refreshAlertBadge();
  }

  // ---------------- WebSocket ----------------
  var wsDot = null, wsTxt = null, liveEvents = [], livePaused = false, pendingCount = 0;
  function initWS() {
    wsDot = document.getElementById('wsDot'); wsTxt = document.getElementById('backendTxt');
    connect();
  }
  var backoff = 1000;
  function connect() {
    var proto = location.protocol === 'https:' ? 'wss://' : 'ws://';
    var url = proto + location.host + '/ws' + (TOKEN ? '?token=' + encodeURIComponent(TOKEN) : '');
    var ws;
    try { ws = new WebSocket(url); } catch (e) { scheduleReconnect(); return; }
    ws.onopen = function () { backoff = 1000; setDot('ok', '已连接'); };
    ws.onclose = function () { setDot('bad', '连接已断开，重试中…'); scheduleReconnect(); };
    ws.onerror = function () { ws.close(); };
    ws.onmessage = function (ev) {
      var msg; try { msg = JSON.parse(ev.data); } catch (e) { return; }
      if (msg.t === 'events') onLiveEvents(msg.d || []);
      else if (msg.t === 'sys') onSysSnapshot(msg.d);
    };
  }
  function scheduleReconnect() { setTimeout(connect, backoff); backoff = Math.min(backoff * 1.6, 15000); }
  function setDot(cls, txt) { if (wsDot) { wsDot.className = 'dot ' + cls; } if (wsTxt) wsTxt.textContent = txt; }

  function onLiveEvents(events) {
    events.forEach(function (ev) { EV.set(ev.id, ev); });
    if (livePaused) { pendingCount += events.length; updatePausedBanner(); }
    else prependLive(events);
    // overview: keep a small recent-alerts cache fresh
    maybeUpdateOverviewAlerts(events);
  }
  function prependLive(events) {
    var tbody = document.getElementById('liveBody');
    if (!tbody) return;
    var filtered = events.filter(liveFilterMatch);
    if (filtered.length) {
      var wrap = document.getElementById('liveWrap');
      var atTop = wrap.scrollTop < 4;
      tbody.insertAdjacentHTML('afterbegin', filtered.map(evRowHTML).join(''));
      liveEvents = filtered.concat(liveEvents);
      while (tbody.rows.length > 2000) tbody.deleteRow(tbody.rows.length - 1);
      liveEvents = liveEvents.slice(0, 2000);
      document.getElementById('liveEmpty').style.display = tbody.rows.length ? 'none' : '';
      document.getElementById('liveCount').textContent = tbody.rows.length + ' 条';
      if (document.getElementById('chkScroll').checked && atTop) wrap.scrollTop = 0;
    }
  }
  function updatePausedBanner() {
    var b = document.getElementById('pausedBanner');
    b.textContent = '已暂停 · 有 ' + pendingCount + ' 条新事件未显示，点击恢复';
    b.classList.toggle('show', livePaused);
  }

  var sysHistory = [];
  var charts = {};
  function onSysSnapshot(snap) {
    if (!snap) return;
    sysHistory.push(snap);
    if (sysHistory.length > 600) sysHistory.shift();
    if (state.page === 'overview') pushOverviewPoint(snap);
    updateOverviewHeader(snap);
  }

  // ---------------- OVERVIEW ----------------
  function initOverview() {
    charts.cpu = new Charts.LineChart(document.getElementById('chCpu'), { series: [{ key: 'v', color: 'var(--accent-2)', label: 'CPU' }], max: 100, fmt: fmtPct, legend: document.getElementById('legCpu') });
    charts.mem = new Charts.LineChart(document.getElementById('chMem'), { series: [{ key: 'used', color: 'var(--accent)', label: '已用' }], fmt: fmtBytes, legend: document.getElementById('legMem') });
    charts.disk = new Charts.LineChart(document.getElementById('chDisk'), { series: [{ key: 'r', color: 'var(--cyan)', label: '读' }, { key: 'w', color: 'var(--cat-disk)', label: '写' }], fmt: fmtBps, legend: document.getElementById('legDisk') });
    charts.net = new Charts.LineChart(document.getElementById('chNet'), { series: [{ key: 'rx', color: 'var(--green)', label: '收' }, { key: 'tx', color: 'var(--accent-2)', label: '发' }], fmt: fmtBps, legend: document.getElementById('legNet') });
    charts.rate = new Charts.LineChart(document.getElementById('chRate'), { series: [{ key: 'v', color: 'var(--accent)', label: '事件/s' }], fmt: function (v) { return v.toFixed(0); }, legend: document.getElementById('legRate') });
    api('/api/info').then(function (info) {
      document.getElementById('hostInfo').textContent = info.host + ' · ' + info.os + ' ' + (info.kernel || '') + ' · ' + info.arch + ' · ' + (info.backend === 'ebpf' ? 'eBPF 采集' : '轮询采集');
      if (info.warnings && info.warnings.length) {
        var w = document.getElementById('warnBar'); w.textContent = '⚠ ' + info.warnings.join('；'); w.classList.add('show');
      }
    }).catch(function () {});
    api('/api/system/history').then(function (r) {
      (r.samples || []).forEach(pushOverviewPoint);
    }).catch(function () {});
    refreshFns.overview = refreshOverview;
    refreshOverview(true);
  }
  function pushOverviewPoint(s) {
    if (!charts.cpu) return;
    var t = s.ts;
    charts.cpu.push({ t: t, v: { v: s.cpu.total } });
    charts.mem.push({ t: t, v: { used: s.mem.used } });
    var dr = 0, dw = 0; (s.disks || []).forEach(function (d) { dr += d.read_bps; dw += d.write_bps; });
    charts.disk.push({ t: t, v: { r: dr, w: dw } });
    var nr = 0, nt = 0; (s.nets || []).forEach(function (n) { nr += n.rx_bps; nt += n.tx_bps; });
    charts.net.push({ t: t, v: { rx: nr, tx: nt } });
  }
  function updateOverviewHeader(s) {
    var c = document.getElementById('cpuNow'); if (c) c.textContent = fmtPct(s.cpu.total);
    var m = document.getElementById('memNow'); if (m) m.textContent = fmtBytes(s.mem.used) + ' / ' + fmtBytes(s.mem.total);
    var dr = 0, dw = 0; (s.disks || []).forEach(function (d) { dr += d.read_bps; dw += d.write_bps; });
    var dd = document.getElementById('diskNow'); if (dd) dd.textContent = fmtBps(dr + dw);
    var nr = 0, nt = 0; (s.nets || []).forEach(function (n) { nr += n.rx_bps; nt += n.tx_bps; });
    var nn = document.getElementById('netNow'); if (nn) nn.textContent = fmtBps(nr + nt);
  }
  function refreshOverview() {
    api('/api/stats').then(function (st) {
      var cards = [
        { label: '总事件数', value: st.total || 0, c: 'var(--accent)' },
        { label: '高风险', value: (st.by_risk && st.by_risk.high) || 0, c: 'var(--red)' },
        { label: '中风险', value: (st.by_risk && st.by_risk.medium) || 0, c: 'var(--yellow)' },
        { label: '运行进程数', value: (sysHistory.length ? sysHistory[sysHistory.length - 1].procs : '–'), c: 'var(--green)' },
      ];
      document.getElementById('ovCards').innerHTML = cards.map(cardHTML).join('');
      document.getElementById('rateNow').textContent = (st.rate && st.rate.length ? st.rate[st.rate.length - 1] : 0) + '/s';
      if (charts.rate && st.rate) {
        var now = Date.now();
        charts.rate.set(st.rate.map(function (v, i) { return { t: now - (st.rate.length - 1 - i) * 1000, v: { v: v } }; }));
      }
      var top = (st.top_procs || []).slice(0, 8);
      var maxN = Math.max(1, top[0] ? top[0].count : 1);
      document.getElementById('ovTop').innerHTML = top.map(function (p) {
        return '<div class="list-item"><span class="grow">' + esc(p.comm) + ' <span class="dim">#' + p.pid + '</span></span>' +
          '<div class="bar-bg" style="width:80px"><div class="bar-fg" style="width:' + (100 * p.count / maxN) + '%"></div></div>' +
          '<span class="dim" style="width:34px;text-align:right">' + p.count + '</span></div>';
      }).join('') || '<div class="empty">暂无数据</div>';
    }).catch(function () {});
    api('/api/events', { risk: 'medium+', limit: 8 }).then(function (r) {
      lastOverviewAlerts = r.events || [];
      renderOverviewAlerts();
    }).catch(function () {});
  }
  var lastOverviewAlerts = [];
  function renderOverviewAlerts() {
    var el = document.getElementById('ovAlerts');
    if (!el) return;
    el.innerHTML = lastOverviewAlerts.map(function (ev) {
      EV.set(ev.id, ev);
      return '<div class="list-item click" data-id="' + ev.id + '"><span class="risk ' + ev.risk + '">' + RISK_LABEL[ev.risk] + '</span>' +
        '<span class="grow">' + esc(ev.rule_title || ev.plain || ev.title) + '</span><span class="dim">' + fmtAgo(ev.ts) + '</span></div>';
    }).join('') || '<div class="empty">暂无告警，一切正常</div>';
  }
  function maybeUpdateOverviewAlerts(events) {
    var risky = events.filter(function (e) { return e.risk === 'medium' || e.risk === 'high'; });
    if (!risky.length) return;
    lastOverviewAlerts = risky.concat(lastOverviewAlerts).slice(0, 8);
    if (state.page === 'overview') renderOverviewAlerts();
  }
  function cardHTML(c) {
    return '<div class="card"><div class="stripe" style="background:' + c.c + '"></div><div class="label">' + c.label + '</div><div class="value">' + c.value + '</div></div>';
  }

  // ---------------- LIVE ----------------
  function liveFilterMatch(ev) {
    var cat = document.getElementById('fCat').value, risk = document.getElementById('fRisk').value;
    var pid = document.getElementById('fPid').value.trim(), q = document.getElementById('fQ').value.trim().toLowerCase();
    if (cat && ev.cat !== cat) return false;
    if (risk) {
      if (risk === 'high' && ev.risk !== 'high') return false;
      if (risk === 'medium+' && !(ev.risk === 'medium' || ev.risk === 'high')) return false;
      if (risk === 'low+' && ev.risk === 'info') return false;
    }
    if (pid && String(ev.pid) !== pid) return false;
    if (q) {
      var hay = [ev.comm, ev.exe, ev.plain, ev.pro, ev.title, JSON.stringify(ev.fields || {})].join(' ').toLowerCase();
      if (hay.indexOf(q) === -1) return false;
    }
    return true;
  }
  function initLive() {
    document.getElementById('liveHead').innerHTML = evHeadHTML();
    var catSel = document.getElementById('fCat');
    for (var k in CAT) catSel.insertAdjacentHTML('beforeend', '<option value="' + k + '">' + CAT[k].zh + '</option>');
    ['fCat', 'fRisk', 'fPid'].forEach(function (id) { document.getElementById(id).addEventListener('change', reapplyLiveFilter); });
    document.getElementById('fQ').addEventListener('input', debounce(reapplyLiveFilter, 200));
    document.getElementById('btnPause').addEventListener('click', function () {
      livePaused = !livePaused;
      this.textContent = livePaused ? '▶ 继续' : '⏸ 暂停';
      if (!livePaused) { updatePausedBanner(); pendingCount = 0; loadLive(true); }
    });
    document.getElementById('pausedBanner').addEventListener('click', function () {
      livePaused = false; document.getElementById('btnPause').textContent = '⏸ 暂停'; pendingCount = 0; updatePausedBanner(); loadLive(true);
    });
    document.getElementById('btnClear').addEventListener('click', function () {
      liveEvents = []; document.getElementById('liveBody').innerHTML = '';
      document.getElementById('liveEmpty').style.display = ''; document.getElementById('liveCount').textContent = '';
    });
    document.getElementById('btnMore').addEventListener('click', function () {
      var before = liveEvents.length ? liveEvents[liveEvents.length - 1].id : undefined;
      api('/api/events', { before: before, limit: 100 }).then(function (r) {
        var evs = (r.events || []).filter(liveFilterMatch);
        document.getElementById('liveBody').insertAdjacentHTML('beforeend', evs.map(evRowHTML).join(''));
        liveEvents = liveEvents.concat(evs);
        document.getElementById('liveCount').textContent = liveEvents.length + ' 条';
      }).catch(function () {});
    });
    refreshFns.live = function (force) { if (force) loadLive(true); };
    loadLive(true);
  }
  function reapplyLiveFilter() { loadLive(true); }
  function loadLive() {
    api('/api/events', { limit: 150 }).then(function (r) {
      liveEvents = (r.events || []).filter(liveFilterMatch);
      renderEvents(document.getElementById('liveBody'), liveEvents, document.getElementById('liveEmpty'));
      document.getElementById('liveCount').textContent = liveEvents.length + ' 条';
    }).catch(function () {});
  }

  // ---------------- TIMELINE ----------------
  function initTimeline() {
    document.querySelector('#page-timeline thead.evhead').innerHTML = evHeadHTML();
    var tl = new Charts.Timeline(document.getElementById('tlCanvas'), {
      riskColor: function (r) { return r === 'high' ? Charts.cssVar('--red') : Charts.cssVar('--yellow'); },
      onClick: function (hit) {
        var lane = hit.lane, params = { since: hit.since, until: hit.until, limit: 300 };
        if (tlLaneMode === 'cat') params.cat = lane.name; else if (/^\d+$/.test(lane.name)) params.pid = lane.name; else params.q = lane.name;
        document.getElementById('tlSelTitle').textContent = (lane.label || lane.name) + ' · ' + hit.b.n + ' 个事件';
        document.getElementById('tlSelSub').textContent = new Date(hit.since).toLocaleTimeString() + ' – ' + new Date(hit.until).toLocaleTimeString();
        api('/api/events', params).then(function (r) { renderEvents(document.getElementById('tlEvents'), r.events || []); }).catch(function () {});
      },
    });
    var tlLaneMode = 'cat';
    document.getElementById('tlLane').addEventListener('click', function (e) {
      var b = e.target.closest('button[data-v]'); if (!b) return;
      document.querySelectorAll('#tlLane button').forEach(function (x) { x.classList.remove('on'); }); b.classList.add('on');
      tlLaneMode = b.dataset.v; loadTimeline();
    });
    document.getElementById('tlRefresh').addEventListener('click', loadTimeline);
    function loadTimeline() {
      api('/api/timeline', { range: state.range.sec, lane: tlLaneMode }).then(function (d) { tl.set(d); }).catch(function () {});
    }
    refreshFns.timeline = loadTimeline;
    loadTimeline();
  }

  // ---------------- NET ----------------
  function netHeadHTML() { return '<tr><th>协议</th><th>本地地址</th><th>远程地址</th><th class="hide-sm">状态</th><th>进程</th><th>服务</th></tr>'; }
  function initNet() {
    document.getElementById('netHead').innerHTML = netHeadHTML();
    document.querySelector('#page-net thead.evhead').innerHTML = evHeadHTML();
    document.getElementById('netQ').addEventListener('input', debounce(renderConns, 150));
    document.getElementById('netState').addEventListener('change', renderConns);
    document.getElementById('netRefresh').addEventListener('click', function () { loadNet(true); });
    refreshFns.net = loadNet;
    loadNet(true);
  }
  var lastConns = [];
  function loadNet() {
    api('/api/connections').then(function (r) { lastConns = r.conns || []; renderConns(); renderNetCards(); }).catch(function () {});
    api('/api/events', { cat: 'net', limit: 60 }).then(function (r) { renderEvents(document.getElementById('netEvents'), r.events || []); }).catch(function () {});
  }
  function renderNetCards() {
    var est = lastConns.filter(function (c) { return c.state === 'ESTABLISHED'; }).length;
    var listen = lastConns.filter(function (c) { return c.state === 'LISTEN'; }).length;
    var hosts = new Set(lastConns.map(function (c) { return c.remote ? c.remote.split(':')[0] : ''; }).filter(Boolean));
    document.getElementById('netCards').innerHTML = [
      { label: '总连接数', value: lastConns.length, c: 'var(--accent)' },
      { label: '已建立', value: est, c: 'var(--green)' },
      { label: '监听中', value: listen, c: 'var(--cyan)' },
      { label: '远程主机数', value: hosts.size, c: 'var(--accent-2)' },
    ].map(cardHTML).join('');
  }
  function renderConns() {
    var q = document.getElementById('netQ').value.trim().toLowerCase(), st = document.getElementById('netState').value;
    var rows = lastConns.filter(function (c) {
      if (st && c.state !== st) return false;
      if (q && (c.comm + ' ' + c.local + ' ' + c.remote + ' ' + (c.host || '')).toLowerCase().indexOf(q) === -1) return false;
      return true;
    });
    document.getElementById('netCount').textContent = rows.length + ' 条';
    document.getElementById('netBody').innerHTML = rows.map(function (c) {
      return '<tr><td class="nowrap">' + esc(c.proto) + '</td><td class="mono nowrap">' + esc(c.local) + '</td>' +
        '<td class="mono nowrap">' + esc(c.remote || '–') + (c.host ? '<div class="dim" style="font-size:11px">' + esc(c.host) + '</div>' : '') + '</td>' +
        '<td class="hide-sm">' + esc(c.state) + '</td><td class="nowrap">' + esc(c.comm || '') + ' <span class="dim">#' + c.pid + '</span></td>' +
        '<td>' + (c.service ? '<span class="tag">' + esc(c.service) + '</span>' : '') + '</td></tr>';
    }).join('') || '<tr><td colspan="6" class="empty">暂无连接</td></tr>';
  }

  // ---------------- DISK ----------------
  function initDisk() {
    document.getElementById('diskRefresh').addEventListener('click', loadDisk);
    diskScatter = new Charts.Scatter(document.getElementById('diskScatter'), { yfmt: function (v) { return v.toExponential(1); }, empty: '暂无块设备 IO 采样' });
    refreshFns.disk = loadDisk;
    loadDisk();
  }
  var diskScatter;
  function loadDisk() {
    api('/api/disk').then(function (d) {
      document.getElementById('diskCards').innerHTML = (d.devices || []).map(function (dev) {
        var c = dev.util > 80 ? 'var(--red)' : dev.util > 40 ? 'var(--yellow)' : 'var(--green)';
        return cardHTML({ label: dev.name, value: fmtBps(dev.read_bps + dev.write_bps), c: c });
      }).join('') || '<div class="empty">暂无磁盘设备</div>';
      var pts = (d.blocks || []).map(function (b) {
        var col = b.op === 'read' ? 'var(--cyan)' : b.op === 'write' ? 'var(--cat-disk)' : 'var(--yellow)';
        return { x: b.ts, y: b.sector, color: col, r: 2.2, tip: '<b>' + esc(b.dev) + '</b> sector ' + b.sector + '<div class="dim">' + esc(b.comm) + ' · ' + b.op + ' · len ' + b.len + '</div>' };
      });
      diskScatter.set(pts);
      document.getElementById('diskFiles').innerHTML = (d.top_files || []).map(function (f) {
        return '<tr><td class="wrap mono">' + esc(f.path) + '</td><td class="hide-sm">' + esc(f.comm) + '</td><td class="num">' + fmtBytes(f.read_bytes) + '</td><td class="num">' + fmtBytes(f.write_bytes) + '</td><td class="num pro-only">' + f.ops + '</td></tr>';
      }).join('') || '<tr><td colspan="5" class="empty">暂无数据</td></tr>';
      document.getElementById('diskProcs').innerHTML = (d.top_procs || []).map(function (p) {
        return '<tr><td class="num pro-only">' + p.pid + '</td><td>' + esc(p.comm) + '</td><td class="num">' + fmtBytes(p.read_bytes) + '</td><td class="num">' + fmtBytes(p.write_bytes) + '</td></tr>';
      }).join('') || '<tr><td colspan="4" class="empty">暂无数据</td></tr>';
    }).catch(function () {});
  }

  // ---------------- MEMORY ----------------
  var allProcsCache = [];
  function initMem() {
    document.querySelector('#page-mem thead').parentElement; // noop guard
    document.getElementById('memLoad').addEventListener('click', function () {
      var pid = document.getElementById('memPidSel').value;
      if (pid) loadMemProc(Number(pid));
    });
    document.getElementById('memPidQ').addEventListener('input', debounce(fillMemPidSel, 150));
    refreshFns.mem = function (force) { if (force) { loadMemSys(); fillMemPidSel(); } };
    loadMemSys();
    api('/api/processes').then(function (r) { allProcsCache = r.procs || []; fillMemPidSel(); }).catch(function () {});
  }
  function fillMemPidSel() {
    var q = (document.getElementById('memPidQ').value || '').trim().toLowerCase();
    var sel = document.getElementById('memPidSel');
    var cur = sel.value;
    var list = allProcsCache.filter(function (p) { return !q || (p.comm + p.pid).toLowerCase().indexOf(q) !== -1; }).slice(0, 300);
    sel.innerHTML = list.map(function (p) { return '<option value="' + p.pid + '">' + esc(p.comm) + ' (' + p.pid + ')</option>'; }).join('');
    if (cur) sel.value = cur;
  }
  window.goToMemProc = function (pid) { location.hash = '#mem'; setTimeout(function () { loadMemProc(pid); }, 50); };
  function loadMemSys() {
    if (!sysHistory.length) { api('/api/system').then(renderMemSys).catch(function () {}); return; }
    renderMemSys(sysHistory[sysHistory.length - 1]);
  }
  function renderMemSys(s) {
    var m = s.mem, free = Math.max(0, m.total - m.used - m.buffers - m.cached);
    var segs = [
      { v: m.used - m.buffers - m.cached > 0 ? m.used - m.buffers - m.cached : m.used, c: 'var(--accent-2)', l: '已用' },
      { v: m.buffers, c: 'var(--cyan)', l: '缓冲(buffers)' },
      { v: m.cached, c: 'var(--green)', l: '缓存(cached)' },
      { v: free, c: 'var(--bg-hover)', l: '空闲' },
    ];
    document.getElementById('memSysBar').innerHTML = segs.map(function (sg) { return '<div style="width:' + (100 * sg.v / m.total) + '%;background:' + sg.c + '"></div>'; }).join('');
    document.getElementById('memSysLegend').innerHTML = segs.map(function (sg) { return '<span><i style="background:' + sg.c + '"></i>' + sg.l + ' ' + fmtBytes(sg.v) + '</span>'; }).join('');
    document.getElementById('memSysTxt').textContent = fmtBytes(m.used) + ' / ' + fmtBytes(m.total);
    var swapPct = m.swap_total ? 100 * m.swap_used / m.swap_total : 0;
    document.getElementById('memSwapBar').innerHTML = '<div style="width:' + swapPct + '%;background:var(--yellow)"></div>';
  }
  var memStrip;
  function loadMemProc(pid) {
    api('/api/process/' + pid).then(function (d) {
      var p = d.proc;
      document.getElementById('memProcTitle').textContent = '内存地图 — ' + p.comm + ' (pid ' + pid + ')';
      document.getElementById('memProcSub').textContent = 'RSS ' + fmtBytes(p.rss) + ' · VMS ' + fmtBytes(p.vms);
      document.getElementById('memProcPlain').innerHTML = '<p class="page-intro plain-only">' + esc(p.comm) + ' 把自己的内存台面划分成了下面这些区域：</p>';
      var summary = d.maps_summary || {};
      var total = Object.values(summary).reduce(function (a, b) { return a + b; }, 0) || 1;
      document.getElementById('memKindBar').innerHTML = Object.keys(summary).map(function (k) {
        var mk = MEMKIND[k] || { zh: k, color: 'var(--text-faint)' };
        return '<div style="width:' + (100 * summary[k] / total) + '%;background:' + mk.color + '"></div>';
      }).join('');
      document.getElementById('memKindLegend').innerHTML = Object.keys(summary).map(function (k) {
        var mk = MEMKIND[k] || { zh: k, color: 'var(--text-faint)' };
        return '<span><i style="background:' + mk.color + '"></i>' + mk.zh + ' ' + fmtBytes(summary[k]) + '</span>';
      }).join('');
      var maps = (d.maps || []).slice().sort(function (a, b) { return Number(a.start) - Number(b.start); });
      if (!memStrip) memStrip = new Charts.AddrStrip(document.getElementById('memStrip'));
      var prevEnd = null;
      memStrip.set(maps.map(function (m) {
        var s = Number(m.start), e = Number(m.end);
        var gap = prevEnd != null ? Math.max(0, s - prevEnd) : 0; prevEnd = e;
        var mk = MEMKIND[m.kind] || { color: 'var(--text-faint)' };
        return { size: m.size, gap: gap, color: mk.color, tip: '<b>' + m.start + ' - ' + m.end + '</b><div class="dim">' + esc(m.kind) + (m.path ? ' · ' + esc(m.path) : '') + ' · ' + fmtBytes(m.size) + '</div>' };
      }));
      document.getElementById('memMapPro').innerHTML = maps.map(function (m) {
        return '<tr><td class="mono nowrap">' + m.start + '-' + m.end + '</td><td class="num">' + fmtBytes(m.size) + '</td><td class="mono">' + esc(m.perms) + '</td><td class="mono">' + esc(m.offset) + '</td><td class="hide-sm mono">' + esc(m.dev) + '</td><td class="hide-sm">' + esc(m.inode) + '</td><td>' + esc(m.kind) + '</td><td class="wrap mono">' + esc(m.path) + '</td></tr>';
      }).join('') || '<tr><td colspan="8" class="empty">该平台暂不支持内存地图</td></tr>';
      document.getElementById('memMapPlain').innerHTML = maps.filter(function (m) { return m.size > 4096; }).slice(0, 150).map(function (m) {
        var mk = MEMKIND[m.kind] || { zh: m.kind, color: 'var(--text-faint)' };
        return '<div class="memmap-row"><span class="k"><i style="background:' + mk.color + '"></i>' + mk.zh + '</span><span class="desc">' + esc(m.plain || '') + '</span><span class="sz">' + fmtBytes(m.size) + '</span></div>';
      }).join('') || '<div class="empty">该平台暂不支持内存地图</div>';
    }).catch(function () {});
    api('/api/events', { cat: 'memory', pid: pid, limit: 50 }).then(function (r) { renderEvents(document.getElementById('memEvents'), r.events || []); }).catch(function () {});
  }

  // ---------------- PROCESSES ----------------
  var procsAll = [], procCollapsed = new Set(), procSelected = null;
  function initProc() {
    document.getElementById('procRefresh').addEventListener('click', loadProcs);
    document.getElementById('procQ').addEventListener('input', debounce(renderProcTree, 150));
    document.getElementById('procExpand').addEventListener('click', function () { procCollapsed.clear(); renderProcTree(); });
    document.getElementById('procCollapse').addEventListener('click', function () {
      procsAll.forEach(function (p) { if (procsAll.some(function (c) { return c.ppid === p.pid; })) procCollapsed.add(p.pid); });
      renderProcTree();
    });
    refreshFns.proc = loadProcs;
    loadProcs();
  }
  function loadProcs() {
    api('/api/processes').then(function (r) { procsAll = r.procs || []; allProcsCache = procsAll; renderProcTree(); }).catch(function () {});
  }
  function renderProcTree() {
    var q = document.getElementById('procQ').value.trim().toLowerCase();
    var byPpid = {};
    procsAll.forEach(function (p) { (byPpid[p.ppid] = byPpid[p.ppid] || []).push(p); });
    var html = [];
    if (q) {
      procsAll.filter(function (p) { return (p.comm + p.pid + (p.cmdline || '')).toLowerCase().indexOf(q) !== -1; })
        .forEach(function (p) { html.push(procRowHTML(p, 0, false)); });
    } else {
      var roots = procsAll.filter(function (p) { return !procsAll.some(function (o) { return o.pid === p.ppid; }); });
      roots.forEach(function (p) { walk(p, 0); });
    }
    function walk(p, depth) {
      var hasKids = !!(byPpid[p.pid] && byPpid[p.pid].length);
      html.push(procRowHTML(p, depth, hasKids));
      if (hasKids && !procCollapsed.has(p.pid)) byPpid[p.pid].forEach(function (c) { walk(c, depth + 1); });
    }
    document.getElementById('procTree').innerHTML = html.join('') || '<div class="empty">暂无进程</div>';
  }
  function procRowHTML(p, depth, hasKids) {
    var twist = hasKids ? (procCollapsed.has(p.pid) ? '▸' : '▾') : ' ';
    return '<div class="tree-row' + (procSelected === p.pid ? ' sel' : '') + '" data-pid="' + p.pid + '" style="padding-left:' + (6 + depth * 16) + 'px">' +
      '<span class="name"><span class="tw" data-twist="' + p.pid + '">' + twist + '</span>' + esc(p.comm) + '</span>' +
      '<span class="r">' + p.pid + '</span><span class="r hide-sm">' + fmtBytes(p.rss) + '</span><span class="r">' + (p.cpu || 0).toFixed(0) + '%</span></div>';
  }
  document.addEventListener('click', function (e) {
    var tw = e.target.closest('[data-twist]');
    if (tw) { var pid = Number(tw.dataset.twist); if (procCollapsed.has(pid)) procCollapsed.delete(pid); else procCollapsed.add(pid); renderProcTree(); return; }
    var row = e.target.closest('.tree-row[data-pid]');
    if (row) { procSelected = Number(row.dataset.pid); renderProcTree(); loadProcDetail(procSelected); }
  });
  function loadProcDetail(pid) {
    var el = document.getElementById('procDetail');
    el.innerHTML = '<div class="empty">加载中…</div>';
    api('/api/process/' + pid).then(function (d) {
      var p = d.proc;
      var fdKinds = {};
      (d.fds || []).forEach(function (f) { fdKinds[f.kind] = (fdKinds[f.kind] || 0) + 1; });
      el.innerHTML =
        '<h3 class="panel-header"><span>' + esc(p.comm) + ' <span class="dim">#' + pid + '</span></span></h3>' +
        '<div class="plain-box">' + esc(d.plain || '') + '</div>' +
        '<dl class="kv mt">' +
        '<dt>命令行</dt><dd class="mono wrap">' + esc(p.cmdline || p.exe) + '</dd>' +
        '<dt>用户</dt><dd>' + esc(p.user) + ' (uid ' + p.uid + ')</dd>' +
        '<dt>状态</dt><dd>' + esc(p.state) + ' · ' + p.threads + ' 线程</dd>' +
        '<dt>内存</dt><dd>RSS ' + fmtBytes(p.rss) + ' / VMS ' + fmtBytes(p.vms) + '</dd>' +
        '<dt>磁盘 IO</dt><dd>读 ' + fmtBytes(d.io ? d.io.read_bytes : 0) + ' · 写 ' + fmtBytes(d.io ? d.io.write_bytes : 0) + '</dd>' +
        '<dt>句柄</dt><dd>' + Object.keys(fdKinds).map(function (k) { return k + '×' + fdKinds[k]; }).join('，') + '</dd>' +
        '<dt>网络连接</dt><dd>' + (d.conns || []).length + ' 个</dd>' +
        '</dl>' +
        '<div class="toolbar mt"><button class="btn" onclick="goToMemProc(' + pid + ')">查看完整内存地图 →</button></div>' +
        (d.conns && d.conns.length ? '<div class="table-wrap mt" style="max-height:200px;overflow:auto"><table><thead><tr><th>协议</th><th>远程</th><th>状态</th></tr></thead><tbody>' +
          d.conns.map(function (c) { return '<tr><td>' + esc(c.proto) + '</td><td class="mono">' + esc(c.remote || c.local) + '</td><td>' + esc(c.state) + '</td></tr>'; }).join('') + '</tbody></table></div>' : '');
    }).catch(function () { el.innerHTML = '<div class="empty">该进程已退出或无权限查看</div>'; });
  }

  // ---------------- ALERTS ----------------
  function initAlerts() {
    document.querySelector('#page-alerts thead.evhead').innerHTML = evHeadHTML();
    var level = 'medium+';
    document.getElementById('alLevel').addEventListener('click', function (e) {
      var b = e.target.closest('button[data-v]'); if (!b) return;
      document.querySelectorAll('#alLevel button').forEach(function (x) { x.classList.remove('on'); }); b.classList.add('on');
      level = b.dataset.v; loadAlerts();
    });
    document.getElementById('alRefresh').addEventListener('click', loadAlerts);
    function loadAlerts() { api('/api/events', { risk: level, limit: 300 }).then(function (r) { renderEvents(document.getElementById('alBody'), r.events || []); }).catch(function () {}); }
    refreshFns.alerts = loadAlerts;
    loadAlerts();
    api('/api/rules').then(function (r) {
      document.getElementById('rulesCount').textContent = '(' + (r.rules || []).length + ' 条)';
      document.getElementById('rulesBody').innerHTML = (r.rules || []).map(function (ru) {
        return '<tr><td><span class="risk ' + ru.risk + '">' + RISK_LABEL[ru.risk] + '</span></td><td>' + esc(ru.title) + '<div class="dim" style="font-size:11.5px">' + esc(ru.desc) + '</div></td>' +
          '<td class="pro-only mono">' + esc(ru.id) + '</td><td class="pro-only mono">' + esc((ru.types || []).join(',')) + '</td><td class="pro-only mono">' + esc(ru.field) + ' ~ /' + esc(ru.pattern) + '/</td></tr>';
      }).join('');
    }).catch(function () {});
  }
  function refreshAlertBadge() {
    api('/api/stats').then(function (st) {
      var n = ((st.by_risk && st.by_risk.high) || 0) + ((st.by_risk && st.by_risk.medium) || 0);
      var b = document.getElementById('alertBadge');
      if (n > 0) { b.hidden = false; b.textContent = n > 99 ? '99+' : n; } else b.hidden = true;
    }).catch(function () {});
  }

  // ---------------- GLOSSARY ----------------
  function initGlossary() {
    api('/api/glossary').then(function (r) {
      var terms = r.terms || [];
      var cats = Array.from(new Set(terms.map(function (t) { return t.cat; }).filter(Boolean)));
      var sel = document.getElementById('glCat');
      cats.forEach(function (c) { sel.insertAdjacentHTML('beforeend', '<option>' + esc(c) + '</option>'); });
      function render() {
        var q = document.getElementById('glQ').value.trim().toLowerCase(), c = sel.value;
        var list = terms.filter(function (t) {
          if (c && t.cat !== c) return false;
          if (q && (t.term + t.short).toLowerCase().indexOf(q) === -1) return false;
          return true;
        });
        document.getElementById('glGrid').innerHTML = list.map(function (t) {
          return '<div class="gloss"><h3>' + esc(t.term) + (t.cat ? '<span class="tag">' + esc(t.cat) + '</span>' : '') + '</h3>' +
            '<div class="short">' + esc(t.short) + '</div><div class="plain-box">' + esc(t.plain) + '</div>' +
            (t.pro ? '<div class="pro-text mt">' + esc(t.pro) + '</div>' : '') + '</div>';
        }).join('') || '<div class="empty">没有找到相关术语</div>';
      }
      document.getElementById('glQ').addEventListener('input', debounce(render, 150));
      sel.addEventListener('change', render);
      refreshFns.glossary = function (force) { if (force) render(); };
      render();
    }).catch(function () {});
  }

  // ---------------- boot ----------------
  function init() {
    initChrome();
    initOverview();
    initLive();
    initTimeline();
    initNet();
    initDisk();
    initMem();
    initProc();
    initAlerts();
    initGlossary();
    initWS();
    route();
  }
  init();
})();
