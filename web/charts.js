/* ArgusBPF — tiny canvas chart helpers (no deps) */
(function (global) {
  'use strict';

  function tt(key, fallback) { return global.I18N ? global.I18N.t(key) : fallback; }

  function cssVar(name, fallback) {
    var v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    return v || fallback || '#888';
  }

  // ---- BeeEye-style colour field, ported from its CUDA waterfall kernel
  // (BeeEye-agent/cuda/BeeEye_render.cu) to plain Canvas math. The GPU part
  // doesn't carry over — there's no browser API that hands JS a CUDA
  // context, canvas-2d is already SIMD-free CPU work — but the actual
  // visual idea (hue = identity, brightness = magnitude, a ground→hue→hot
  // ramp with a neighbourhood glow and perceptual gamma instead of flat
  // alpha blending) is just colour math and ports directly. Used by
  // Timeline below; same constants as the .cu file so a bucket and a
  // packet-waterfall pixel "mean" the same brightness the same way.
  var BEE_GROUND = [0.043, 0.062, 0.118], BEE_HOT = [1.0, 0.976, 0.929];
  var BEE_CHROMA_BOOST = 1.35, BEE_GAMMA = 0.45, BEE_HOT_START = 0.62;
  var rgbCache = {};
  function cssToRGB01(str) {
    if (rgbCache[str]) return rgbCache[str];
    var c = rgbCache[str] = (function () {
      try {
        if (!cssToRGB01._ctx) {
          var cv = document.createElement('canvas'); cv.width = cv.height = 1;
          cssToRGB01._ctx = cv.getContext('2d', { willReadFrequently: true });
        }
        var ctx = cssToRGB01._ctx;
        ctx.fillStyle = '#000'; ctx.fillStyle = str; // invalid strings are silently ignored, keeping '#000'
        ctx.fillRect(0, 0, 1, 1);
        var d = ctx.getImageData(0, 0, 1, 1).data;
        return [d[0] / 255, d[1] / 255, d[2] / 255];
      } catch (e) { return [0.5, 0.5, 0.5]; }
    })();
    return c;
  }
  function clamp01(v) { return v < 0 ? 0 : v > 1 ? 1 : v; }

  // Traces a smooth curve through pts onto ctx's current path (no
  // beginPath/stroke — the caller does both, so it can keep building the
  // path for a fill afterwards). Quadratic-midpoint technique: each
  // interior point becomes a control point and the curve actually passes
  // through the midpoint to the next sample, landing exactly on the last
  // point — cheap, no spline matrix, and good enough that LineChart's
  // real-time data doesn't read as a jagged connect-the-dots any more.
  function traceSmooth(ctx, pts) {
    if (!pts.length) return;
    ctx.moveTo(pts[0][0], pts[0][1]);
    for (var i = 1; i < pts.length - 1; i++) {
      var mx = (pts[i][0] + pts[i + 1][0]) / 2, my = (pts[i][1] + pts[i + 1][1]) / 2;
      ctx.quadraticCurveTo(pts[i][0], pts[i][1], mx, my);
    }
    if (pts.length > 1) ctx.lineTo(pts[pts.length - 1][0], pts[pts.length - 1][1]);
  }
  // beeEyeShade maps one bucket's hue + 0..1 intensity to a real "rgb(...)"
  // string: saturate the hue, mix up from the dark ground by intensity,
  // gamma-correct so quiet buckets don't vanish, bloom toward warm white
  // once a bucket is hot enough to read as a burst rather than "more lit".
  function beeEyeShade(hueCss, v) {
    var hue = cssToRGB01(hueCss);
    var lum = 0.299 * hue[0] + 0.587 * hue[1] + 0.114 * hue[2];
    var hr = clamp01(lum + (hue[0] - lum) * BEE_CHROMA_BOOST);
    var hg = clamp01(lum + (hue[1] - lum) * BEE_CHROMA_BOOST);
    var hb = clamp01(lum + (hue[2] - lum) * BEE_CHROMA_BOOST);
    v = clamp01(Math.pow(clamp01(v), BEE_GAMMA));
    var r = BEE_GROUND[0] + (hr - BEE_GROUND[0]) * v;
    var g = BEE_GROUND[1] + (hg - BEE_GROUND[1]) * v;
    var b = BEE_GROUND[2] + (hb - BEE_GROUND[2]) * v;
    if (v > BEE_HOT_START) {
      var hot = (v - BEE_HOT_START) / (1 - BEE_HOT_START) * 0.80;
      r += (BEE_HOT[0] - r) * hot; g += (BEE_HOT[1] - g) * hot; b += (BEE_HOT[2] - b) * hot;
    }
    return 'rgb(' + Math.round(clamp01(r) * 255) + ',' + Math.round(clamp01(g) * 255) + ',' + Math.round(clamp01(b) * 255) + ')';
  }
  // beeEyeGlow is the waterfall kernel's neighbourhood gather, collapsed to
  // 1-D (time only — our lanes are independent categories/processes/agents,
  // not adjacent frequency bands, so bleeding a burst *across* lanes the
  // way the kernel bleeds across channels would blur together things that
  // are not actually related). Radius/sigma are scaled down from the
  // kernel's pixel-space values to bucket-space: our buckets are already a
  // coarse 1-per-timeslot sample, not one-per-pixel.
  function beeEyeGlow(norms, i, radius, sigma) {
    var sum = 0, wsum = 0;
    for (var d = -radius; d <= radius; d++) {
      var j = i + d; if (j < 0 || j >= norms.length) continue;
      var w = Math.exp(-(d * d) / (2 * sigma * sigma));
      sum += norms[j] * w; wsum += w;
    }
    return wsum > 0 ? sum / wsum : 0;
  }

  // Accepts a bare custom-property name ("--accent"), a full var()
  // reference ("var(--accent)", as app.js's color dictionaries use —
  // valid in CSS but NOT something Canvas's strokeStyle/fillStyle can
  // parse, so it must be resolved to a real color string here first — or
  // already a literal color/hex, which passes through unchanged.
  function resolveColor(c) {
    if (!c) return c;
    var m = /^var\((--[\w-]+)\s*(?:,[^)]*)?\)$/.exec(c.trim());
    if (m) return cssVar(m[1]);
    if (c.indexOf('--') === 0) return cssVar(c);
    return c;
  }

  function setupCanvas(canvas) {
    var dpr = window.devicePixelRatio || 1;
    var rect = canvas.getBoundingClientRect();
    var w = Math.max(10, rect.width), h = Math.max(10, rect.height);
    if (canvas.width !== Math.round(w * dpr) || canvas.height !== Math.round(h * dpr)) {
      canvas.width = Math.round(w * dpr);
      canvas.height = Math.round(h * dpr);
    }
    var ctx = canvas.getContext('2d');
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    return { ctx: ctx, w: w, h: h };
  }

  function observe(canvas, fn) {
    if (typeof ResizeObserver !== 'undefined') {
      var ro = new ResizeObserver(function () { fn(); });
      ro.observe(canvas.parentElement || canvas);
    } else {
      window.addEventListener('resize', fn);
    }
  }

  function niceMax(v) {
    if (!(v > 0)) return 1;
    var p = Math.pow(10, Math.floor(Math.log10(v)));
    var n = v / p;
    var m = n <= 1 ? 1 : n <= 2 ? 2 : n <= 2.5 ? 2.5 : n <= 5 ? 5 : 10;
    return m * p;
  }

  var tip = null;
  function tooltip() { return tip || (tip = document.getElementById('tooltip')); }
  function showTip(html, x, y) {
    var t = tooltip(); if (!t) return;
    t.innerHTML = html; t.style.display = 'block';
    var r = t.getBoundingClientRect();
    var nx = x + 14, ny = y + 14;
    if (nx + r.width > window.innerWidth - 8) nx = x - r.width - 14;
    if (ny + r.height > window.innerHeight - 8) ny = y - r.height - 14;
    t.style.left = Math.max(4, nx) + 'px'; t.style.top = Math.max(4, ny) + 'px';
  }
  function hideTip() { var t = tooltip(); if (t) t.style.display = 'none'; }

  /* ---------------- LineChart ---------------- */
  // opts: {series:[{key,color(var name or hex),fill}], max: fixed max or null, fmt: fn(v), points: n}
  function LineChart(canvas, opts) {
    this.c = canvas; this.o = opts; this.data = []; // data: [{t, v:{key:val}}]
    var self = this;
    observe(canvas, function () { self.draw(); });
    canvas.addEventListener('mousemove', function (e) { self.hover(e); });
    canvas.addEventListener('mouseleave', function () { self.hoverIdx = null; hideTip(); self.draw(); });
  }
  LineChart.prototype.set = function (data) { this.data = data.slice(-(this.o.points || 600)); this.draw(); };
  LineChart.prototype.push = function (pt) {
    this.data.push(pt);
    var n = this.o.points || 600;
    if (this.data.length > n) this.data.splice(0, this.data.length - n);
    this.draw();
  };
  LineChart.prototype.color = function (s) { return resolveColor(s.color); };
  LineChart.prototype.draw = function () {
    if (!this.c.offsetParent) return; // hidden
    var s = setupCanvas(this.c), ctx = s.ctx, w = s.w, h = s.h;
    var padL = 46, padR = 8, padT = 8, padB = 18;
    var o = this.o, data = this.data, series = o.series;
    ctx.clearRect(0, 0, w, h);
    var max = o.max;
    if (!max) {
      var m = 0;
      for (var i = 0; i < data.length; i++) for (var j = 0; j < series.length; j++) {
        var v = data[i].v[series[j].key]; if (v > m) m = v;
      }
      max = niceMax(m * 1.1);
    }
    var cw = w - padL - padR, ch = h - padT - padB;
    var grid = cssVar('--grid', 'rgba(204,204,220,.08)'), dim = cssVar('--text-faint');
    ctx.font = '11px ' + cssVar('--mono', 'monospace');
    ctx.fillStyle = dim; ctx.strokeStyle = grid; ctx.lineWidth = 1; ctx.setLineDash([4, 4]);
    for (var g = 0; g <= 4; g++) {
      var y = padT + ch - (ch * g / 4);
      ctx.beginPath(); ctx.moveTo(padL, Math.round(y) + .5); ctx.lineTo(w - padR, Math.round(y) + .5); ctx.stroke();
      ctx.textAlign = 'right'; ctx.textBaseline = 'middle';
      ctx.fillText((o.fmt || String)(max * g / 4), padL - 6, y);
    }
    ctx.setLineDash([]);
    var n = o.points || 600;
    this.legend(this.hoverIdx != null ? this.hoverIdx : data.length - 1);
    if (data.length < 2) {
      ctx.textAlign = 'center'; ctx.fillText(tt('chart.waitingData', '等待数据…'), padL + cw / 2, padT + ch / 2); return;
    }
    // time labels
    ctx.textAlign = 'left'; ctx.textBaseline = 'alphabetic';
    var span = (data[data.length - 1].t - data[0].t) / 1000;
    if (span > 0) {
      ctx.fillText('-' + Math.round(span) + 's', padL, h - 4);
      ctx.textAlign = 'right'; ctx.fillText(tt('chart.now', '现在'), w - padR, h - 4);
    }
    var step = cw / (n - 1);
    var x0 = padL + cw - (data.length - 1) * step;
    for (var k = 0; k < series.length; k++) {
      var se = series[k], col = this.color(se);
      var pts = [];
      for (var p = 0; p < data.length; p++) {
        var val = data[p].v[se.key] || 0;
        pts.push([x0 + p * step, padT + ch - Math.min(1, val / max) * ch]);
      }
      ctx.beginPath();
      traceSmooth(ctx, pts);
      ctx.strokeStyle = col; ctx.lineWidth = 1.6; ctx.lineJoin = 'round'; ctx.stroke();
      if (se.fill !== false) {
        ctx.lineTo(x0 + (data.length - 1) * step, padT + ch); ctx.lineTo(x0, padT + ch); ctx.closePath();
        ctx.globalAlpha = .12; ctx.fillStyle = col; ctx.fill(); ctx.globalAlpha = 1;
      }
    }
    if (this.hoverIdx != null && data[this.hoverIdx]) {
      var hx = x0 + this.hoverIdx * step;
      ctx.strokeStyle = cssVar('--text-dim'); ctx.lineWidth = 1; ctx.setLineDash([3, 3]);
      ctx.beginPath(); ctx.moveTo(Math.round(hx) + .5, padT); ctx.lineTo(Math.round(hx) + .5, padT + ch); ctx.stroke();
      if (this.hoverY != null && this.hoverY >= padT && this.hoverY <= padT + ch) {
        ctx.beginPath(); ctx.moveTo(padL, Math.round(this.hoverY) + .5); ctx.lineTo(w - padR, Math.round(this.hoverY) + .5); ctx.stroke();
      }
      ctx.setLineDash([]);
      for (var q = 0; q < series.length; q++) {
        var hv = data[this.hoverIdx].v[series[q].key] || 0;
        ctx.fillStyle = this.color(series[q]); ctx.strokeStyle = cssVar('--bg-panel');
        ctx.beginPath(); ctx.arc(hx, padT + ch - Math.min(1, hv / max) * ch, 3.5, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
      }
    }
    this._geo = { x0: x0, step: step };
  };
  // Grafana-style legend: colour swatch + name + current (or hovered) value
  LineChart.prototype.legend = function (idx) {
    var el = this.o.legend; if (!el) return;
    var o = this.o, d = this.data[idx], self = this;
    el.innerHTML = o.series.map(function (se) {
      var v = d ? (o.fmt || String)(d.v[se.key] || 0) : '–';
      return '<span><i style="background:' + self.color(se) + '"></i>' + (se.label || se.key) + ' <b>' + v + '</b></span>';
    }).join('');
  };
  LineChart.prototype.hover = function (e) {
    if (!this._geo || !this.data.length) return;
    var r = this.c.getBoundingClientRect();
    var idx = Math.round((e.clientX - r.left - this._geo.x0) / this._geo.step);
    if (idx < 0 || idx >= this.data.length) { this.hoverIdx = null; hideTip(); this.draw(); return; }
    this.hoverIdx = idx; this.hoverY = e.clientY - r.top; this.draw();
    var d = this.data[idx], o = this.o, html = '<div class="dim" style="margin-bottom:3px">' + new Date(d.t).toLocaleString() + '</div>';
    for (var k = 0; k < o.series.length; k++) {
      var se = o.series[k];
      html += '<div><span style="color:' + this.color(se) + '">●</span> ' + (se.label || se.key) + ': <b>' + (o.fmt || String)(d.v[se.key] || 0) + '</b></div>';
    }
    showTip(html, e.clientX, e.clientY);
  };

  /* ---------------- Scatter ---------------- */
  // points: [{x(ms), y(number), color, r, tip}]
  function Scatter(canvas, opts) {
    this.c = canvas; this.o = opts || {}; this.pts = [];
    var self = this;
    observe(canvas, function () { self.draw(); });
    canvas.addEventListener('mousemove', function (e) { self.hover(e); });
    canvas.addEventListener('mouseleave', hideTip);
  }
  Scatter.prototype.set = function (pts) { this.pts = pts; this.draw(); };
  Scatter.prototype.draw = function () {
    if (!this.c.offsetParent) return;
    var s = setupCanvas(this.c), ctx = s.ctx, w = s.w, h = s.h, o = this.o;
    var padL = 78, padR = 10, padT = 8, padB = 20, cw = w - padL - padR, ch = h - padT - padB;
    ctx.clearRect(0, 0, w, h);
    ctx.font = '11px ' + cssVar('--mono', 'monospace');
    var dim = cssVar('--text-faint'), grid = cssVar('--grid', 'rgba(204,204,220,.08)');
    var pts = this.pts;
    if (!pts.length) { ctx.fillStyle = dim; ctx.textAlign = 'center'; ctx.fillText((typeof o.empty === 'function' ? o.empty() : o.empty) || tt('empty.noData', '暂无数据'), w / 2, h / 2); this._geo = null; return; }
    var xmin = Infinity, xmax = -Infinity, ymin = Infinity, ymax = -Infinity;
    pts.forEach(function (p) { if (p.x < xmin) xmin = p.x; if (p.x > xmax) xmax = p.x; if (p.y < ymin) ymin = p.y; if (p.y > ymax) ymax = p.y; });
    if (xmax === xmin) { xmax += 1000; xmin -= 1000; }
    if (ymax === ymin) { ymax += 1; ymin = Math.max(0, ymin - 1); }
    ctx.strokeStyle = grid; ctx.fillStyle = dim; ctx.lineWidth = 1;
    for (var g = 0; g <= 4; g++) {
      var y = padT + ch - ch * g / 4, val = ymin + (ymax - ymin) * g / 4;
      ctx.beginPath(); ctx.moveTo(padL, Math.round(y) + .5); ctx.lineTo(w - padR, Math.round(y) + .5); ctx.stroke();
      ctx.textAlign = 'right'; ctx.textBaseline = 'middle';
      ctx.fillText((o.yfmt || String)(val), padL - 6, y);
    }
    ctx.textBaseline = 'alphabetic'; ctx.textAlign = 'left';
    ctx.fillText(new Date(xmin).toLocaleTimeString(), padL, h - 4);
    ctx.textAlign = 'right'; ctx.fillText(new Date(xmax).toLocaleTimeString(), w - padR, h - 4);
    var geo = [];
    for (var i = 0; i < pts.length; i++) {
      var p = pts[i];
      var px = padL + (p.x - xmin) / (xmax - xmin) * cw;
      var py = padT + ch - (p.y - ymin) / (ymax - ymin) * ch;
      var col = resolveColor(p.color);
      ctx.globalAlpha = .75; ctx.fillStyle = col;
      ctx.beginPath(); ctx.arc(px, py, p.r || 2.5, 0, Math.PI * 2); ctx.fill();
      geo.push([px, py, p]);
    }
    ctx.globalAlpha = 1;
    this._geo = geo;
  };
  Scatter.prototype.hover = function (e) {
    if (!this._geo) return;
    var r = this.c.getBoundingClientRect(), mx = e.clientX - r.left, my = e.clientY - r.top;
    var best = null, bd = 64;
    for (var i = 0; i < this._geo.length; i++) {
      var g = this._geo[i], d = (g[0] - mx) * (g[0] - mx) + (g[1] - my) * (g[1] - my);
      if (d < bd) { bd = d; best = g[2]; }
    }
    if (best && best.tip) showTip(best.tip, e.clientX, e.clientY); else hideTip();
  };

  /* ---------------- Timeline swimlanes ---------------- */
  // data: {start,end,bucket,lanes:[{name,label,color,buckets:[{n,risk}]}]}
  function Timeline(canvas, opts) {
    this.c = canvas; this.o = opts || {}; this.d = null; this.sel = null;
    var self = this;
    observe(canvas, function () { self.draw(); });
    canvas.addEventListener('mousemove', function (e) { self.hover(e); });
    canvas.addEventListener('mouseleave', function () { hideTip(); self.hov = null; self.draw(); });
    canvas.addEventListener('click', function (e) {
      var hit = self.hit(e); if (!hit) return;
      self.sel = hit; self.draw();
      if (self.o.onClick) self.o.onClick(hit);
    });
  }
  Timeline.prototype.set = function (d) { this.d = d; this.sel = null; this.resize(); this.draw(); };
  Timeline.prototype.resize = function () {
    var n = this.d && this.d.lanes ? this.d.lanes.length : 0;
    var h = Math.max(160, 30 + n * 26);
    // Grow the canvas itself, not its container — "按进程/按 AI Agent" can
    // have many lanes, and growing the container pushed the whole page
    // (and the click-through detail panel below it) arbitrarily far down.
    // #tlBox caps at a fixed height and scrolls internally instead (see
    // style.css), so the rest of the page — detail panel included — stays
    // put regardless of lane count.
    this.c.style.height = h + 'px';
    this.c.parentElement.style.height = '';
  };
  Timeline.prototype.geo = function () {
    var r = this.c.getBoundingClientRect();
    var padL = Math.min(150, Math.max(90, r.width * .18)), padR = 8, padT = 6, laneH = 26, padB = 22;
    var nb = this.d && this.d.lanes && this.d.lanes[0] ? this.d.lanes[0].buckets.length : 0;
    return { padL: padL, padR: padR, padT: padT, padB: padB, laneH: laneH, nb: nb, bw: nb ? (r.width - padL - padR) / nb : 0, w: r.width, h: r.height };
  };
  Timeline.prototype.draw = function () {
    if (!this.c.offsetParent) return;
    var s = setupCanvas(this.c), ctx = s.ctx, w = s.w, h = s.h;
    ctx.clearRect(0, 0, w, h);
    var d = this.d, dim = cssVar('--text-faint'), text = cssVar('--text-dim'), grid = cssVar('--grid', 'rgba(204,204,220,.08)');
    ctx.font = '12px ' + cssVar('--sans', 'sans-serif');
    if (!d || !d.lanes || !d.lanes.length) { ctx.fillStyle = dim; ctx.textAlign = 'center'; ctx.fillText(tt('chart.noEventsPeriod', '该时间段没有事件'), w / 2, h / 2); return; }
    var g = this.geo(), riskC = this.o.riskColor, maxN = 1;
    d.lanes.forEach(function (l) { l.buckets.forEach(function (b) { if (b.n > maxN) maxN = b.n; }); });
    var lmax = Math.log(maxN + 1);
    for (var li = 0; li < d.lanes.length; li++) {
      var lane = d.lanes[li], y = g.padT + li * g.laneH;
      ctx.fillStyle = text; ctx.textAlign = 'right'; ctx.textBaseline = 'middle';
      var label = lane.label || lane.name;
      if (ctx.measureText(label).width > g.padL - 14) { while (label.length > 2 && ctx.measureText(label + '…').width > g.padL - 14) label = label.slice(0, -1); label += '…'; }
      ctx.fillText(label, g.padL - 8, y + g.laneH / 2);
      ctx.strokeStyle = grid; ctx.beginPath(); ctx.moveTo(g.padL, y + g.laneH + .5); ctx.lineTo(w - g.padR, y + g.laneH + .5); ctx.stroke();
      var base = lane.color ? resolveColor(lane.color) : cssVar('--accent');
      // Per-bucket magnitude, log-normalised to 0..1 — this is the
      // waterfall kernel's `intensity[]` input, computed here instead of
      // uploaded, then glow-gathered across neighbouring time buckets
      // exactly like the kernel gathers across neighbouring pixels.
      var norms = lane.buckets.map(function (b) { return b && b.n ? Math.log(b.n + 1) / lmax : 0; });
      for (var bi = 0; bi < lane.buckets.length; bi++) {
        var b = lane.buckets[bi]; if (!b || !b.n) continue;
        var x = g.padL + bi * g.bw;
        var hue = (b.risk === 'high' || b.risk === 'medium') ? riskC(b.risk) : base;
        var glow = beeEyeGlow(norms, bi, 3, 1.6);
        var v = norms[bi] * 0.95 + glow * 0.85;
        ctx.fillStyle = beeEyeShade(hue, v);
        ctx.fillRect(x + .5, y + 4, Math.max(1, g.bw - 1), g.laneH - 8);
      }
    }
    var mark = this.sel || this.hov;
    [this.hov, this.sel].forEach(function (m, i) {
      if (!m) return;
      ctx.strokeStyle = i ? cssVar('--accent') : text; ctx.lineWidth = i ? 2 : 1;
      ctx.strokeRect(g.padL + m.bi * g.bw + .5, g.padT + m.li * g.laneH + 2, Math.max(2, g.bw - 1), g.laneH - 4);
    });
    void mark;
    // time axis
    ctx.fillStyle = dim; ctx.textBaseline = 'alphabetic'; ctx.font = '11px ' + cssVar('--mono', 'monospace');
    var yb = g.padT + d.lanes.length * g.laneH + 15, ticks = Math.max(2, Math.floor((w - g.padL) / 110));
    for (var t = 0; t <= ticks; t++) {
      var ts = d.start + (d.end - d.start) * t / ticks, tx = g.padL + (w - g.padL - g.padR) * t / ticks;
      ctx.textAlign = t === 0 ? 'left' : t === ticks ? 'right' : 'center';
      var dt = new Date(ts);
      ctx.fillText((d.end - d.start > 86400000 * .9 ? (dt.getMonth() + 1) + '/' + dt.getDate() + ' ' : '') + dt.toTimeString().slice(0, (d.end - d.start) <= 600000 ? 8 : 5), tx, yb);
    }
  };
  Timeline.prototype.hit = function (e) {
    if (!this.d || !this.d.lanes) return null;
    var r = this.c.getBoundingClientRect(), g = this.geo();
    var mx = e.clientX - r.left, my = e.clientY - r.top;
    var li = Math.floor((my - g.padT) / g.laneH), bi = Math.floor((mx - g.padL) / g.bw);
    if (li < 0 || li >= this.d.lanes.length || bi < 0 || bi >= g.nb) return null;
    var lane = this.d.lanes[li], b = lane.buckets[bi];
    var t0 = this.d.start + bi * this.d.bucket;
    return { li: li, bi: bi, lane: lane, b: b, since: t0, until: t0 + this.d.bucket };
  };
  Timeline.prototype.hover = function (e) {
    var hit = this.hit(e);
    this.hov = hit; this.draw();
    if (!hit || !hit.b || !hit.b.n) { hideTip(); return; }
    var f = function (t) { return new Date(t).toLocaleTimeString(); };
    showTip('<b>' + (hit.lane.label || hit.lane.name) + '</b><div class="dim">' + f(hit.since) + ' – ' + f(hit.until) + '</div>' +
      '<div>' + tt('chart.eventsCount', '{n} 个事件').replace('{n}', hit.b.n) + (hit.b.risk && hit.b.risk !== 'info' ? tt('chart.highestRiskSuffix', '，最高风险 ') + '<span class="risk ' + hit.b.risk + '">' + hit.b.risk + '</span>' : '') + '</div>', e.clientX, e.clientY);
  };

  /* ---------------- AddrStrip (memory layout) ---------------- */
  // regions: [{s(BigInt-less number approximations ok), size, color, tip}] sorted by address
  function AddrStrip(canvas) {
    this.c = canvas; this.regs = [];
    var self = this;
    observe(canvas, function () { self.draw(); });
    canvas.addEventListener('mousemove', function (e) { self.hover(e); });
    canvas.addEventListener('mouseleave', hideTip);
  }
  AddrStrip.prototype.set = function (regs) { this.regs = regs; this.draw(); };
  AddrStrip.prototype.draw = function () {
    if (!this.c.offsetParent) return;
    var s = setupCanvas(this.c), ctx = s.ctx, w = s.w, h = s.h;
    ctx.clearRect(0, 0, w, h);
    var regs = this.regs; if (!regs.length) { this._geo = null; return; }
    // width units: log2(size) per region + gap units for holes
    var units = [], total = 0;
    for (var i = 0; i < regs.length; i++) {
      var gapU = regs[i].gap ? Math.min(6, Math.log2(1 + regs[i].gap / 4096) / 4) : 0;
      var u = Math.max(1, Math.log2(1 + regs[i].size / 4096));
      units.push([gapU, u]); total += gapU + u;
    }
    var scale = w / total, x = 0, geo = [], faint = cssVar('--text-faint');
    for (var j = 0; j < regs.length; j++) {
      if (units[j][0]) {
        ctx.fillStyle = faint; ctx.globalAlpha = .15;
        ctx.fillRect(x, h / 2 - 1, units[j][0] * scale, 2);
        ctx.globalAlpha = 1;
        x += units[j][0] * scale;
      }
      var rw = units[j][1] * scale;
      var col = resolveColor(regs[j].color);
      ctx.fillStyle = col; ctx.fillRect(x, 4, Math.max(1, rw - .5), h - 8);
      geo.push([x, x + rw, regs[j]]);
      x += rw;
    }
    this._geo = geo;
  };
  AddrStrip.prototype.hover = function (e) {
    if (!this._geo) return;
    var r = this.c.getBoundingClientRect(), mx = e.clientX - r.left;
    for (var i = 0; i < this._geo.length; i++) {
      if (mx >= this._geo[i][0] && mx < this._geo[i][1]) { showTip(this._geo[i][2].tip, e.clientX, e.clientY); return; }
    }
    hideTip();
  };

  global.Charts = { LineChart: LineChart, Scatter: Scatter, Timeline: Timeline, AddrStrip: AddrStrip, cssVar: cssVar, resolveColor: resolveColor, showTip: showTip, hideTip: hideTip };
})(window);
