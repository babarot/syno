"use strict";

// The page polls /api/panels/<profile>/<panel> at each panel's interval and
// draws what comes back. The server asks the NAS only when its copy is due,
// so several open pages cost the NAS no more than one.

const $ = id => document.getElementById(id);
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const hb = b => { const u = ["B", "KB", "MB", "GB", "TB", "PB"]; let i = 0; while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; } return i ? `${b.toFixed(b >= 100 ? 0 : 1)} ${u[i]}` : `${Math.round(b)} B`; };
const rank = { ok: 0, skip: 0, warn: 1, unknown: 2, fail: 3 };
const HISTORY = 24; // points of the CPU and memory chart

const state = {
  profiles: [],
  intervals: {}, // panel name -> seconds
  cur: "",
  data: {},      // "profile/panel" -> the server's answer
  hist: { cpu: [], mem: [] },
  io: {},        // volume path -> { read: [], write: [] }, like hist
  scrub: {},     // pool name -> samples of a running scrubbing, [{ t, pct }]
  timers: [],
};

// The profile and the theme are remembered per browser, when storage works.
function store(key, value) {
  try { localStorage.setItem(key, value); } catch { /* storage can be off */ }
}
function load(key) {
  try { return localStorage.getItem(key); } catch { return null; }
}

async function api(path, opts = {}) {
  const res = await fetch(path, { ...opts, headers: { "X-Syno-Dashboard": "1", ...(opts.headers || {}) } });
  if (res.status === 401) throw new Error("Not signed in to this dashboard: open the URL that syno dashboard printed.");
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.json();
}

// ---- drawing helpers ----

function ring(value, max, size, stroke, color, center, label) {
  const r = (size - stroke) / 2, c = 2 * Math.PI * r, f = Math.max(0, Math.min(1, value / max));
  // Round caps stick out stroke/2 past each end, so the arc is drawn shorter
  // by one stroke and starts half a stroke late; the caps then end where the
  // value does. Below 100% a gap of at least minGap stays open, so 98% does
  // not close into a full circle.
  const minGap = stroke * 0.8;
  const full = f >= 1;
  const len = full ? c : Math.max(0.01, Math.min(c * f, c - minGap) - stroke);
  return `<div class="ring" style="width:${size}px;height:${size}px">
    <svg width="${size}" height="${size}"><circle cx="${size / 2}" cy="${size / 2}" r="${r}" fill="none" stroke="var(--track)" stroke-width="${stroke}"/>
    <circle cx="${size / 2}" cy="${size / 2}" r="${r}" fill="none" stroke="${color}" stroke-width="${stroke}" stroke-linecap="${full ? "butt" : "round"}" stroke-dasharray="${len} ${c}" stroke-dashoffset="${full ? 0 : -stroke / 2}" style="transition:stroke-dasharray .6s"/></svg>
    <div class="center"><div class="val">${center}</div><div class="lab">${label}</div></div></div>`;
}

function segRing(parts, size, stroke, center, label) {
  const r = (size - stroke) / 2, c = 2 * Math.PI * r, total = parts.reduce((s, p) => s + p.n, 0) || 1;
  let off = 0;
  const arcs = parts.filter(p => p.n).map(p => {
    const len = c * p.n / total;
    const a = `<circle cx="${size / 2}" cy="${size / 2}" r="${r}" fill="none" stroke="${p.color}" stroke-width="${stroke}" stroke-dasharray="${Math.max(len - 3, 1)} ${c}" stroke-dashoffset="${-off}"/>`;
    off += len;
    return a;
  }).join("");
  return `<div class="ring" style="width:${size}px;height:${size}px"><svg width="${size}" height="${size}"><circle cx="${size / 2}" cy="${size / 2}" r="${r}" fill="none" stroke="var(--track)" stroke-width="${stroke}"/>${arcs}</svg><div class="center"><div class="val">${center}</div><div class="lab">${label}</div></div></div>`;
}

