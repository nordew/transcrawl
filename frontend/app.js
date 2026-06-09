const $ = (id) => document.getElementById(id);

function el(tag, attrs, ...children) {
  const n = document.createElement(tag);
  if (attrs) {
    for (const [k, v] of Object.entries(attrs)) {
      if (k === "class") n.className = v;
      else if (k === "text") n.textContent = v;
      else if (k === "on" && typeof v === "object") {
        for (const [ev, fn] of Object.entries(v)) n.addEventListener(ev, fn);
      } else if (v != null) n.setAttribute(k, v);
    }
  }
  for (const c of children) if (c != null) n.append(c);
  return n;
}

const form = $("form");
const els = {
  channels: $("channels"),
  last: $("last"),
  langs: $("langs"),
  format: $("format"),
  manual: $("manual_only"),
  overwrite: $("overwrite"),
  go: $("go"),
  stop: $("stop"),
  ribbon: $("ribbon"),
  progress: $("progress"),
  fill: $("progress-fill"),
  ptext: $("progress-text"),
  results: $("results"),
  empty: $("empty"),
};

const counts = { ok: 0, skipped: 0, no_transcript: 0, failed: 0 };
let total = 0;
let processed = 0;
let source = null;
const groups = new Map();

const STORE_KEY = "transcrawl.inputs";
restoreInputs();

form.addEventListener("submit", (e) => {
  e.preventDefault();
  start();
});
els.stop.addEventListener("click", () => stop("Stopped."));

function restoreInputs() {
  try {
    const s = JSON.parse(localStorage.getItem(STORE_KEY) || "{}");
    if (s.channels) els.channels.value = s.channels;
    if (s.last) els.last.value = s.last;
    if (s.langs) els.langs.value = s.langs;
    if (s.format) els.format.value = s.format;
    els.manual.checked = !!s.manual;
    els.overwrite.checked = !!s.overwrite;
  } catch (_) {}
}

function saveInputs() {
  localStorage.setItem(STORE_KEY, JSON.stringify({
    channels: els.channels.value,
    last: els.last.value,
    langs: els.langs.value,
    format: els.format.value,
    manual: els.manual.checked,
    overwrite: els.overwrite.checked,
  }));
}

function channelList() {
  return els.channels.value
    .split(/[\n,]+/)
    .map((s) => s.trim())
    .filter(Boolean)
    .join(",");
}

function start() {
  const channels = channelList();
  if (!channels) return;
  saveInputs();
  reset();

  document.body.classList.add("running");
  els.go.disabled = true;
  els.stop.hidden = false;
  els.ribbon.hidden = false;
  els.progress.hidden = false;
  els.empty.hidden = true;
  els.ptext.textContent = "Connecting…";

  const params = new URLSearchParams({
    channels,
    last: els.last.value || "10",
    langs: els.langs.value || "en",
    format: els.format.value,
    manual_only: els.manual.checked ? "true" : "false",
    overwrite: els.overwrite.checked ? "true" : "false",
  });

  source = new EventSource("/api/stream?" + params.toString());
  source.onmessage = (ev) => {
    let msg;
    try { msg = JSON.parse(ev.data); } catch (_) { return; }
    handle(msg);
  };
  source.onerror = () => {
    if (source && source.readyState === EventSource.CLOSED) return;
    stop("Connection lost.");
  };
}

function reset() {
  counts.ok = counts.skipped = counts.no_transcript = counts.failed = 0;
  total = 0;
  processed = 0;
  groups.clear();
  els.results.replaceChildren();
  renderCounts();
  setProgress();
}

function stop(msg) {
  if (source) { source.close(); source = null; }
  document.body.classList.remove("running");
  els.go.disabled = false;
  els.stop.hidden = true;
  if (msg) els.ptext.textContent = msg;
}

function handle(ev) {
  switch (ev.type) {
    case "enumerated":
      total += ev.total;
      ensureGroup(ev.channel, ev.total);
      setProgress();
      break;
    case "video":
      bumpCount(ev.video.status);
      processed++;
      addItem(ev.channel, ev.video);
      setProgress();
      break;
    case "channel_error":
      addNotice(`${ev.url}: ${ev.message}`);
      break;
    case "done": {
      const s = ev.summary || {};
      stop(`Done — ${s.ok || 0} saved, ${s.skipped || 0} skipped, ${s.no_transcript || 0} no captions, ${s.failed || 0} failed.`);
      els.fill.style.width = "100%";
      break;
    }
    case "error":
      addNotice(ev.message, true);
      stop("Failed.");
      break;
  }
}

