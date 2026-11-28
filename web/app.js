// kubestrata view: deterministic polar layout of a cluster snapshot. Angle = namespace sector
// (grouped by team), sub-sector by category; radius = age on the selected ring scale.
// Compare mode puts the source and the target side by side, with sectors computed over both and
// each resource colored by its state (equal, drift, pending, new).
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
  var COMPARE = CFG.mode === "compare";
  var CMP = COMPARE ? window.KUBESTRATA_COMPARE : null;
  if (COMPARE) {
    if (!CMP) throw new Error("comparison data not loaded");
    if (CMP.schema_version !== 1) throw new Error("comparison schema_version " + CMP.schema_version + " not supported");
    if ((CFG.clusters || []).length !== 2 || CMP.source.cluster_id !== CFG.clusters[0] || CMP.target.cluster_id !== CFG.clusters[1]) {
      throw new Error("comparison does not match the clusters of this view");
    }
  }

  var CATS = ["workload", "network", "config", "storage", "policy", "crd"];
  var CAT_COLOR = { workload: "#4fc3f7", network: "#ba68c8", config: "#ffd54f", storage: "#81c784",
    policy: "#ff8a65", crd: "#90a4ae" };
  var MGR = ["argocd", "flux", "helm", "kapp", "terraform", "operator", "kubectl", "console", "platform", "other", "unknown"];
  var MGR_COLOR = { argocd: "#ff9800", flux: "#5c6bc0", helm: "#26a69a", kapp: "#00acc1", terraform: "#c0ca33",
    operator: "#ab47bc", kubectl: "#ef5350", console: "#ec407a", platform: "#8d6e63", other: "#9e9e9e", unknown: "#616161" };
  var MGR_HINT = { argocd: "GitOps: re-target the destination", flux: "GitOps: re-target the destination",
    helm: "reinstall the release in the target", terraform: "IaC: re-target the Terraform code to the new cluster",
    kapp: "Carvel kapp: deploy the same kapp app (or kapp-controller App) to the target",
    operator: "migrate the custom resource, not its children",
    kubectl: "applied by hand: export, clean, move to Git",
    console: "edited in a web console: a manual change, review and codify it before migrating",
    platform: "created by the cluster platform: recreated by the target platform, do not migrate",
    other: "unknown tool: check managedFields", unknown: "no manager data" };
  // a volume the platform provisioned holds data: the target recreates the object, the data must move
  function mgrHint(r) {
    if (r.manager === "platform" && (r.kind === "PersistentVolume" || r.kind === "PersistentVolumeClaim")) {
      return "provisioned by the cluster platform: the target recreates the object, move the data";
    }
    return MGR_HINT[r.manager] || "";
  }
  var STATES = ["equal", "drift", "pending", "new"];
  var STATE_COLOR = { equal: "#66bb6a", drift: "#ffb300", pending: "#ef5350", "new": "#42a5f5" };
  var STATE_HINT = { equal: "in both, same fingerprint", drift: "in both, the fingerprint differs",
    pending: "only in the source: not migrated yet", "new": "only in the target" };
  var PALETTE = ["#4fc3f7", "#ffb74d", "#aed581", "#f06292", "#9575cd", "#4db6ac", "#e57373", "#fff176",
    "#7986cb", "#a1887f", "#90a4ae", "#dce775"];
  var DAY = 86400000, CAP = 1095, RR_MIN = 0.05;
  var ITEM = {};
  // a kind translation (Route→HTTPRoute, DeploymentConfig→Deployment) is one item under both ids
  if (CMP) CMP.items.forEach(function (it) { ITEM[it.id] = it; if (it.target_id) ITEM[it.target_id] = it; });
  // identity of a resource: the source id of its translation pair, else its own id
  function keyOf(id) { return ITEM[id] ? ITEM[id].id : id; }
  // id of the same identity in a model: the pair's other side when the model has not this one
  function idIn(m, id) {
    if (!id || m.byId[id]) return id;
    var it = ITEM[id];
    if (!it || !it.target_id) return id;
    return m.byId[it.id] ? it.id : it.target_id;
  }

  // ---- state (initial values from the config, then from the URL hash)
  // ns: namespace filter ("_" = cluster-scoped), exact match on the resource namespace; q: free-text search
  var st = { scale: CFG.scale || "30d", axis: CFG.axis || "created", color: COMPARE ? "state" : "cat", system: false,
    ns: "", q: "", sel: null, cats: {}, cluster: (CFG.clusters || [])[0] };
  CATS.forEach(function (c) { st.cats[c] = true; });
  (function () {
    var h = location.hash.replace(/^#/, "").split("&");
    h.forEach(function (kv) {
      var p = kv.split("="), k = p[0], v = decodeURIComponent(p[1] || "");
      if (k === "scale" || k === "axis" || k === "color" || k === "cluster") st[k] = v;
      if (k === "system") st.system = v === "1";
      if (k === "ns") st.ns = v;
      if (k === "sel") st.sel = v;
      if (k === "q") st.q = v;
      if (k === "hide") v.split(",").forEach(function (c) { if (c) st.cats[c] = false; });
    });
    if (st.color === "state" && !COMPARE) st.color = "cat";
  })();
  function saveHash() {
    var parts = ["scale=" + st.scale, "axis=" + st.axis, "color=" + st.color];
    if (!COMPARE && (CFG.clusters || []).length > 1) parts.push("cluster=" + encodeURIComponent(st.cluster));
    if (st.system) parts.push("system=1");
    if (st.ns) parts.push("ns=" + encodeURIComponent(st.ns));
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
  // ages are in days; below one day they read in minutes and hours
  function ageText(d) {
    var min = d * 1440;
    if (min < 1) return "under a minute";
    if (min < 2) return "1 minute";
    if (min < 60) return Math.floor(min) + " minutes";
    if (min < 120) return "1 hour";
    if (d < 1) return Math.floor(min / 60) + " hours";
    if (d < 2) return "1 day";
    if (d < 60) return Math.floor(d) + " days";
    if (d < 730) return Math.round(d / 30.4) + " months";
    return (d / 365).toFixed(1) + " years";
  }
  // ring label of an age in days: 30m, 1h30m, 6h, 36h, 5d, 1y, 1.5y
  function ringLabel(d) {
    var min = Math.round(d * 1440), year = 525600;
    if (min < 60) return min + "m";
    if (min >= year && (min * 2) % year === 0) return (min / year) + "y";
    if (min % 1440) return Math.floor(min / 60) + "h" + (min % 60 ? (min % 60) + "m" : "");
    var days = min / 1440;
    return days >= 365 && days % 365 === 0 ? (days / 365) + "y" : days + "d";
  }
  function pct(c) { return c === null || c === undefined ? "—" : Math.round(c * 100) + "%"; }
  // cluster.argocd: what Argo CD manages; managed 0 means installed, nothing attributed
  function argoText(a) {
    var apps = a.applications + " Application" + (a.applications === 1 ? "" : "s") +
      (a.remote ? ", " + a.remote + " deploying to other clusters" : "");
    if (!a.managed) return "installed, no managed objects found (" + apps + ")";
    return a.managed + " objects managed" + (a.from_status ? ", " + a.from_status + " found only in Application status" : "") +
      " (" + apps + ")";
  }
  // {"c6a.xlarge": 3, "m6a.large": 1} → "c6a.xlarge: 3, m6a.large: 1"
  function counts(o) { return Object.keys(o).sort().map(function (k) { return k + ": " + o[k]; }).join(", "); }

  // ---- ring scales: rr(age) in [RR_MIN, 1]; the band past the last ring is compressed (log)
  var SCALES = ["30m", "1h", "5d", "10d", "30d", "years", "log", "auto"];
  // years: for long-lived clusters, where most resources are older than a year
  var YEARS = [30, 90, 180, 365, 730, 1095, 1460, 1825, 2555, 3650];
  // ring step of a linear scale ("30m", "1h", "5d") in days; 0 when the name is not one
  function stepOf(name) {
    var p = /^(\d+)([mhd])$/.exec(name || "");
    return p ? parseInt(p[1], 10) * { m: 1, h: 60, d: 1440 }[p[2]] / 1440 : 0;
  }
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
    var auto = CFG.auto_ring_minutes && CFG.auto_ring_minutes[st.axis];
    if (name === "custom" && CFG.ring_minutes && CFG.ring_minutes.length) {
      rings = CFG.ring_minutes.map(function (m) { return m / 1440; });
    } else if (name === "years") {
      rings = YEARS.slice();
    } else if (name === "auto" && auto && auto.length) {
      // computed by kubestrata at quantiles of the resource ages of this view, per axis
      rings = auto.map(function (m) { return m / 1440; });
    } else {
      var step = stepOf(name) || 30;
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
  // teams of every snapshot in the view: in compare mode a team keeps its color on both sides
  function teamsOf(ids) {
    var teams = {};
    ids.forEach(function (id) {
      var snap = DATA[id], ns = {};
      if (!snap) return;
      (snap.namespaces || []).forEach(function (n) { ns[n.name] = n; });
      (snap.resources || []).forEach(function (r) {
        var t = r.team || (ns[r.ns || "_"] && ns[r.ns || "_"].team) || "";
        if (t) teams[t] = true;
      });
    });
    return Object.keys(teams).sort();
  }
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
    var teamList;
    if (COMPARE) teamList = teamsOf(CFG.clusters);
    else {
      var teams = {};
      nodes.forEach(function (n) { if (n.team) teams[n.team] = true; });
      teamList = Object.keys(teams).sort();
    }
    var teamColor = {};
    teamList.forEach(function (t, i) { teamColor[t] = PALETTE[i % PALETTE.length]; });
    models[id] = { id: id, snap: snap, ref: ref, ns: ns, nodes: nodes, byId: byId, out: out, inn: inn,
      teamColor: teamColor, teams: teamList };
    return models[id];
  }

  function visible(n) {
    if (!st.cats[n.cat]) return false;
    if (n.system && !st.system) return false;
    if (st.ns && n.ns !== st.ns) return false;
    return true;
  }
  function ageOf(m, n) {
    var t = st.axis === "changed" && n.changed ? n.changed : n.created;
    return Math.max(0, (m.ref - t) / DAY);
  }
  // ---- search (q): terms split on commas and spaces, "quoted text" is one term. Free terms match
  // anywhere in the resource text (id, name, kind, app, team, manager, labels, facts) and any of
  // them is enough (OR); ns:, kind:, team:, mgr: and name: match that field exactly, * as a
  // wildcard, OR within a field and AND across fields; -term excludes what it matches. One term
  // reads as before: a substring of the text.
  var FIELDS = { ns: true, kind: true, team: true, mgr: true, name: true };
  var query = null, queryText = null;
  function parseQuery(q) {
    var out = { any: [], fields: {}, not: [], empty: true }, re = /(-?)(?:([A-Za-z]+):)?(?:"([^"]*)"?|([^\s,"]+))/g, mt;
    while ((mt = re.exec(q)) !== null) {
      var f = mt[2] ? mt[2].toLowerCase() : "", v = mt[3] !== undefined ? mt[3] : mt[4] || "";
      if (f && !FIELDS[f]) { v = mt[2] + ":" + v; f = ""; } // an image tag or a label, not a field
      v = v.toLowerCase();
      if (!v || (!mt[1] && !f && v === "-")) continue; // a lone - while typing -term
      var t = { f: f, v: v, glob: f && v.indexOf("*") >= 0 ? globRE(v) : null };
      out.empty = false;
      if (mt[1]) out.not.push(t);
      else if (f) (out.fields[f] = out.fields[f] || []).push(t);
      else out.any.push(t);
    }
    return out;
  }
  function globRE(v) {
    return new RegExp("^" + v.split("*").map(function (p) { return p.replace(/[.+?^${}()|[\]\\]/g, "\\$&"); }).join(".*") + "$");
  }
  function fieldOf(n, f) {
    switch (f) {
      case "ns": return n.ns;
      case "kind": return n.r.kind;
      case "team": return n.team;
      case "mgr": return n.r.manager;
      default: return n.r.name;
    }
  }
  function termHits(n, t) {
    if (!t.f) return n.text.indexOf(t.v) >= 0;
    var v = String(fieldOf(n, t.f) || "").toLowerCase();
    return t.glob ? t.glob.test(v) : v === t.v;
  }
  function matches(n) {
    if (!st.q) return true;
    if (queryText !== st.q) { query = parseQuery(st.q); queryText = st.q; }
    var q = query;
    if (q.empty) return true;
    for (var i = 0; i < q.not.length; i++) if (termHits(n, q.not[i])) return false;
    if (q.any.length && !q.any.some(function (t) { return termHits(n, t); })) return false;
    return Object.keys(q.fields).every(function (f) {
      return q.fields[f].some(function (t) { return termHits(n, t); });
    });
  }
  function stateOf(id) { return ITEM[id] ? ITEM[id].state : ""; }
  function colorOf(m, n) {
    if (st.color === "state") return STATE_COLOR[stateOf(n.id)] || "#616161";
    if (st.color === "manager") return MGR_COLOR[n.r.manager] || MGR_COLOR.other;
    if (st.color === "team") return n.team ? m.teamColor[n.team] : "#616161";
    return CAT_COLOR[n.cat];
  }
  // states of the visible resources, each identity counted once
  function stateCounts() {
    var seen = {}, c = { equal: 0, drift: 0, pending: 0, "new": 0 };
    panes.forEach(function (p) {
      model(p.cluster).nodes.forEach(function (n) {
        if (!visible(n) || seen[keyOf(n.id)]) return;
        seen[keyOf(n.id)] = true;
        var s = stateOf(n.id);
        if (s) c[s]++;
      });
    });
    return c;
  }

  // ---- sectors over the namespaces of every shown model (one, or two in compare)
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
  var hov = null; // id under the pointer, highlighted in every pane
  function makePane(clusterId) {
    var root = el("div", { class: "pane" });
    var cv = el("canvas");
    var title = el("div", { class: "title" });
    root.appendChild(cv); root.appendChild(title);
    $("panes").appendChild(root);
    var p = { root: root, cv: cv, ctx: cv.getContext("2d"), title: title, cluster: clusterId,
      view: { k: 1, x: 0, y: 0 }, pos: [] };
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
    // narrow panes (compare) keep room for the namespace labels outside the circle
    var R = p.R = Math.max(60, Math.min(w * 0.36, (h - 40) * 0.40));
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
        ctx.fillStyle = "rgba(139,143,153,0.9)"; ctx.fillText(ringLabel(d), 3 / k, -r - 2 / k);
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
    var fid = idIn(m, hov || st.sel);
    var focus = (fid && m.byId[fid]) || null;
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
      var n = pick(p, ev), id = n ? n.id : null;
      if (id !== hov) { hov = id; tip(p, n, ev); redraw(); } else if (n) tip(p, n, ev);
    });
    p.cv.addEventListener("mouseleave", function () { hov = null; tip(p, null); redraw(); });
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
    if (COMPARE) {
      ["source", "target"].forEach(function (label, i) {
        var mi = model(panes[i].cluster), x = mi.byId[idIn(mi, n.id)];
        if (x) t.appendChild(el("span", { class: "ln" }, label + ": " + x.r.kind + " · " + ageText(ageOf(mi, x)) + " · " + x.r.manager));
      });
      var it = ITEM[n.id];
      if (it) {
        var s = el("span", { class: "ln" }, it.state + (it.translation ? " (" + it.translation + ")" : "") +
          (it.diff && it.diff.length ? ": " + it.diff.map(function (d) { return d.fact; }).join(", ") : ""));
        s.style.color = STATE_COLOR[it.state];
        t.appendChild(s);
      }
    } else {
      t.appendChild(el("span", null, ageText(ageOf(m, n)) + " · " + n.r.manager + (n.team ? " · " + n.team : "")));
    }
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
  // three columns: label, source value, target value ("—" where a side has nothing)
  function dash(v) { var s = fmt(v); return s === "" ? "—" : s; }
  function row3(tbl, k, a, b, cls) {
    var tr = el("tr");
    tr.appendChild(el("td", null, k));
    var ta = el("td", cls ? { class: cls } : null, dash(a)), tb = el("td", cls ? { class: cls } : null, dash(b));
    tr.appendChild(ta); tr.appendChild(tb); tbl.appendChild(tr);
    return [ta, tb];
  }
  function table3() {
    var t = el("table", { class: "two" }), tr = el("tr", { class: "hd" });
    tr.appendChild(el("td")); tr.appendChild(el("td", null, "source")); tr.appendChild(el("td", null, "target"));
    t.appendChild(tr);
    return t;
  }
  function renderPanel() {
    if (COMPARE) { renderComparePanel(); return; }
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
      row(t, "nodes", (c.nodes && c.nodes.count || 0) + (Object.keys(arch).length ? " (" + counts(arch) + ")" : ""));
      if (c.nodes && c.nodes.instance_types) row(t, "instance types", counts(c.nodes.instance_types));
      if (c.nodes && c.nodes.capacity_types) row(t, "capacity types", counts(c.nodes.capacity_types));
      if (c.distro_signals && c.distro_signals.length) row(t, "distro signals", c.distro_signals);
      if (c.argocd) row(t, "argo cd", argoText(c.argocd), c.argocd.managed ? null : "eol");
      row(t, "resources", s.resources.length);
      row(t, "edges", (s.edges || []).length);
      if (skippedOf(s).length) row(t, "skipped kinds", skippedOf(s).join(", ") + " (collect --skip-kinds, --no-secrets)", "eol");
      box.appendChild(t);
      box.appendChild(el("h3", null, "managers"));
      var mt = el("table"), mc = {}, raw = {};
      m.nodes.forEach(function (x) {
        if (!visible(x)) return;
        mc[x.r.manager] = (mc[x.r.manager] || 0) + 1;
        var rn = x.r.manager === "other" && x.r.facts && x.r.facts.manager_raw;
        if (rn) raw[rn] = (raw[rn] || 0) + 1;
      });
      MGR.forEach(function (k) { if (mc[k]) row(mt, k + " (" + mc[k] + ")", MGR_HINT[k]); });
      // the tools behind "other", by the field manager that created the objects
      var top = Object.keys(raw).sort(function (a, b) { return raw[b] - raw[a] || (a < b ? -1 : 1); }).slice(0, 5);
      if (top.length) row(mt, "other, top field managers", top.map(function (k) { return k + " (" + raw[k] + ")"; }).join(", "));
      box.appendChild(mt);
      errors(box, s, "collection errors");
      box.appendChild(el("p", { class: "empty" }, "Click a resource for details. Drag to pan, scroll to zoom, double-click to reset."));
      return;
    }
    var r = n.r;
    box.appendChild(el("h2", null, r.kind + " " + r.name));
    box.appendChild(el("div", { class: "sub" }, r.id));
    var t2 = el("table");
    row(t2, "namespace", r.ns || "(cluster-scoped)");
    row(t2, "team", n.team || "—");
    row(t2, "manager", r.manager + " — " + mgrHint(r));
    if (r.app) row(t2, "app", r.app);
    row(t2, "created", fmt(r.created) + " (" + ageText((m.ref - n.created) / DAY) + ")");
    if (n.changed) row(t2, "changed", fmt(r.changed) + " (" + ageText((m.ref - n.changed) / DAY) + ")");
    if (r.owner) {
      var td = row(t2, "owner", "");
      link(td, r.owner);
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
    related(box, m, n, "related");
    var labels = r.labels || {}, lk = Object.keys(labels).sort();
    if (lk.length) {
      box.appendChild(el("h3", null, "labels"));
      var lt = el("table");
      lk.forEach(function (k) { row(lt, k, labels[k]); });
      box.appendChild(lt);
    }
  }
  // kinds the collection left out on purpose (collect --skip-kinds, --no-secrets)
  function skippedOf(s) { return (s.scope && s.scope.skipped_kinds) || []; }
  function errors(box, s, title) {
    if (!(s.errors || []).length) return;
    box.appendChild(el("h3", null, title));
    var ul = el("ul");
    s.errors.forEach(function (e) { ul.appendChild(el("li", { class: "warn" }, e.kind + (e.ns ? " " + e.ns : "") + ": " + e.reason)); });
    box.appendChild(ul);
  }
  function related(box, m, n, title) {
    var rel = m.out[n.id].map(function (e) { return ["→ " + e.rel, e.to]; })
      .concat(m.inn[n.id].map(function (e) { return ["← " + e.rel, e.from]; }));
    box.appendChild(el("h3", null, title + " (" + rel.length + ")"));
    if (!rel.length) box.appendChild(el("div", { class: "empty" }, "none"));
    else {
      var rt = el("table");
      rel.forEach(function (x) { link(row(rt, x[0], ""), x[1]); });
      box.appendChild(rt);
    }
  }
  function stateLine(state, text) {
    var d = el("div", { class: "state" }), i = el("i");
    i.style.background = STATE_COLOR[state] || "#616161";
    d.appendChild(i); d.appendChild(document.createTextNode(text));
    return d;
  }
  function renderComparePanel() {
    var box = $("panel");
    box.textContent = "";
    var ms = panes.map(function (p) { return model(p.cluster); });
    var a = st.sel && ms[0].byId[idIn(ms[0], st.sel)], b = st.sel && ms[1].byId[idIn(ms[1], st.sel)];
    if (!a && !b) {
      var sa = ms[0].snap, sb = ms[1].snap, ca = sa.cluster, cb = sb.cluster;
      box.appendChild(el("h2", null, ca.context + " → " + cb.context));
      box.appendChild(el("div", { class: "sub" }, "resources paired by group/Kind/namespace/name"));
      var t = table3();
      row3(t, "cluster id", ca.id, cb.id);
      row3(t, "distro", ca.distro + " " + ca.version, cb.distro + " " + cb.version);
      row3(t, "collected", sa.collected_at, sb.collected_at);
      row3(t, "nodes", ca.nodes && ca.nodes.count || 0, cb.nodes && cb.nodes.count || 0);
      ["instance_types", "capacity_types"].forEach(function (k) {
        var a = ca.nodes && ca.nodes[k], b = cb.nodes && cb.nodes[k];
        if (a || b) row3(t, k.replace("_", " "), a && counts(a), b && counts(b));
      });
      if (ca.argocd || cb.argocd) row3(t, "argo cd", ca.argocd && argoText(ca.argocd), cb.argocd && argoText(cb.argocd));
      row3(t, "resources", sa.resources.length, sb.resources.length);
      if (skippedOf(sa).length || skippedOf(sb).length) {
        row3(t, "skipped kinds", skippedOf(sa).join(", ") || "—", skippedOf(sb).join(", ") || "—", "eol");
      }
      box.appendChild(t);
      box.appendChild(el("h3", null, "states (shown resources)"));
      var c = stateCounts();
      STATES.forEach(function (s) { box.appendChild(stateLine(s, s + " " + c[s] + " — " + STATE_HINT[s])); });
      box.appendChild(el("h3", null, "coverage"));
      var ct = el("table");
      row(ct, "total", pct(CMP.totals.coverage) + " of the source is in the target (equal + drift), system namespaces excluded");
      box.appendChild(ct);
      errors(box, sa, "collection errors (source)");
      errors(box, sb, "collection errors (target)");
      box.appendChild(el("p", { class: "empty" }, "Hover a resource to find its pair, click for details. Click a row of the table below to show only its namespace, again to clear. Drag to pan, scroll to zoom, double-click to reset."));
      return;
    }
    var n = a || b, r = n.r, it = ITEM[st.sel] || { state: a && b ? "equal" : a ? "pending" : "new" };
    box.appendChild(el("h2", null, r.kind + " " + r.name));
    box.appendChild(el("div", { class: "sub" }, r.id));
    box.appendChild(stateLine(it.state, it.state + " — " + STATE_HINT[it.state]));
    if (it.translation) {
      var tr = el("div", { class: "translation" }, "kind translation " + it.translation + ": the target serves " +
        it.id.split("/").slice(1).join("/") + " as " + it.target_id.split("/").slice(1).join("/") +
        "; compared on the facts both kinds express");
      box.appendChild(tr);
    }
    var ra = a ? a.r : {}, rb = b ? b.r : {};
    var t2 = table3();
    row3(t2, "namespace", a && (ra.ns || "(cluster-scoped)"), b && (rb.ns || "(cluster-scoped)"));
    row3(t2, "team", a && a.team, b && b.team);
    row3(t2, "manager", ra.manager, rb.manager);
    if (ra.app || rb.app) row3(t2, "app", ra.app, rb.app);
    row3(t2, "created", a && fmt(ra.created) + " (" + ageText((ms[0].ref - a.created) / DAY) + ")",
      b && fmt(rb.created) + " (" + ageText((ms[1].ref - b.created) / DAY) + ")");
    if ((a && a.changed) || (b && b.changed)) {
      row3(t2, "changed", a && a.changed && fmt(ra.changed) + " (" + ageText((ms[0].ref - a.changed) / DAY) + ")",
        b && b.changed && fmt(rb.changed) + " (" + ageText((ms[1].ref - b.changed) / DAY) + ")");
    }
    if (ra.owner || rb.owner) {
      var cells = row3(t2, "owner", "", "");
      [ra.owner, rb.owner].forEach(function (o, i) { if (o) { cells[i].textContent = ""; link(cells[i], o); } });
    }
    box.appendChild(t2);
    var diff = it.diff || [], differs = {};
    if (it.state === "drift") {
      box.appendChild(el("h3", null, "differences (" + diff.length + ")"));
      if (!diff.length) box.appendChild(el("div", { class: "empty" }, "the fingerprint differs; no fact detail"));
      else {
        var dt = table3();
        diff.forEach(function (d) { differs[d.fact] = true; row3(dt, d.fact, d.source, d.target, "diff"); });
        box.appendChild(dt);
      }
    }
    var fa = ra.facts || {}, fb = rb.facts || {}, keys = {};
    Object.keys(fa).concat(Object.keys(fb)).forEach(function (k) { keys[k] = true; });
    var kl = Object.keys(keys).sort();
    if (kl.length) {
      box.appendChild(el("h3", null, "facts"));
      var ft = table3();
      kl.forEach(function (k) { row3(ft, k, a ? fa[k] : undefined, b ? fb[k] : undefined, differs[k] ? "diff" : null); });
      box.appendChild(ft);
    }
    if (a) related(box, ms[0], a, "related in the source");
    if (b) related(box, ms[1], b, "related in the target");
  }
  function link(td, id) {
    var a = el("a", null, id.split("/").slice(1).join("/"));
    a.title = id;
    a.addEventListener("click", function () {
      st.sel = panes.some(function (p) { return model(p.cluster).byId[id]; }) ? id : null;
      saveHash(); renderPanel(); redraw();
    });
    td.appendChild(a);
  }

  // ---- coverage table (compare mode), under the panes
  function coverage() {
    var box = $("coverage");
    box.textContent = "";
    var t = el("table"), hd = el("tr", { class: "hd" });
    ["namespace", "team", "source", "target", "equal", "drift", "pending", "new", "coverage"].forEach(function (h) {
      hd.appendChild(el("td", null, h));
    });
    t.appendChild(hd);
    var rows = CMP.namespaces.filter(function (s) { return st.system || !s.system; });
    rows.concat([CMP.totals]).forEach(function (s) {
      var total = s === CMP.totals;
      var tr = el("tr", { class: total ? "total" : (st.ns === s.ns ? "on" : "") });
      tr.appendChild(el("td", null, total ? "total" + (st.system ? " (system namespaces excluded)" : "") : s.ns === "_" ? "cluster-scoped" : s.ns));
      tr.appendChild(el("td", null, s.team || ""));
      [s.source, s.target, s.equal, s.drift, s.pending, s["new"]].forEach(function (v) { tr.appendChild(el("td", { class: "n" }, String(v))); });
      var td = el("td", { class: "cov" }), bar = el("span", { class: "bar" }), fill = el("i");
      var c = s.coverage;
      fill.style.width = (c === null || c === undefined ? 0 : Math.round(c * 100)) + "%";
      fill.style.background = c >= 0.75 ? STATE_COLOR.equal : c >= 0.25 ? STATE_COLOR.drift : STATE_COLOR.pending;
      bar.appendChild(fill); td.appendChild(bar); td.appendChild(el("span", null, pct(c)));
      tr.appendChild(td);
      if (!total) {
        tr.title = st.ns === s.ns ? "clear the namespace filter" :
          "show only " + (s.ns === "_" ? "cluster-scoped resources" : "namespace " + s.ns);
        tr.addEventListener("click", function () { st.ns = st.ns === s.ns ? "" : s.ns; refresh(); });
      }
      t.appendChild(tr);
    });
    box.appendChild(t);
  }

  // ---- header controls, legend, footer
  function buildControls() {
    var sel = $("cluster");
    (CFG.clusters || []).forEach(function (id) {
      var s = DATA[id];
      sel.appendChild(el("option", { value: id }, s ? s.cluster.context : id));
    });
    if (COMPARE || (CFG.clusters || []).length < 2) $("clusterGroup").style.display = "none";
    sel.value = st.cluster;
    sel.addEventListener("change", function () {
      st.cluster = sel.value; st.sel = null; panes[0].cluster = st.cluster; refresh();
    });
    if (COMPARE) {
      $("color").insertBefore(el("option", { value: "state" }, "state"), $("color").firstChild);
      $("main").classList.add("compare");
      document.querySelector("header h1").textContent = "kubestrata · compare";
      document.title = "kubestrata · " + CMP.source.context + " → " + CMP.target.context;
    }
    var scales = $("scales"), names = SCALES.slice();
    if (CFG.ring_minutes && CFG.ring_minutes.length) names.push("custom");
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
    q.addEventListener("input", function () { st.q = q.value.trim(); saveHash(); redraw(); footer(); if (COMPARE) coverage(); });
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
    var ng = $("nsGroup");
    ng.textContent = "";
    ng.style.display = st.ns ? "" : "none";
    if (st.ns) {
      var nb = el("button", { class: "chip on", id: "nsClear", title: "clear the namespace filter" },
        "namespace " + (st.ns === "_" ? "cluster-scoped" : st.ns) + " ×");
      nb.addEventListener("click", function () { st.ns = ""; refresh(); });
      ng.appendChild(nb);
    }
    var box = $("cats"), cat = {};
    // each identity counted once over the panes
    panes.forEach(function (p) {
      model(p.cluster).nodes.forEach(function (x) {
        if ((st.system || !x.system) && (!st.ns || x.ns === st.ns) && !cat[keyOf(x.id)]) cat[keyOf(x.id)] = x.cat;
      });
    });
    box.textContent = "";
    CATS.forEach(function (c) {
      var n = Object.keys(cat).filter(function (id) { return cat[id] === c; }).length;
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
    var entries, head = "color: " + st.color;
    if (st.color === "state") {
      var c = stateCounts();
      head = "state";
      entries = STATES.map(function (s) {
        return [s + " " + c[s] + { equal: "", drift: "", pending: " (source only)", "new": " (target only)" }[s], STATE_COLOR[s]];
      });
    } else if (st.color === "manager") entries = MGR.map(function (k) { return [k, MGR_COLOR[k]]; });
    else if (st.color === "team") entries = m.teams.map(function (t) { return [t, m.teamColor[t]]; }).concat([["no team", "#616161"]]);
    else entries = CATS.map(function (c) { return [c, CAT_COLOR[c]]; });
    box.appendChild(el("div", { class: "h" }, head));
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
    // shown and match count each identity once over the panes (data-shown keeps the sum of the panes)
    var seen = {}, hit = {}, hits = 0, shown = 0;
    panes.forEach(function (p) {
      p.pos.forEach(function (o) {
        var id = keyOf(o.n.id);
        if (!seen[id]) { seen[id] = true; shown++; }
        if (st.q && !hit[id] && matches(o.n)) { hit[id] = true; hits++; }
      });
    });
    var match = st.q ? " · " + hits + " match" : "";
    if (st.q) body.setAttribute("data-match", String(hits)); else body.removeAttribute("data-match");
    if (COMPARE) {
      var sa = model(panes[0].cluster).snap, sb = model(panes[1].cluster).snap, c = stateCounts();
      var errs = (sa.errors || []).length + (sb.errors || []).length;
      $("status").textContent = "collected: source " + sa.resources.length + " · target " + sb.resources.length +
        " · " + shown + " shown (identities counted once)" + match + " · states (active filters): equal " + c.equal +
        " · drift " + c.drift + " · pending " + c.pending + " · new " + c["new"] + " · coverage " +
        pct(CMP.totals.coverage) + " (all non-system namespaces)" + (errs ? " · " + errs + " collection errors" : "");
      $("meta").textContent = sa.cluster.context + " → " + sb.cluster.context + " · kubestrata " + (CFG.version || "");
      return;
    }
    var m = model(panes[0].cluster), cl = m.snap.cluster;
    $("status").textContent = m.snap.resources.length + " resources · " + shown + " shown" + match +
      ((m.snap.errors || []).length ? " · " + m.snap.errors.length + " collection errors" : "");
    $("meta").textContent = cl.context + " · " + cl.distro + " " + cl.version + " · collected " + fmt(m.snap.collected_at) +
      " · kubestrata " + (CFG.version || "");
  }
  function titles() {
    panes.forEach(function (p, i) {
      var s = model(p.cluster).snap, c = s.cluster;
      p.title.textContent = "";
      if (panes.length > 1) {
        p.title.appendChild(el("b", null, (COMPARE ? (i === 0 ? "source · " : "target · ") : "") + c.context));
        p.title.appendChild(el("div", null, c.distro + " " + c.version + " · " + fmt(s.collected_at)));
      }
    });
  }

  function redraw() { panes.forEach(draw); }
  function refresh() {
    sec = sectors(panes.map(function (p) { return model(p.cluster); }));
    panes.forEach(function (p) { layout(p, sec); });
    syncControls(); legend(); titles(); redraw(); footer(); renderPanel(); saveHash();
    if (COMPARE) coverage();
  }

  // ---- start
  if (!(CFG.clusters || []).length) throw new Error("no clusters in this view");
  if (!DATA[st.cluster]) st.cluster = CFG.clusters[0];
  buildControls();
  if (COMPARE) CFG.clusters.forEach(function (id) { panes.push(makePane(id)); });
  else panes.push(makePane(st.cluster));
  window.addEventListener("resize", redraw);
  refresh();
  var shownAll = 0, drawAll = 0;
  panes.forEach(function (p) { shownAll += p.shown; drawAll += p.drawMs; });
  body.setAttribute("data-status", "ok");
  body.setAttribute("data-rings", panes[0].sc.rings.map(ringLabel).join(","));
  body.setAttribute("data-shown", String(shownAll));
  body.setAttribute("data-draw-ms", drawAll.toFixed(1));
  body.setAttribute("data-total-ms", (performance.now() - T0).toFixed(1));
})();