const levelColor = (v, warn, fail) => v >= fail ? "var(--fail)" : v >= warn ? "var(--warn)" : "var(--ok)";
const ago = s => s < 60 ? `${Math.round(s)}s` : s < 3600 ? `${Math.round(s / 60)}m` : `${Math.round(s / 3600)}h`;
const every = s => s < 60 ? `${s}s` : s < 3600 ? `${s / 60}m` : `${s / 3600}h`;

function worst(results) {
  return (results || []).reduce((w, r) => rank[r.level] > rank[w] ? r.level : w, "ok");
}

// ---- panels ----

// drawHealth shows the doctor checks with the update checks, which the
// updates panel runs at a slower pace.
function drawHealth(doc, upd) {
  const results = [...doc.results, ...(upd?.checks || [])];
  const cnt = { ok: 0, warn: 0, fail: 0, skip: 0, unknown: 0 };
  results.forEach(r => cnt[r.level]++);
  const w = worst(results);
  const checked = results.length - cnt.skip;
  $("health-ring").innerHTML = segRing([
    { n: cnt.fail, color: "var(--fail)" }, { n: cnt.unknown, color: "var(--info)" }, { n: cnt.warn, color: "var(--warn)" },
    { n: cnt.ok, color: "var(--ok)" }, { n: cnt.skip, color: "var(--skip)" },
  ], 150, 16, `${cnt.ok}/${checked}`, "checks ok");
  const verdict = { ok: "All good", warn: "Needs attention", fail: "Problems found", unknown: "Some checks failed to run" }[w];
  $("verdict").innerHTML = `<span class="c-${w === "unknown" ? "muted" : w}">${verdict}</span>`;
  $("verdict-sub").innerHTML = [
    cnt.fail && `<span class="c-fail">${cnt.fail} failing</span>`, cnt.warn && `<span class="c-warn">${cnt.warn} warning</span>`,
    cnt.unknown && `${cnt.unknown} could not run`, `${cnt.ok} ok`, cnt.skip && `${cnt.skip} skipped`,
  ].filter(Boolean).join(" · ");
  $("tiles").innerHTML = [...results].sort((a, b) => rank[b.level] - rank[a.level]).map(r => {
    const lv = r.level === "unknown" ? "skip" : r.level;
    return `<div class="tile ${lv}"><div class="hd"><span class="lv">${esc(r.level.toUpperCase())}</span><span class="n">${esc(r.check)}</span></div>
      <div class="s">${esc(r.summary)}</div>${r.details?.length ? `<ul>${r.details.map(x => `<li>${esc(x)}</li>`).join("")}</ul>` : ""}</div>`;
  }).join("");
}

function drawSystem(sys, info) {
  if (sys) {
    $("gauges").innerHTML = ring(sys.cpu_percent, 100, 96, 10, "var(--accent)", `${sys.cpu_percent.toFixed(0)}%`, "CPU")
      + ring(sys.memory_percent, 100, 96, 10, "var(--accent2)", `${sys.memory_percent.toFixed(0)}%`, "Memory")
      + (info ? ring(info.temperature_c, 80, 96, 10, info.temperature_warning ? "var(--fail)" : "var(--ok)", `${info.temperature_c.toFixed(0)}°`, "System") : "");
  }
  const h = state.hist;
  const area = (data, color) => {
    if (data.length < 2) return "";
    const step = 300 / (HISTORY - 1), pts = data.map((v, i) => [(i + HISTORY - data.length) * step, 70 - v / 100 * 66]);
    const line = pts.map(p => p.join(",")).join(" ");
    return `<polygon points="${pts[0][0]},70 ${line} ${pts[pts.length - 1][0]},70" fill="${color}" opacity=".15"/><polyline points="${line}" fill="none" stroke="${color}" stroke-width="2" vector-effect="non-scaling-stroke"/>`;
  };
  $("area").innerHTML = area(h.mem, "var(--accent2)") + area(h.cpu, "var(--accent)");
  if (info) {
    $("sys").innerHTML = [["Model", info.model], ["DSM", info.dsm], ["Uptime", info.uptime], ["CPU", info.cpu_cores ? `${info.cpu_cores} cores` : "-"],
      ["Memory", sys ? hb(sys.memory_bytes) : "-"]].map(([k, v]) => `<dt>${k}</dt><dd>${esc(v)}</dd>`).join("");
  }
}