function bumpCount(status) {
  if (status in counts) counts[status]++;
  renderCounts();
}

function renderCounts() {
  $("c-ok").textContent = counts.ok;
  $("c-skip").textContent = counts.skipped;
  $("c-none").textContent = counts.no_transcript;
  $("c-fail").textContent = counts.failed;
}

function setProgress() {
  const pct = total > 0 ? Math.min(100, Math.round((processed / total) * 100)) : 0;
  els.fill.style.width = pct + "%";
  els.ptext.textContent = total > 0 ? `${processed} / ${total} videos` : "Enumerating…";
}

function ensureGroup(handle, count) {
  if (groups.has(handle)) return groups.get(handle);
  const list = el("div", { class: "group__list" });
  const head = el("div", { class: "group__head" },
    el("span", { class: "group__name", text: handle }),
    el("span", { class: "group__count", text: `${count} video${count === 1 ? "" : "s"}` }),
  );
  els.results.appendChild(el("div", { class: "group" }, head, list));
  groups.set(handle, list);
  return list;
}

function addItem(handle, v) {
  const list = ensureGroup(handle, 0);
  const meta = el("div", { class: "item__meta" });
  if (v.upload_date) meta.append(el("span", { class: "tag", text: fmtDate(v.upload_date) }));
  meta.append(el("span", { text: v.id }));
  if (v.lang) meta.append(el("span", { class: "tag", text: v.lang }));
  meta.append(el("span", { text: label(v.status) }));
  if (v.reason) meta.append(el("span", { class: "reason", text: v.reason }));

  const act = el("div", { class: "item__act" });
  if (v.status === "ok" && v.file) {
    const q = `channel=${encodeURIComponent(handle)}&file=${encodeURIComponent(v.file)}`;
    act.append(
      el("button", { class: "linkbtn", on: { click: () => openDrawer(handle, v.file, v.title || v.id) } }, "View"),
      el("a", { class: "linkbtn", href: `/api/file?${q}&download=1` }, "Download"),
    );
  }

  list.append(el("div", { class: `item s-${v.status}` },
    el("span", { class: "statusdot" }),
    el("div", { class: "item__main" },
      el("div", { class: "item__title", text: v.title || v.id }),
      meta,
    ),
    act,
  ));
}

function addNotice(text, isError) {
  els.results.append(el("div", { class: "notice" + (isError ? " notice--error" : ""), text }));
}

function label(status) {
  return { ok: "saved", skipped: "skipped", no_transcript: "no captions", failed: "failed" }[status] || status;
}

function fmtDate(d) {
  if (!d || d.length !== 8) return d || "";
  return `${d.slice(0, 4)}-${d.slice(4, 6)}-${d.slice(6, 8)}`;
}

const drawer = $("drawer");
const scrim = $("scrim");
let drawerOpener = null;

async function openDrawer(handle, file, title) {
  drawerOpener = document.activeElement;
  $("drawer-title").textContent = title;
  $("drawer-sub").textContent = file;
  const q = `channel=${encodeURIComponent(handle)}&file=${encodeURIComponent(file)}`;
  $("drawer-dl").href = `/api/file?${q}&download=1`;
  const body = $("drawer-body");
  body.classList.add("is-loading");
  body.textContent = "Loading transcript…";
  showDrawer();

  try {
    const res = await fetch(`/api/file?${q}`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    body.textContent = await res.text();
    body.classList.remove("is-loading");
  } catch (e) {
    body.textContent = "Could not load transcript: " + e.message;
  }
}

function showDrawer() {
  scrim.hidden = false;
  drawer.hidden = false;
  drawer.style.animation = "none";
  void drawer.offsetWidth;
  drawer.style.animation = "";
  $("drawer-close").focus();
}
function hideDrawer() {
  scrim.hidden = true;
  drawer.hidden = true;
  if (drawerOpener && typeof drawerOpener.focus === "function") drawerOpener.focus();
  drawerOpener = null;
}
scrim.addEventListener("click", hideDrawer);
$("drawer-close").addEventListener("click", hideDrawer);
document.addEventListener("keydown", (e) => { if (e.key === "Escape" && !drawer.hidden) hideDrawer(); });
