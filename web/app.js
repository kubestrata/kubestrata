// kubestrata view: deterministic polar layout of a cluster snapshot. Angle = namespace sector
// (grouped by team), sub-sector by category; radius = age on the selected ring scale.
// Data is untrusted: every string reaches the page through textContent, never innerHTML.
(function () {
  "use strict";
  var T0 = performance.now();
  var body = document.body;
  window.onerror = function (msg, _src, line) {
    body.setAttribute("data-status", "error: " + msg + " (line " + line + ")");
  };
  var CFG = window.KUBESTRATA_VIEW || {};
  var DATA = window.KUBESTRATA || {};
  if (CFG.schema_version !== 1) throw new Error("view schema_version " + CFG.schema_version + " not supported");

  var CATS = ["workload", "network", "config", "storage", "policy", "crd"];
  var CAT_COLOR = { workload: "#4fc3f7", network: "#ba68c8", config: "#ffd54f", storage: "#81c784",
    policy: "#ff8a65", crd: "#90a4ae" };
  var MGR = ["argocd", "flux", "helm", "operator", "kubectl", "other", "unknown"];
  var MGR_COLOR = { argocd: "#ff9800", flux: "#5c6bc0", helm: "#26a69a", operator: "#ab47bc",
    kubectl: "#ef5350", other: "#9e9e9e", unknown: "#616161" };
  var MGR_HINT = { argocd: "GitOps: re-target the destination", flux: "GitOps: re-target the destination",
    helm: "reinstall the release in the target", operator: "migrate the custom resource, not its children",
    kubectl: "applied by hand: export, clean, move to Git", other: "unknown tool: check managedFields",
    unknown: "no manager data" };
  var PALETTE = ["#4fc3f7", "#ffb74d", "#aed581", "#f06292", "#9575cd", "#4db6ac", "#e57373", "#fff176",
    "#7986cb", "#a1887f", "#90a4ae", "#dce775"];
  var DAY = 86400000, CAP = 1095, RR_MIN = 0.05;

  // ---- state (initial values from the config, then from the URL hash)
  var st = { scale: CFG.scale || "30d", axis: CFG.axis || "created", color: "cat", system: false,
    q: "", sel: null, cats: {}, cluster: (CFG.clusters || [])[0] };
  CATS.forEach(function (c) { st.cats[c] = true; });
  (function () {
    var h = location.hash.replace(/^#/, "").split("&");
    h.forEach(function (kv) {
      var p = kv.split("="), k = p[0], v = decodeURIComponent(p[1] || "");
      if (k === "scale" || k === "axis" || k === "color" || k === "cluster") st[k] = v;
      if (k === "system") st.system = v === "1";
      if (k === "sel") st.sel = v;
      if (k === "q") st.q = v;
      if (k === "hide") v.split(",").forEach(function (c) { if (c) st.cats[c] = false; });
    });
  })();
  function saveHash() {
    var parts = ["scale=" + st.scale, "axis=" + st.axis, "color=" + st.color];
    if ((CFG.clusters || []).length > 1) parts.push("cluster=" + encodeURIComponent(st.cluster));
    if (st.system) parts.push("system=1");
    var hide = CATS.filter(function (c) { return !st.cats[c]; });
    if (hide.length) parts.push("hide=" + hide.join(","));
    if (st.q) parts.push("q=" + encodeURIComponent(st.q));
    if (st.sel) parts.push("sel=" + encodeURIComponent(st.sel));
    history.replaceState(null, "", "#" + parts.join("&"));
  }

  // ---- helpers
  function el(tag, attrs, text) {
    var e = document.createElement(tag);
    if (attrs) Object.keys(attrs).forEach(function (k) {
      if (k === "class") e.className = attrs[k]; else e.setAttribute(k, attrs[k]);
    });
    if (text !== undefined) e.textContent = text;
    return e;
  }
  function $(id) { return document.getElementById(id); }
  function fnv(s) {
    var h = 0x811c9dc5;
    for (var i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 0x01000193) >>> 0; }
    return h / 4294967296;
  }
  function ageText(d) {
    if (d < 1) return "today";
    if (d < 2) return "1 day";
    if (d < 60) return Math.floor(d) + " days";
    if (d < 730) return Math.round(d / 30.4) + " months";
    return (d / 365).toFixed(1) + " years";
  }
  function dayLabel(d) { return d >= 365 && d % 365 === 0 ? (d / 365) + "y" : d + "d"; }

  // ---- ring scales: rr(age) in [RR_MIN, 1]; the band past the last ring is compressed (log)
  function scaleOf(name) {
    var rings;
    if (name === "log") {
      return {
        rings: [7, 30, 90, 365, 730, 1095],
        rr: function (a) {
          a = Math.max(0, Math.min(a, CAP));
          return RR_MIN + (1 - RR_MIN) * Math.log1p(a) / Math.log1p(CAP);
        }
      };
    }
    if (name === "custom" && CFG.rings && CFG.rings.length) {
      rings = CFG.rings.slice();
    } else {
      var step = parseInt(name, 10) || 30;
      rings = [];
      for (var i = 1; i <= 12; i++) rings.push(step * i);
    }
    var last = rings[rings.length - 1], cap = Math.max(CAP, last * 2);
    return {
      rings: rings,
      rr: function (a) {
        a = Math.max(0, a);
        if (a >= last) return 0.9 + 0.1 * Math.log1p(Math.min(a, cap) - last) / Math.log1p(cap - last);
        var prev = 0;
        for (var i = 0; i < rings.length; i++) {
          if (a < rings[i]) {
            var f = (i + (a - prev) / (rings[i] - prev)) / rings.length;
            return RR_MIN + (0.9 - RR_MIN) * f;
          }
          prev = rings[i];
        }
        return 0.9;
      },
      ringR: function (i) { return RR_MIN + (0.9 - RR_MIN) * (i + 1) / rings.length; }
    };
  }

  // ---- model of one snapshot
  var models = {};
  function model(id) {
    if (models[id]) return models[id];
    var snap = DATA[id];
    if (!snap) throw new Error("snapshot " + id + " not loaded");
    if (snap.schema_version !== 1) throw new Error("snapshot schema_version " + snap.schema_version + " not supported");
    var ref = Date.parse(snap.collected_at);
    var ns = {};
    (snap.namespaces || []).forEach(function (n) { ns[n.name] = n; });
    var byId = {}, out = {}, inn = {};
    var nodes = (snap.resources || []).map(function (r) {
      var n = { r: r, id: r.id, cat: CATS.indexOf(r.cat) >= 0 ? r.cat : "crd", ns: r.ns || "_",
        h1: fnv(r.id), h2: fnv(r.id + "#") };
      var nsInfo = ns[n.ns];
      n.system = !!(nsInfo && nsInfo.system);
      n.team = r.team || (nsInfo && nsInfo.team) || "";
      n.created = Date.parse(r.created);
      n.changed = r.changed ? Date.parse(r.changed) : null;
      n.text = [r.id, r.name, r.kind, r.app, n.team, r.manager,
        JSON.stringify(r.labels || {}), JSON.stringify(r.facts || {})].join(" ").toLowerCase();
      byId[n.id] = n; out[n.id] = []; inn[n.id] = [];
      return n;
    });
    (snap.edges || []).forEach(function (e) {
      if (byId[e.from] && byId[e.to]) { out[e.from].push(e); inn[e.to].push(e); }
    });
    var teams = {};
    nodes.forEach(function (n) { if (n.team) teams[n.team] = true; });
    var teamList = Object.keys(teams).sort();
    var teamColor = {};
    teamList.forEach(function (t, i) { teamColor[t] = PALETTE[i % PALETTE.length]; });
    models[id] = { id: id, snap: snap, ref: ref, ns: ns, nodes: nodes, byId: byId, out: out, inn: inn,
      teamColor: teamColor, teams: teamList };
    return models[id];
  }

  function visible(n) {
    if (!st.cats[n.cat]) return false;
    if (n.system && !st.system) return false;
    return true;
  }
  function ageOf(m, n) {
    var t = st.axis === "changed" && n.changed ? n.changed : n.created;
    return Math.max(0, (m.ref - t) / DAY);
  }
  function matches(n) { return !st.q || n.text.indexOf(st.q.toLowerCase()) >= 0; }
  function colorOf(m, n) {
    if (st.color === "manager") return MGR_COLOR[n.r.manager] || MGR_COLOR.other;
    if (st.color === "team") return n.team ? m.teamColor[n.team] : "#616161";
    return CAT_COLOR[n.cat];
  }

  // ---- sectors over the namespaces of every shown model (one today, two in compare)
  function sectors(ms) {
    var groups = {};
    // weight of a namespace (and of each category in it) = the largest count among the models
    ms.forEach(function (m) {
      var perNS = {}, perCat = {};
      m.nodes.forEach(function (n) {
        if (!visible(n)) return;
        var g = groups[n.ns] || (groups[n.ns] = { ns: n.ns, team: "", cats: {}, w: 0 });
        if (n.team && !g.team) g.team = n.team;
        perNS[n.ns] = (perNS[n.ns] || 0) + 1;
        var key = n.ns + "\u0000" + n.cat;
        perCat[key] = (perCat[key] || 0) + 1;
        g.cats[n.cat] = Math.max(g.cats[n.cat] || 0, perCat[key]);
      });
      Object.keys(perNS).forEach(function (ns) { groups[ns].w = Math.max(groups[ns].w, perNS[ns]); });
    });
    var list = Object.keys(groups).map(function (k) { return groups[k]; });
    list.sort(function (a, b) {
      if ((a.ns === "_") !== (b.ns === "_")) return a.ns === "_" ? 1 : -1;
      var ta = a.team || "\uffff", tb = b.team || "\uffff";
      return ta < tb ? -1 : ta > tb ? 1 : a.ns < b.ns ? -1 : a.ns > b.ns ? 1 : 0;
    });
    var total = list.reduce(function (s, g) { return s + g.w; }, 0) || 1;
    var minW = total * 0.025, sum = 0;
    list.forEach(function (g) { g.w = Math.max(g.w, minW); sum += g.w; });
    var gap = list.length > 1 ? 0.012 : 0, a = -Math.PI / 2, full = 2 * Math.PI - gap * list.length;
    var map = {};
    list.forEach(function (g) {
      g.a0 = a; g.a1 = a + full * g.w / sum; a = g.a1 + gap;
      var cats = CATS.filter(function (c) { return g.cats[c]; });
      var cs = cats.reduce(function (s, c) { return s + Math.sqrt(g.cats[c]); }, 0) || 1, b = g.a0;
      g.sub = {};
      cats.forEach(function (c) {
        var span = (g.a1 - g.a0) * Math.sqrt(g.cats[c]) / cs;
        g.sub[c] = [b, b + span]; b += span;
      });
      map[g.ns] = g;
    });
    return { list: list, map: map };
  }

  // ---- panes
  var panes = [];
  function makePane(clusterId) {
    var root = el("div", { class: "pane" });
    var cv = el("canvas");
    var title = el("div", { class: "title" });
    root.appendChild(cv); root.appendChild(title);
    $("panes").appendChild(root);
    var p = { root: root, cv: cv, ctx: cv.getContext("2d"), title: title, cluster: clusterId,
      view: { k: 1, x: 0, y: 0 }, pos: [], hov: null };
    bindPane(p);
    return p;
  }
  function layout(p, sec) {
    var m = model(p.cluster), sc = scaleOf(st.scale);
    p.sc = sc;
    p.pos = [];
    m.nodes.forEach(function (n) {
      if (!visible(n)) return;
      var g = sec.map[n.ns], sub = g && g.sub[n.cat];
      if (!sub) return;
      var pad = Math.min(0.02, (sub[1] - sub[0]) * 0.15);
      var ang = sub[0] + pad + n.h1 * Math.max(0, sub[1] - sub[0] - 2 * pad);
      var rr = sc.rr(ageOf(m, n)) * (0.985 + 0.03 * n.h2);
      p.pos.push({ n: n, a: ang, rr: Math.min(rr, 1) });
    });
  }
  function shape(ctx, cat, x, y, s) {
    ctx.beginPath();
    switch (cat) {
      case "network": ctx.moveTo(x, y - s * 1.2); ctx.lineTo(x + s * 1.2, y); ctx.lineTo(x, y + s * 1.2); ctx.lineTo(x - s * 1.2, y); ctx.closePath(); break;
      case "storage": ctx.rect(x - s, y - s, 2 * s, 2 * s); break;
      case "policy": ctx.moveTo(x, y - s * 1.2); ctx.lineTo(x + s * 1.1, y + s * 0.8); ctx.lineTo(x - s * 1.1, y + s * 0.8); ctx.closePath(); break;
      case "config": ctx.arc(x, y, s * 0.65, 0, 6.2832); break;
      default: ctx.arc(x, y, s, 0, 6.2832);
    }
  }
  function sizeOf(n) {
    if (n.cat === "workload") return 3.6 + Math.min(3.4, Math.sqrt(n.r.facts && n.r.facts.pods || 0) * 0.9);
    return n.cat === "config" ? 3.2 : 3.4;
  }
  var sec = null;
  function draw(p) {
    var t0 = performance.now();
    var dpr = window.devicePixelRatio || 1;
    var w = p.cv.clientWidth, h = p.cv.clientHeight;
    p.cv.width = Math.max(1, w * dpr); p.cv.height = Math.max(1, h * dpr);
    var ctx = p.ctx, k = p.view.k;
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, p.cv.width, p.cv.height);
    ctx.setTransform(dpr * k, 0, 0, dpr * k, dpr * (w / 2 + p.view.x), dpr * (h / 2 + p.view.y + 14));
    var R = p.R = Math.max(60, Math.min(w, h - 40) * 0.40);
    var m = model(p.cluster);
    // rings
    ctx.font = (11 / k) + "px -apple-system, Helvetica, Arial, sans-serif";
    ctx.textBaseline = "bottom"; ctx.textAlign = "left";
    var rings = p.sc.rings, every = rings.length > 8 ? 2 : 1;
    rings.forEach(function (d, i) {
      var r = (p.sc.ringR ? p.sc.ringR(i) : p.sc.rr(d)) * R;
      ctx.strokeStyle = "rgba(79,195,247,0.16)"; ctx.lineWidth = 1 / k;
      ctx.setLineDash([4 / k, 5 / k]); ctx.beginPath(); ctx.arc(0, 0, r, 0, 6.2832); ctx.stroke(); ctx.setLineDash([]);
      if (i % every === every - 1 || i === rings.length - 1) {
        ctx.fillStyle = "rgba(139,143,153,0.9)"; ctx.fillText(dayLabel(d), 3 / k, -r - 2 / k);
      }
    });
    ctx.strokeStyle = "#232a35"; ctx.lineWidth = 1 / k;
    ctx.beginPath(); ctx.arc(0, 0, R, 0, 6.2832); ctx.stroke();
    // sectors and namespace labels
    // labels only where the arc has room on screen; zooming in reveals the rest
    var lastLabel = -1e9, firstLabel = null;
    ctx.font = (12 / k) + "px -apple-system, Helvetica, Arial, sans-serif";
    sec.list.forEach(function (g) {
      ctx.strokeStyle = "#2a3340"; ctx.lineWidth = 1 / k;
      ctx.beginPath(); ctx.moveTo(0, 0); ctx.lineTo(R * Math.cos(g.a0), R * Math.sin(g.a0)); ctx.stroke();
      var am = (g.a0 + g.a1) / 2, lx = (R + 14 / k) * Math.cos(am), ly = (R + 14 / k) * Math.sin(am);
      var label = g.ns === "_" ? "cluster-scoped" : g.ns;
      // centered labels (top and bottom) need their width of arc; side labels need a line height
      var need = Math.abs(Math.cos(am)) < 0.45 ? ctx.measureText(label).width * k + 8 : 30;
      if ((am - lastLabel) * R * k < need) return;
      if (firstLabel !== null && (firstLabel + 2 * Math.PI - am) * R * k < need) return;
      if (firstLabel === null) firstLabel = am;
      lastLabel = am;
      var c = Math.cos(am);
      ctx.textAlign = c > 0.2 ? "left" : c < -0.2 ? "right" : "center";
      ctx.textBaseline = "middle";
      ctx.fillStyle = "#c9ccd3"; ctx.font = (12 / k) + "px -apple-system, Helvetica, Arial, sans-serif";
      ctx.fillText(label, lx, ly);
      if (g.team && (g.a1 - g.a0) * R * k > 60) {
        ctx.fillStyle = "#6b7280"; ctx.font = (10 / k) + "px -apple-system, Helvetica, Arial, sans-serif";
        ctx.fillText(g.team, lx, ly + 13 / k);
      }
    });
    // edges of the focused node
    var focus = p.hov || (st.sel && m.byId[st.sel]) || null;
    var at = {};
    p.pos.forEach(function (q) { q.x = q.rr * R * Math.cos(q.a); q.y = q.rr * R * Math.sin(q.a); at[q.n.id] = q; });
    if (focus && at[focus.id]) {
      var f = at[focus.id];
      ctx.lineWidth = 1.3 / k; ctx.strokeStyle = "rgba(255,255,255,0.55)";
      m.out[focus.id].concat(m.inn[focus.id]).forEach(function (e) {
        var o = at[e.from === focus.id ? e.to : e.from];
        if (!o) return;
        ctx.beginPath(); ctx.moveTo(f.x, f.y); ctx.lineTo(o.x, o.y); ctx.stroke();
      });
    }
    // nodes
    var q = st.q, shown = 0;
    p.pos.forEach(function (o) {
      var n = o.n, hit = matches(n);
      var alpha = q ? (hit ? 1 : 0.1) : 1 - 0.5 * o.rr;
      if (focus && (n === focus)) alpha = 1;
      ctx.globalAlpha = alpha;
      ctx.fillStyle = colorOf(m, n);
      shape(ctx, n.cat, o.x, o.y, sizeOf(n) / Math.sqrt(k));
      ctx.fill();
      shown++;
    });
    ctx.globalAlpha = 1;
    if (focus && at[focus.id]) {
      var s = at[focus.id];
      ctx.strokeStyle = "#ffffff"; ctx.lineWidth = 1.6 / k;
      ctx.beginPath(); ctx.arc(s.x, s.y, 9 / k, 0, 6.2832); ctx.stroke();
    }
    p.shown = shown;
    p.drawMs = performance.now() - t0;
  }

  // ---- interaction
  function pick(p, ev) {
    var b = p.cv.getBoundingClientRect(), k = p.view.k;
    var x = (ev.clientX - b.left - b.width / 2 - p.view.x) / k;
    var y = (ev.clientY - b.top - b.height / 2 - p.view.y - 14) / k;
    var best = null, bd = (8 / k) * (8 / k);
    p.pos.forEach(function (o) {
      var dx = o.x - x, dy = o.y - y, d = dx * dx + dy * dy;
      if (d < bd) { bd = d; best = o.n; }
    });
    return best;
  }
  function bindPane(p) {
    var drag = null;
    p.cv.addEventListener("mousedown", function (ev) {
      drag = { x: ev.clientX, y: ev.clientY, vx: p.view.x, vy: p.view.y, moved: false };
      p.cv.classList.add("drag");
    });
    window.addEventListener("mouseup", function (ev) {
      if (!drag) return;
      p.cv.classList.remove("drag");
      if (!drag.moved && ev.target === p.cv) {
        var n = pick(p, ev);
        st.sel = n ? n.id : null; saveHash(); renderPanel(); redraw();
      }
      drag = null;
    });
    p.cv.addEventListener("mousemove", function (ev) {
      if (drag) {
        var dx = ev.clientX - drag.x, dy = ev.clientY - drag.y;
        if (Math.abs(dx) + Math.abs(dy) > 3) drag.moved = true;
        panes.forEach(function (o) { o.view.x = drag.vx + dx; o.view.y = drag.vy + dy; });
        redraw(); return;
      }
      var n = pick(p, ev);
      if (n !== p.hov) { p.hov = n; tip(p, n, ev); redraw(); } else if (n) tip(p, n, ev);
    });
    p.cv.addEventListener("mouseleave", function () { p.hov = null; tip(p, null); redraw(); });
    p.cv.addEventListener("wheel", function (ev) {
      ev.preventDefault();
      var b = p.cv.getBoundingClientRect();
      var f = Math.exp(-ev.deltaY * 0.0015), k = Math.max(0.4, Math.min(25, p.view.k * f));
      var mx = ev.clientX - b.left - b.width / 2, my = ev.clientY - b.top - b.height / 2 - 14;
      panes.forEach(function (o) {
        o.view.x = mx - (mx - o.view.x) * k / o.view.k;
        o.view.y = my - (my - o.view.y) * k / o.view.k;
        o.view.k = k;
      });
      redraw();
    }, { passive: false });
    p.cv.addEventListener("dblclick", function () {
      panes.forEach(function (o) { o.view = { k: 1, x: 0, y: 0 }; }); redraw();
    });
  }
  function tip(p, n, ev) {
    var t = $("tip");
    if (!n) { t.style.display = "none"; return; }
    var m = model(p.cluster);
    t.textContent = "";
    t.appendChild(el("b", null, n.r.kind + " " + (n.r.ns ? n.r.ns + "/" : "") + n.r.name));
    t.appendChild(el("span", null, ageText(ageOf(m, n)) + " · " + n.r.manager + (n.team ? " · " + n.team : "")));
    var mb = $("main").getBoundingClientRect();
    t.style.left = Math.min(ev.clientX - mb.left + 14, mb.width - 340) + "px";
    t.style.top = (ev.clientY - mb.top + 14) + "px";
    t.style.display = "block";
  }

  // ---- side panel
  function fmt(v) {
    if (v === null || v === undefined) return "";
    if (Array.isArray(v)) return v.map(fmt).join(", ");
    if (typeof v === "object") return JSON.stringify(v);
    if (typeof v === "string" && /^\d{4}-\d{2}-\d{2}T/.test(v)) return v.replace("T", " ").replace("Z", " UTC");
    return String(v);
  }
  function row(tbl, k, v, cls) {
    var tr = el("tr");
    tr.appendChild(el("td", null, k));
    var td = el("td", cls ? { class: cls } : null, fmt(v));
    tr.appendChild(td); tbl.appendChild(tr);
    return td;
  }
  function renderPanel() {
    var box = $("panel");
    box.textContent = "";
    var m = model(panes[0].cluster), n = st.sel && m.byId[st.sel];
    if (!n) {
      var s = m.snap, c = s.cluster;
      box.appendChild(el("h2", null, c.context));
      box.appendChild(el("div", { class: "sub" }, c.distro + " · " + c.version + " · " + c.server_host));
      var t = el("table");
      row(t, "cluster id", c.id);
      row(t, "collected", s.collected_at);
      var arch = (c.nodes && c.nodes.arch) || {};
      row(t, "nodes", (c.nodes && c.nodes.count || 0) + (Object.keys(arch).length ? " (" +
        Object.keys(arch).sort().map(function (a) { return a + ": " + arch[a]; }).join(", ") + ")" : ""));
      if (c.distro_signals && c.distro_signals.length) row(t, "distro signals", c.distro_signals);
      row(t, "resources", s.resources.length);
      row(t, "edges", (s.edges || []).length);
      box.appendChild(t);
      box.appendChild(el("h3", null, "managers"));
      var mt = el("table"), mc = {};
      m.nodes.forEach(function (x) { if (visible(x)) mc[x.r.manager] = (mc[x.r.manager] || 0) + 1; });
      MGR.forEach(function (k) { if (mc[k]) row(mt, k + " (" + mc[k] + ")", MGR_HINT[k]); });
      box.appendChild(mt);
      if ((s.errors || []).length) {
        box.appendChild(el("h3", null, "collection errors"));
        var ul = el("ul");
        s.errors.forEach(function (e) { ul.appendChild(el("li", { class: "warn" }, e.kind + (e.ns ? " " + e.ns : "") + ": " + e.reason)); });
        box.appendChild(ul);
      }
      box.appendChild(el("p", { class: "empty" }, "Click a resource for details. Drag to pan, scroll to zoom, double-click to reset."));
      return;
    }
    var r = n.r;
    box.appendChild(el("h2", null, r.kind + " " + r.name));
    box.appendChild(el("div", { class: "sub" }, r.id));
    var t2 = el("table");
    row(t2, "namespace", r.ns || "(cluster-scoped)");
    row(t2, "team", n.team || "—");
    row(t2, "manager", r.manager + " — " + (MGR_HINT[r.manager] || ""));
    if (r.app) row(t2, "app", r.app);
    row(t2, "created", fmt(r.created) + " (" + ageText((m.ref - n.created) / DAY) + ")");
    if (n.changed) row(t2, "changed", fmt(r.changed) + " (" + ageText((m.ref - n.changed) / DAY) + ")");
    if (r.owner) {
      var td = row(t2, "owner", "");
      link(td, r.owner, m);
    }
    box.appendChild(t2);
    var facts = r.facts || {};
    var keys = Object.keys(facts).sort();
    if (keys.length) {
      box.appendChild(el("h3", null, "facts"));
      var ft = el("table");
      keys.forEach(function (k) { row(ft, k, facts[k], k === "end_of_life" ? "eol" : null); });
      box.appendChild(ft);
    }
    var rel = m.out[n.id].map(function (e) { return ["→ " + e.rel, e.to]; })
      .concat(m.inn[n.id].map(function (e) { return ["← " + e.rel, e.from]; }));
    box.appendChild(el("h3", null, "related (" + rel.length + ")"));
    if (!rel.length) box.appendChild(el("div", { class: "empty" }, "none"));
    else {
      var rt = el("table");
      rel.forEach(function (x) { link(row(rt, x[0], ""), x[1], m); });
      box.appendChild(rt);
    }
    var labels = r.labels || {}, lk = Object.keys(labels).sort();
    if (lk.length) {
      box.appendChild(el("h3", null, "labels"));
      var lt = el("table");
      lk.forEach(function (k) { row(lt, k, labels[k]); });
      box.appendChild(lt);
    }
  }
  function link(td, id, m) {
    var a = el("a", null, id.split("/").slice(1).join("/"));
    a.title = id;
    a.addEventListener("click", function () { st.sel = m.byId[id] ? id : null; saveHash(); renderPanel(); redraw(); });
    td.appendChild(a);
  }

  // ---- header controls, legend, footer
  function buildControls() {
    var sel = $("cluster");
    (CFG.clusters || []).forEach(function (id) {
      var s = DATA[id];
      sel.appendChild(el("option", { value: id }, s ? s.cluster.context : id));
    });
    if ((CFG.clusters || []).length < 2) $("clusterGroup").style.display = "none";
    sel.value = st.cluster;
    sel.addEventListener("change", function () {
      st.cluster = sel.value; st.sel = null; panes[0].cluster = st.cluster; refresh();
    });
    var scales = $("scales"), names = ["5d", "10d", "30d", "log"];
    if (CFG.rings && CFG.rings.length) names.push("custom");
    names.forEach(function (s) {
      var b = el("button", { "data-scale": s }, s);
      b.addEventListener("click", function () { st.scale = s; refresh(); });
      scales.appendChild(b);
    });
    document.querySelectorAll("[data-axis]").forEach(function (b) {
      b.addEventListener("click", function () { st.axis = b.getAttribute("data-axis"); refresh(); });
    });
    $("color").addEventListener("change", function () { st.color = $("color").value; refresh(); });
    $("system").addEventListener("click", function () { st.system = !st.system; refresh(); });
    var q = $("q");
    q.value = st.q;
    q.addEventListener("input", function () { st.q = q.value.trim(); saveHash(); redraw(); footer(); });
    document.addEventListener("keydown", function (ev) {
      if (ev.key === "/" && document.activeElement !== q) { ev.preventDefault(); q.focus(); }
      if (ev.key === "Escape") { st.sel = null; saveHash(); renderPanel(); redraw(); }
    });
  }
  function syncControls() {
    document.querySelectorAll("[data-scale]").forEach(function (b) { b.classList.toggle("on", b.getAttribute("data-scale") === st.scale); });
    document.querySelectorAll("[data-axis]").forEach(function (b) { b.classList.toggle("on", b.getAttribute("data-axis") === st.axis); });
    $("color").value = st.color;
    $("system").classList.toggle("on", st.system);
    var box = $("cats"), m = model(panes[0].cluster);
    box.textContent = "";
    CATS.forEach(function (c) {
      var n = m.nodes.filter(function (x) { return x.cat === c && (st.system || !x.system); }).length;
      if (!n) return;
      var b = el("button", { class: "chip" + (st.cats[c] ? " on" : " off") });
      var dot = el("i"); dot.style.background = CAT_COLOR[c];
      b.appendChild(dot); b.appendChild(document.createTextNode(c)); b.appendChild(el("span", { class: "n" }, String(n)));
      b.addEventListener("click", function () { st.cats[c] = !st.cats[c]; refresh(); });
      box.appendChild(b);
    });
  }
  function legend() {
    var box = $("legend"), m = model(panes[0].cluster);
    box.textContent = "";
    var entries;
    if (st.color === "manager") entries = MGR.map(function (k) { return [k, MGR_COLOR[k]]; });
    else if (st.color === "team") entries = m.teams.map(function (t) { return [t, m.teamColor[t]]; }).concat([["no team", "#616161"]]);
    else entries = CATS.map(function (c) { return [c, CAT_COLOR[c]]; });
    box.appendChild(el("div", { class: "h" }, "color: " + st.color));
    var MAX = 10;
    entries.slice(0, MAX).forEach(function (e) {
      var d = el("div"), i = el("i"); i.style.background = e[1];
      d.appendChild(i); d.appendChild(document.createTextNode(e[0])); box.appendChild(d);
    });
    if (entries.length > MAX) box.appendChild(el("div", { class: "h" }, "+ " + (entries.length - MAX) + " more (colors repeat)"));
    box.appendChild(el("div", { class: "h" }, "shape: ● workload ◆ network ● config ■ storage ▲ policy"));
    box.appendChild(el("div", { class: "h" }, "center = newest · edge = oldest (" + st.axis + ")"));
  }
  function footer() {
    var p = panes[0], m = model(p.cluster), c = m.snap.cluster, hits = 0;
    if (st.q) p.pos.forEach(function (o) { if (matches(o.n)) hits++; });
    $("status").textContent = m.snap.resources.length + " resources · " + p.shown + " shown" +
      (st.q ? " · " + hits + " match" : "") + ((m.snap.errors || []).length ? " · " + m.snap.errors.length + " collection errors" : "");
    $("meta").textContent = c.context + " · " + c.distro + " " + c.version + " · collected " + fmt(m.snap.collected_at) +
      " · kubestrata " + (CFG.version || "");
  }
  function titles() {
    panes.forEach(function (p) {
      var c = model(p.cluster).snap.cluster;
      p.title.textContent = "";
      if (panes.length > 1) {
        p.title.appendChild(el("b", null, c.context));
        p.title.appendChild(el("div", null, c.distro + " · " + fmt(model(p.cluster).snap.collected_at)));
      }
    });
  }

  function redraw() { panes.forEach(draw); }
  function refresh() {
    sec = sectors(panes.map(function (p) { return model(p.cluster); }));
    panes.forEach(function (p) { layout(p, sec); });
    syncControls(); legend(); titles(); redraw(); footer(); renderPanel(); saveHash();
  }

  // ---- start
  if (!(CFG.clusters || []).length) throw new Error("no clusters in this view");
  if (!DATA[st.cluster]) st.cluster = CFG.clusters[0];
  buildControls();
  panes.push(makePane(st.cluster));
  window.addEventListener("resize", redraw);
  refresh();
  body.setAttribute("data-status", "ok");
  body.setAttribute("data-shown", String(panes[0].shown));
  body.setAttribute("data-draw-ms", panes[0].drawMs.toFixed(1));
  body.setAttribute("data-total-ms", (performance.now() - T0).toFixed(1));
})();