// bayOrder puts the disks in the order of the bays of the NAS, with a
// placeholder for each empty bay, then the M.2 SSDs and the disks of
// expansion units.
function bayOrder(d) {
  if (!d.bays) return d.disks;
  const inBay = {};
  d.disks.forEach(x => { if (x.bay) inBay[x.bay] = x; });
  const out = [];
  for (let n = 1; n <= d.bays.total; n++) out.push(inBay[n] || { empty: n });
  return out.concat(d.disks.filter(x => !x.bay || x.bay > d.bays.total));
}

// drawPool tells the state of the pool a volume is on, and when it was
// last scrubbed.
function drawPool(p, th) {
  const running = p.scrub_percent != null;
  const days = p.last_scrubbed ? (Date.now() - Date.parse(p.last_scrubbed)) / 86400000 : null;
  const scrubLate = !running && (days == null || days > th.scrub_age_days) || !p.scrub_scheduled;
  const scrub = days == null ? "never scrubbed" : `scrubbed ${Math.floor(days)} days ago`;
  // DSM shows the scrubbing in place of the pool's status.
  const status = running && p.status === "background_scrubbing" ? "normal, being scrubbed" : p.status;
  return `<div class="pool"><h4>${esc(p.name)}</h4>
    <dl class="kvs">
      <dt>Status</dt><dd class="${p.status === "normal" || status !== p.status ? "" : "c-fail"}">${esc(status)}</dd>
      <dt>RAID</dt><dd>${esc(p.raid)} · ${p.disks} ${p.disks === 1 ? "disk" : "disks"}</dd>
      <dt>Size</dt><dd>${hb(p.used_bytes)} of ${hb(p.total_bytes)}</dd>
      <dt>Scrubbing</dt><dd class="${scrubLate ? "c-warn" : ""}">${running ? "running now" : scrub}${p.scrub_scheduled ? "" : ", no schedule"}</dd>
    </dl>${running ? drawScrub(p) : ""}</div>`;
}

// drawScrub shows how far a running scrubbing has got. DSM gives no time
// left, so the page guesses it from how fast the percent went up while it
// was open.
function drawScrub(p) {
  const pct = p.scrub_percent, samples = state.scrub[p.name] || [];
  let left = "time left: watching how fast it goes…";
  if (samples.length >= 2) {
    const a = samples[0], b = samples[samples.length - 1];
    const rate = (b.pct - a.pct) / ((b.t - a.t) / 3600000); // percent an hour
    if (rate > 0) {
      const h = (100 - pct) / rate;
      left = `about ${h < 1 ? Math.max(1, Math.round(h * 60)) + " min" : h < 48 ? Math.round(h) + " h" : Math.round(h / 24) + " days"} left, guessed from the last ${ago((b.t - a.t) / 1000)}`;
    }
  }
  return `<div class="scrub"><div class="stackbar"><i style="width:${pct}%;background:var(--info)"></i></div>
    <div class="legend"><span>scrubbing ${pct.toFixed(2)}%</span><span class="c-muted">${left}</span></div></div>`;
}

// trackScrub keeps the samples drawScrub guesses the time left from.
function trackScrub(storage) {
  const now = Date.now();
  for (const p of storage.pools) {
    if (p.scrub_percent == null) { delete state.scrub[p.name]; continue; }
    const s = state.scrub[p.name] ||= [];
    if (!s.length || s[s.length - 1].pct !== p.scrub_percent) s.push({ t: now, pct: p.scrub_percent });
  }
}

// drawIO draws what each volume read and wrote over the last 2 minutes,
// collected from the system panel while the page is open.
function drawIO() {
  document.querySelectorAll(".volio").forEach(el => {
    const h = state.io[el.dataset.vol], last = get(state.cur, "system")?.data?.volumes?.find(v => v.path === el.dataset.vol);
    if (!h || !last) { el.innerHTML = `<h4>I/O</h4><div class="empty">Waiting for the first sample.</div>`; return; }
    const max = Math.max(...h.read, ...h.write, 1024);
    const line = (data, color) => {
      if (data.length < 2) return "";
      const step = 300 / (HISTORY - 1), pts = data.map((v, i) => [(i + HISTORY - data.length) * step, 60 - v / max * 56]);
      const ps = pts.map(p => p.join(",")).join(" ");
      return `<polygon points="${pts[0][0]},60 ${ps} ${pts[pts.length - 1][0]},60" fill="${color}" opacity=".15"/><polyline points="${ps}" fill="none" stroke="${color}" stroke-width="2" vector-effect="non-scaling-stroke"/>`;
    };
    el.innerHTML = `<h4>I/O <span class="c-muted">busy ${last.busy_percent.toFixed(0)}%</span></h4>
      <svg class="area" viewBox="0 0 300 60" preserveAspectRatio="none">${line(h.write, "var(--accent2)") + line(h.read, "var(--accent)")}</svg>
      <div class="legend"><span><i class="sw cpu"></i>read ${hb(last.read_bytes_per_sec)}/s</span><span><i class="sw mem"></i>write ${hb(last.write_bytes_per_sec)}/s</span><span class="c-muted">peak ${hb(max)}/s · last 2 minutes</span></div>`;
  });
}

// recycleOn sums the recycle bins of the shares on a volume, or returns null
// before they are summed.
function recycleOn(recycle, volume) {
  if (!recycle) return null;
  return recycle.shares.filter(s => s.volume === volume).reduce((n, s) => n + (s.recycle_bin_bytes || 0), 0);
}

function drawStorage(d, recycle) {
  const th = d.thresholds;
  $("volumes").innerHTML = d.volumes.length ? d.volumes.map(v => {
    const pct = v.total_bytes ? v.used_bytes / v.total_bytes * 100 : 0, free = v.total_bytes - v.used_bytes;
    const color = levelColor(pct, th.volume_warn, th.volume_fail);
    const pool = d.pools.find(p => p.name === v.pool);
    const bin = Math.min(recycleOn(recycle, v.path) ?? 0, v.used_bytes), data = v.used_bytes - bin;
    const after = v.total_bytes ? data / v.total_bytes * 100 : 0;
    return `<div class="vol">${ring(pct, 100, 140, 14, color, `${pct.toFixed(0)}%`, "used")}
      <div><h3>${esc(v.path)} <span class="tag">${esc(v.fs)}</span> <span class="tag">${esc(v.pool)}</span>${pool ? ` <span class="tag">${esc(pool.raid)}</span>` : ""}${v.status !== "normal" ? ` <span class="tag c-fail">${esc(v.status)}${v.detail ? ": " + esc(v.detail) : ""}</span>` : ""}</h3>
      <div style="margin-top:6px"><span class="free" style="color:${color}">${hb(free)}</span> <span class="c-muted">free of ${hb(v.total_bytes)}</span></div>
      <div class="stackbar"><i style="width:${data / v.total_bytes * 100}%;background:${color}"></i><i style="width:${bin / v.total_bytes * 100}%;background:var(--warn);opacity:.7"></i></div>
      <div class="legend"><span><i class="sw" style="background:${color}"></i>data ${hb(data)}</span>${bin ? `<span><i class="sw recycle"></i>recycle bins ${hb(bin)}</span>` : ""}<span><i class="sw free"></i>free ${hb(free)}</span>
      <span class="c-muted">· warn at ${th.volume_warn}%, fail at ${th.volume_fail}%</span></div>
      ${pct >= th.volume_warn && pct - after >= 1 ? `<div class="note">Emptying the recycle bins would bring it to ${after.toFixed(1)}%.</div>` : ""}</div></div>
      <div class="volmore">${pool ? drawPool(pool, th) : ""}<div class="volio" data-vol="${esc(v.path)}"></div></div>`;
  }).join("") : `<div class="empty">No volumes.</div>`;
  drawIO();

  $("bays").innerHTML = d.disks.length || d.bays ? bayOrder(d).map(x => {
    if (x.empty) {
      return `<div class="bay empty"><div class="top"><span class="name">Bay ${x.empty}</span></div><div class="model">empty</div></div>`;
    }
    const bad = x.bad_sectors > 0;
    const lv = x.status !== "normal" || x.smart !== "normal" ? "fail"
      : bad || x.temperature_c >= th.disk_temp_warn || (x.life_percent != null && x.life_percent < 20) ? "warn" : "ok";
    // The bar spans 20°C to the fail threshold plus 10.
    const hi = th.disk_temp_fail + 10, tpos = Math.min(Math.max((x.temperature_c - 20) / (hi - 20), 0), 1) * 100;
    const wpos = (th.disk_temp_warn - 20) / (hi - 20) * 100, fpos = (th.disk_temp_fail - 20) / (hi - 20) * 100;
    const life = x.life_percent == null
      ? `<span class="c-muted" style="grid-column:span 2">not reported by DSM</span>`
      : `<span class="mini"><i style="width:${x.life_percent}%;background:${x.life_percent < 20 ? "var(--fail)" : x.life_percent < 50 ? "var(--warn)" : "var(--ok)"}"></i></span><span class="v">${x.life_percent.toFixed(0)}%</span>`;
    const hours = x.power_on_hours == null
      ? `<span class="c-muted" style="grid-column:span 2">not reported by DSM</span>`
      : `<span class="c-muted">${(x.power_on_hours / 24 / 365).toFixed(1)} years</span><span class="v">${Math.round(x.power_on_hours).toLocaleString()}h</span>`;
    return `<div class="bay ${lv}">
      <div class="top"><span class="name">${esc(x.name)}</span><span class="tag">${esc(x.type)}</span></div>
      <div class="model" title="${esc(x.model)}">${esc(x.model)} · ${hb(x.size_bytes)}</div>
      <div class="row"><span>Temp</span><span class="thermo" style="background:linear-gradient(90deg, var(--ok) 0 ${wpos}%, var(--warn) ${wpos}% ${fpos}%, var(--fail) ${fpos}%)"><b style="left:calc(${tpos}% - 1px)"></b></span><span class="v">${x.temperature_c.toFixed(0)}°C</span></div>
      <div class="row"><span>Life left</span>${life}</div>
      <div class="row"><span>Power-on</span>${hours}</div>
      <div class="leds"><span class="led ${x.status === "normal" ? "on" : "bad"}">status ${esc(x.status)}</span><span class="led ${x.smart === "normal" ? "on" : "bad"}">SMART ${esc(x.smart)}</span><span class="led ${bad ? "bad" : "on"}">${x.bad_sectors} bad sectors</span><span class="led">${esc(x.pool)}</span></div></div>`;
  }).join("") : `<div class="empty">No disks.</div>`;
  $("bay-count").textContent = d.bays ? `· ${d.bays.used} of ${d.bays.total} bays used` : "";
}

const palette = ["#7a4fe0", "#2bb3c0", "#2f7ae5", "#d0679a", "#4caf7d", "#c98a2b", "#6b7280"];

// drawShares lays the shares out as a treemap, with the part in each
// recycle bin hatched once the bins are summed.
function drawShares(d, recycle) {
  const el = $("treemap");
  const bins = {};
  (recycle?.shares || []).forEach(s => { if (s.recycle_bin_bytes != null) bins[s.name] = s.recycle_bin_bytes; });
  $("treemap-bin").style.display = recycle ? "" : "none";
  const items = d.shares.map(s => ({ name: s.name, hidden: s.hidden, used: s.used_bytes, bin: bins[s.name] || 0 }));
  if (!items.length) { el.innerHTML = `<div class="empty">No shared folders.</div>`; return; }
  const W = el.clientWidth, H = el.clientHeight, size = i => Math.max(i.used, 1), total = items.reduce((n, i) => n + size(i), 0);
  const cells = [];
  (function lay(list, x, y, w, h) {
    if (!list.length) return;
    if (list.length === 1) { cells.push({ ...list[0], x, y, w, h }); return; }
    const sum = list.reduce((n, i) => n + size(i), 0);
    let acc = 0, k = 0;
    while (k < list.length - 1 && (acc + size(list[k])) / sum < 0.5) acc += size(list[k++]);
    if (k === 0) acc = size(list[k++]);
    const f = acc / sum;
    if (w >= h) { lay(list.slice(0, k), x, y, w * f, h); lay(list.slice(k), x + w * f, y, w * (1 - f), h); }
    else { lay(list.slice(0, k), x, y, w, h * f); lay(list.slice(k), x, y + h * f, w, h * (1 - f)); }
  })([...items].sort((a, b) => b.used - a.used), 0, 0, W, H);
  el.innerHTML = cells.map((c, i) => {
    const binH = c.bin && c.used ? Math.max(c.h * Math.min(c.bin / c.used, 1), 3) : 0;
    const small = c.w < 70 || c.h < 40, share = c.used / total * 100;
    return `<div class="cell" title="${esc(c.name)}: ${hb(c.used)}${c.bin ? `, ${hb(c.bin)} in its recycle bin` : ""}" style="left:${c.x}px;top:${c.y}px;width:${c.w}px;height:${c.h}px;background:${palette[i % palette.length]}">
      ${small ? "" : `<b>${esc(c.name)}${c.hidden ? " · hidden" : ""}</b>${hb(c.used)} · ${share.toFixed(share < 1 ? 2 : 0)}%`}
      ${binH ? `<div class="bin" style="height:${binH}px"></div>` : ""}</div>`;
  }).join("");
}

function drawRecycle(d) {
  const on = d.shares.filter(s => s.recycle_bin), off = d.shares.filter(s => !s.recycle_bin);
  const total = on.reduce((n, s) => n + (s.recycle_bin_bytes || 0), 0);
  const max = Math.max(...on.map(s => s.recycle_bin_bytes || 0), 1);
  $("recycle-big").textContent = hb(total);
  $("recycle-sub").textContent = "would be freed by emptying them";
  $("recycle").innerHTML = [...on].sort((a, b) => (b.recycle_bin_bytes || 0) - (a.recycle_bin_bytes || 0)).map(s =>
    `<div class="rb"><span class="n" title="${esc(s.name)}">${esc(s.name)}</span><span class="mini" style="height:10px"><i style="width:${(s.recycle_bin_bytes || 0) / max * 100}%;background:var(--warn)"></i></span><span class="v">${s.recycle_bin_bytes == null ? "-" : hb(s.recycle_bin_bytes)}</span></div>`
  ).join("") + off.map(s =>
    `<div class="rb"><span class="n" title="${esc(s.name)}">${esc(s.name)}</span><span class="c-muted">recycle bin off</span><span class="v c-muted">-</span></div>`
  ).join("");
}

function drawUpdates(d) {
  const outdated = d.packages.filter(p => p.latest), sec = outdated.filter(p => p.security).length;
  const current = d.packages.length - outdated.length;
  $("upd-head").innerHTML = `${segRing([{ n: sec, color: "var(--fail)" }, { n: outdated.length - sec, color: "var(--warn)" }, { n: current, color: "var(--ok)" }], 104, 12, outdated.length, outdated.length === 1 ? "update" : "updates")}
    <div><div><span class="dot bg-fail"></span> ${sec} security</div><div><span class="dot bg-warn"></span> ${outdated.length - sec} other</div><div><span class="dot bg-ok"></span> ${current} up to date</div></div>`;
  const dsm = d.checks.find(c => c.check === "dsm-update");
  $("dsm-update").innerHTML = dsm ? `<span class="c-${dsm.level === "unknown" ? "muted" : dsm.level}">${esc(dsm.summary)}</span>` : "";
  // Security updates first, so a scrolled list does not hide them.
  const order = [...outdated].sort((a, b) => (b.security ? 1 : 0) - (a.security ? 1 : 0));
  $("pkgs").innerHTML = order.map(p => `<div class="pkg"><span>${esc(p.name)} ${p.security ? `<span class="tag c-fail">security</span>` : ""}</span><span></span><span class="ver">${esc(p.version)} <span class="arrow">→</span> ${esc(p.latest)}</span></div>`).join("");
  fadePkgs();
}

// The list fades at its bottom while more of it lies below.
function fadePkgs() {
  const l = $("pkgs");
  l.classList.toggle("more", l.scrollHeight - l.scrollTop - l.clientHeight > 1);
}

function drawContainers(d) {
  if (!d.installed) {
    $("ctrs").innerHTML = `<div class="empty">Container Manager is not installed on this NAS.</div>`;
    return;
  }
  if (!d.containers.length) {
    $("ctrs").innerHTML = `<div class="empty">No containers.</div>`;
    return;
  }
  const memMax = Math.max(...d.containers.map(c => c.memory_bytes || 0), 1);
  $("ctrs").innerHTML = d.containers.map(c => {
    const run = c.state === "running";
    const lv = c.health === "unhealthy" || c.error ? "fail" : run ? "ok" : "skip";
    const cpu = c.cpu_percent, mem = c.memory_bytes;
    const usage = run && cpu != null && mem != null
      ? `<div class="bars"><span>CPU</span><span class="mini"><i style="width:${Math.min(cpu, 100)}%;background:var(--accent)"></i></span><span class="v">${cpu.toFixed(1)}%</span>
         <span>MEM</span><span class="mini"><i style="width:${mem / memMax * 100}%;background:var(--accent2)"></i></span><span class="v">${hb(mem)}</span></div>`
      : `<div class="c-muted" style="font-size:11px">${c.error ? `<span class="c-fail">${esc(c.error)}</span>` : run ? "usage not reported" : "not running"}</div>`;
    return `<div class="ctr ${run ? "" : "stopped"}">
      <div class="top"><i class="dot bg-${lv}"></i><span class="name" title="${esc(c.name)}">${esc(c.name)}</span>${c.health ? `<span class="tag ${c.health === "unhealthy" ? "c-fail" : "c-ok"}">${esc(c.health)}</span>` : ""}${c.project ? `<span class="tag">${esc(c.project)}</span>` : ""}</div>
      <div class="img" title="${esc(c.image)}">${esc(c.image)}</div>
      ${usage}
      <div class="foot"><span>${esc(c.status)}</span></div></div>`;
  }).join("");
}

// ---- state and polling ----

const get = (profile, panel) => state.data[`${profile}/${panel}`];

function render(panel) {
  const p = state.cur, e = get(p, panel);
  document.querySelectorAll(`[data-panel="${panel}"]`).forEach(sec => {
    sec.querySelector(".err").textContent = e?.error || "";
    sec.classList.toggle("stale-data", !!(e?.error && e?.data));
  });
  if (!e?.data) {
    if (panel === "system" || panel === "info") drawSystem(get(p, "system")?.data, get(p, "info")?.data);
    return;
  }
  const data = name => get(p, name)?.data;
  switch (panel) {
    case "doctor": drawHealth(e.data, data("updates")); break;
    case "system": case "info": drawSystem(data("system"), data("info")); drawIO(); break;
    case "storage": trackScrub(e.data); drawStorage(e.data, data("recycle")); break;
    case "containers": drawContainers(e.data); break;
    case "shares": drawShares(e.data, data("recycle")); break;
    case "recycle":
      drawRecycle(e.data);
      if (data("shares")) drawShares(data("shares"), e.data);
      if (data("storage")) drawStorage(data("storage"), e.data);
      break;
    case "updates":
      drawUpdates(e.data);
      if (data("doctor")) drawHealth(data("doctor"), e.data);
      break;
  }
  if (panel === "info") $("host").textContent = e.data.host;
  drawTabs();
}

function drawTabs() {
  $("nas").innerHTML = state.profiles.map(name => {
    const doc = get(name, "doctor")?.data, info = get(name, "info")?.data;
    const dot = doc ? worst(doc.results) : "skip";
    return `<button aria-selected="${name === state.cur}" data-n="${esc(name)}"><i class="dot bg-${dot === "unknown" ? "skip" : dot}"></i>${esc(name)}${info ? `<small>${esc(info.model)}</small>` : ""}</button>`;
  }).join("");
  $("nas").querySelectorAll("button").forEach(b => b.onclick = () => select(b.dataset.n));
}

function drawAges() {
  const now = Date.now();
  document.querySelectorAll("section[data-panel]").forEach(sec => {
    const panel = sec.dataset.panel, e = get(state.cur, panel), src = sec.querySelector(".src");
    const interval = state.intervals[panel];
    if (!e?.updated_at) { src.textContent = e?.error ? `failed · every ${every(interval)}` : "loading…"; return; }
    const age = (now - Date.parse(e.updated_at)) / 1000;
    src.textContent = `${ago(age)} ago · every ${every(interval)}`;
    src.classList.toggle("stale", age > interval * 2 + 5);
  });
}

async function poll(profile, panel, refresh = false) {
  // A page in a background tab asks nothing, so the NAS rests too.
  if (document.hidden && !refresh) return;
  try {
    const e = await api(`/api/panels/${encodeURIComponent(profile)}/${panel}${refresh ? "/refresh" : ""}`, refresh ? { method: "POST" } : {});
    state.data[`${profile}/${panel}`] = e;
    if (panel === "system" && profile === state.cur && e.data && !e.error) {
      const h = state.hist;
      h.cpu.push(e.data.cpu_percent); h.mem.push(e.data.memory_percent);
      if (h.cpu.length > HISTORY) { h.cpu.shift(); h.mem.shift(); }
      for (const v of e.data.volumes || []) {
        const io = state.io[v.path] ||= { read: [], write: [] };
        io.read.push(v.read_bytes_per_sec); io.write.push(v.write_bytes_per_sec);
        if (io.read.length > HISTORY) { io.read.shift(); io.write.shift(); }
      }
    }
  } catch (err) {
    const old = state.data[`${profile}/${panel}`] || {};
    state.data[`${profile}/${panel}`] = { ...old, error: err.message };
  }
  if (profile === state.cur) render(panel);
  else if (panel === "doctor" || panel === "info") drawTabs();
}

function schedule() {
  state.timers.forEach(clearInterval);
  state.timers = [];
  for (const [panel, sec] of Object.entries(state.intervals)) {
    poll(state.cur, panel);
    state.timers.push(setInterval(() => poll(state.cur, panel), sec * 1000));
  }
  // The other profiles only color their tabs.
  const others = () => state.profiles.filter(p => p !== state.cur).forEach(p => { poll(p, "doctor"); poll(p, "info"); });
  others();
  state.timers.push(setInterval(others, (state.intervals.doctor || 300) * 1000));
}

function select(profile) {
  if (profile === state.cur) return;
  state.cur = profile;
  state.hist = { cpu: [], mem: [] };
  state.io = {};
  state.scrub = {};
  store("syno.profile", profile);
  $("host").textContent = get(profile, "info")?.data?.host || "";
  for (const panel of Object.keys(state.intervals)) render(panel);
  drawTabs();
  schedule();
}

async function start() {
  $("theme").onclick = () => {
    const r = document.documentElement;
    const dark = r.dataset.theme ? r.dataset.theme === "dark" : matchMedia("(prefers-color-scheme: dark)").matches;
    r.dataset.theme = dark ? "light" : "dark";
    store("syno.theme", r.dataset.theme);
  };
  const theme = load("syno.theme");
  if (theme) document.documentElement.dataset.theme = theme;
  document.querySelectorAll("[data-refresh]").forEach(b => b.onclick = () => b.dataset.refresh.split(" ").forEach(panel => poll(state.cur, panel, true)));
  addEventListener("resize", () => { const d = get(state.cur, "shares")?.data; if (d) drawShares(d, get(state.cur, "recycle")?.data); });

  try {
    const cfg = await api("/api/config");
    state.profiles = cfg.profiles;
    cfg.panels.forEach(p => state.intervals[p.name] = p.interval_s);
  } catch (err) {
    document.querySelector("main").innerHTML = `<section class="panel s12"><div class="err">${esc(err.message)}</div></section>`;
    return;
  }
  const saved = load("syno.profile");
  state.cur = state.profiles.includes(saved) ? saved : state.profiles[0];
  drawTabs();
  schedule();
  setInterval(drawAges, 1000);
  // Catch up at once when the tab comes back, rather than at the next tick.
  $("pkgs").addEventListener("scroll", fadePkgs);
  new ResizeObserver(fadePkgs).observe($("pkgs"));
  document.addEventListener("visibilitychange", () => { if (!document.hidden) schedule(); });
}

start();
