/* ============================================================
   Clipboard — realtime dashboard client
   ============================================================ */

const $ = (id) => document.getElementById(id);

const state = {
  room: null,
  clientId: null,
  device: null,
  items: new Map(),
  peers: [],
  filter: "all",
  limits: { max_upload_bytes: 100 * 1024 * 1024, retention: "24h" },
  socket: null,
  connected: false,
  retry: 0,
  retryTimer: null,
  heartbeat: null,
};

/* ── Small helpers ─────────────────────────────────────────── */

function formatBytes(n) {
  if (!n) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1);
  const value = n / 1024 ** i;
  return `${value >= 10 || i === 0 ? Math.round(value) : value.toFixed(1)} ${units[i]}`;
}

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
function formatAgo(iso) {
  const seconds = Math.round((Date.now() - new Date(iso).getTime()) / 1000);
  if (seconds < 45) return "just now";
  const steps = [
    [60, "second", 1],
    [3600, "minute", 60],
    [86400, "hour", 3600],
    [604800, "day", 86400],
  ];
  for (const [limit, unit, divisor] of steps) {
    if (seconds < limit) return rtf.format(-Math.round(seconds / divisor), unit);
  }
  return new Date(iso).toLocaleDateString();
}

// A readable duration for "kept for 24h".
function formatRetention(raw) {
  const match = /^(\d+(?:\.\d+)?)([hms])/.exec(raw || "");
  if (!match) return raw || "—";
  const [, amount, unit] = match;
  const names = { h: "hour", m: "minute", s: "second" };
  const n = Number(amount);
  return `${n} ${names[unit]}${n === 1 ? "" : "s"}`;
}

function guessDeviceName() {
  const ua = navigator.userAgent;
  const platform =
    /Android/i.test(ua) ? "Android" :
    /iPhone|iPad|iPod/i.test(ua) ? "iOS" :
    /Mac OS X/i.test(ua) ? "Mac" :
    /Windows/i.test(ua) ? "Windows" :
    /Linux/i.test(ua) ? "Linux" : "Device";
  const browser =
    /Edg\//.test(ua) ? "Edge" :
    /OPR\//.test(ua) ? "Opera" :
    /Firefox\//.test(ua) ? "Firefox" :
    /Chrome\//.test(ua) ? "Chrome" :
    /Safari\//.test(ua) ? "Safari" : "browser";
  return `${platform} · ${browser}`;
}

function deviceName() {
  let name = localStorage.getItem("clipboard.device");
  if (!name) {
    name = guessDeviceName();
    localStorage.setItem("clipboard.device", name);
  }
  return name;
}

/* ── Toasts ────────────────────────────────────────────────── */

function toast(message, kind = "") {
  const el = document.createElement("div");
  el.className = `toast ${kind}`.trim();
  el.textContent = message;
  $("toasts").append(el);
  setTimeout(() => {
    el.classList.add("leaving");
    setTimeout(() => el.remove(), 220);
  }, kind === "error" ? 4200 : 2400);
}

/* ── API ───────────────────────────────────────────────────── */

async function api(path, options = {}) {
  const res = await fetch(path, {
    ...options,
    headers: {
      "X-Device-Name": state.device || deviceName(),
      ...(options.body && !(options.body instanceof FormData)
        ? { "Content-Type": "application/json" }
        : {}),
      ...(options.headers || {}),
    },
  });
  if (!res.ok) {
    let message = `request failed (${res.status})`;
    try {
      const body = await res.json();
      if (body.error) message = body.error;
    } catch {
      /* non-JSON error body */
    }
    throw new Error(message);
  }
  return res.status === 204 ? null : res.json();
}

/* ── Routing ───────────────────────────────────────────────── */

function roomFromPath() {
  const match = /^\/r\/([a-z0-9-]{4,64})\/?$/i.exec(location.pathname);
  return match ? match[1].toLowerCase() : null;
}

function goToRoom(code, { replace = false } = {}) {
  const url = `/r/${code}`;
  if (replace) history.replaceState({}, "", url);
  else history.pushState({}, "", url);
  route();
}

function route() {
  const code = roomFromPath();
  if (code && code === state.room) return;
  closeRoom(); // drop the previous room's socket before opening another
  if (code) {
    openRoom(code);
  } else {
    $("dashboard").hidden = true;
    $("landing").hidden = false;
    document.title = "Clipboard — realtime clipboard for all your devices";
  }
}

/* ── Room lifecycle ────────────────────────────────────────── */

function openRoom(code) {
  state.room = code;
  state.items.clear();
  cardCache.clear();
  $("landing").hidden = true;
  $("dashboard").hidden = false;
  $("roomCode").textContent = code;
  $("codeDisplay").textContent = code;
  document.title = `${code} · Clipboard`;
  localStorage.setItem("clipboard.lastRoom", code);
  renderStream();
  connect();
}

function closeRoom() {
  if (state.room && !$("qrModal").hidden) closeQR();
  state.room = null;
  if (state.retryTimer) clearTimeout(state.retryTimer);
  if (state.heartbeat) {
    clearInterval(state.heartbeat);
    state.heartbeat = null;
  }
  if (state.socket) {
    const socket = state.socket;
    state.socket = null;
    socket.close(1000, "leaving");
  }
  setStatus("offline");
}

/* ── WebSocket ─────────────────────────────────────────────── */

function connect() {
  if (!state.room) return;
  if (state.socket && state.socket.readyState <= WebSocket.OPEN) return;

  setStatus("connecting");
  const scheme = location.protocol === "https:" ? "wss" : "ws";
  const url = `${scheme}://${location.host}/ws?room=${encodeURIComponent(state.room)}` +
    `&device=${encodeURIComponent(state.device)}`;
  const socket = new WebSocket(url);
  state.socket = socket;

  socket.addEventListener("open", () => {
    state.retry = 0;
    state.connected = true;
    setStatus("online");
    $("offlineBanner").hidden = true;
    startHeartbeat(socket);
  });

  socket.addEventListener("message", (event) => {
    let envelope;
    try {
      envelope = JSON.parse(event.data);
    } catch {
      return;
    }
    handleServerMessage(envelope);
  });

  socket.addEventListener("close", () => {
    if (state.socket !== socket) return; // superseded by a newer connection
    state.socket = null;
    state.connected = false;
    if (!state.room) return;
    setStatus("offline");
    $("offlineBanner").hidden = false;
    scheduleReconnect();
  });

  socket.addEventListener("error", () => socket.close());
}

// Some proxies — Heroku's router among them — close a connection that has been
// idle for around a minute. The server pings too; this is the other half, so
// the socket stays warm even where only client traffic is counted.
function startHeartbeat(socket) {
  if (state.heartbeat) clearInterval(state.heartbeat);
  state.heartbeat = setInterval(() => {
    if (socket.readyState !== WebSocket.OPEN) {
      clearInterval(state.heartbeat);
      state.heartbeat = null;
      return;
    }
    socket.send(JSON.stringify({ type: "ping" }));
  }, 25000);
}

// Back off gradually, with jitter so a restarted server does not get every
// dashboard reconnecting on the same tick.
function scheduleReconnect() {
  state.retry += 1;
  const base = Math.min(1000 * 2 ** (state.retry - 1), 15000);
  const delay = base + Math.random() * 400;
  if (state.retryTimer) clearTimeout(state.retryTimer);
  state.retryTimer = setTimeout(connect, delay);
}

function send(type, payload) {
  if (!state.socket || state.socket.readyState !== WebSocket.OPEN) return false;
  state.socket.send(JSON.stringify({ type, payload }));
  return true;
}

function handleServerMessage({ type, payload }) {
  switch (type) {
    case "welcome":
      state.clientId = payload.client_id;
      cardCache.clear();
      state.limits = payload.limits || state.limits;
      state.peers = payload.peers || [];
      state.items.clear();
      for (const item of payload.items || []) state.items.set(item.id, item);
      renderLimits();
      renderPeers();
      renderStream();
      break;

    case "item.created":
      state.items.set(payload.id, payload);
      renderStream({ highlight: payload.id });
      break;

    case "item.deleted":
      state.items.delete(payload.id);
      renderStream();
      break;

    case "room.cleared":
      state.items.clear();
      cardCache.clear();
      renderStream();
      break;

    case "presence":
      state.peers = payload.peers || [];
      renderPeers();
      break;

    case "error":
      toast(payload.message || "Something went wrong", "error");
      break;
  }
}

function setStatus(status) {
  const dot = $("statusDot");
  dot.className = `dot ${status}`;
  dot.title = { online: "Connected", connecting: "Connecting…", offline: "Disconnected" }[status];
}

/* ── Sending content ───────────────────────────────────────── */

async function sendText(content) {
  const text = (content || "").replace(/\s+$/, "");
  if (!text) return;
  if (send("text.create", { content: text })) return;

  // Socket is down — fall back to the REST route so the paste is not lost.
  try {
    await api(`/api/rooms/${state.room}/items`, {
      method: "POST",
      body: JSON.stringify({ content: text, device: state.device }),
    });
  } catch (err) {
    toast(err.message, "error");
  }
}

function uploadFiles(fileList) {
  const files = Array.from(fileList || []);
  if (!files.length) return;
  const limit = state.limits.max_upload_bytes;

  for (const file of files) {
    if (file.size > limit) {
      toast(`${file.name || "That file"} is larger than ${formatBytes(limit)}`, "error");
      continue;
    }
    uploadOne(file);
  }
}

// One request per file keeps progress meaningful and stops a single bad file
// from failing the whole batch.
function uploadOne(file) {
  const row = document.createElement("div");
  row.className = "upload";
  row.innerHTML =
    '<span class="upload-name"></span><span class="upload-pct">0%</span>' +
    '<span class="upload-track"><span class="upload-fill"></span></span>';
  row.querySelector(".upload-name").textContent = file.name || "pasted file";
  $("uploads").append(row);

  const fill = row.querySelector(".upload-fill");
  const pct = row.querySelector(".upload-pct");

  const form = new FormData();
  form.append("device", state.device);
  form.append("file", file, file.name || `pasted-${Date.now()}`);

  const xhr = new XMLHttpRequest();
  xhr.open("POST", `/api/rooms/${state.room}/upload`);
  xhr.setRequestHeader("X-Device-Name", state.device);

  xhr.upload.addEventListener("progress", (event) => {
    if (!event.lengthComputable) return;
    const ratio = event.loaded / event.total;
    fill.style.width = `${ratio * 100}%`;
    pct.textContent = `${Math.round(ratio * 100)}%`;
  });

  xhr.addEventListener("load", () => {
    if (xhr.status >= 200 && xhr.status < 300) {
      fill.style.width = "100%";
      pct.textContent = "done";
      setTimeout(() => row.remove(), 700);
      // The websocket broadcast renders the card; nothing else to do here.
      return;
    }
    let message = `Upload failed (${xhr.status})`;
    try {
      message = JSON.parse(xhr.responseText).error || message;
    } catch { /* non-JSON error body */ }
    failUpload(row, pct, message);
  });

  xhr.addEventListener("error", () => failUpload(row, pct, "Upload failed — check your connection"));
  xhr.addEventListener("abort", () => row.remove());

  xhr.send(form);
}

function failUpload(row, pct, message) {
  row.classList.add("failed");
  pct.textContent = "failed";
  toast(message, "error");
  setTimeout(() => row.remove(), 4000);
}

function deleteItem(id) {
  state.items.delete(id);
  renderStream();
  if (send("item.delete", { id })) return;
  api(`/api/rooms/${state.room}/items/${id}`, { method: "DELETE" })
    .catch((err) => toast(err.message, "error"));
}

/* ── Copying ───────────────────────────────────────────────── */

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    toast("Copied to clipboard");
  } catch {
    // Clipboard API needs a secure context; fall back to a hidden textarea.
    const area = document.createElement("textarea");
    area.value = text;
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.append(area);
    area.select();
    const ok = document.execCommand?.("copy");
    area.remove();
    toast(ok ? "Copied to clipboard" : "Copying is blocked in this browser", ok ? "" : "error");
  }
}

async function copyImage(item) {
  const url = `/api/items/${item.id}/raw`;
  try {
    if (!window.ClipboardItem) throw new Error("unsupported");
    const blob = await (await fetch(url)).blob();
    // Browsers only reliably accept PNG on the clipboard.
    const png = blob.type === "image/png" ? blob : await toPng(blob);
    await navigator.clipboard.write([new ClipboardItem({ "image/png": png })]);
    toast("Image copied");
  } catch {
    await copyText(new URL(url, location.origin).href);
  }
}

function toPng(blob) {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => {
      const canvas = document.createElement("canvas");
      canvas.width = img.naturalWidth;
      canvas.height = img.naturalHeight;
      canvas.getContext("2d").drawImage(img, 0, 0);
      canvas.toBlob((out) => (out ? resolve(out) : reject(new Error("encode failed"))), "image/png");
    };
    img.onerror = () => reject(new Error("decode failed"));
    img.src = URL.createObjectURL(blob);
  });
}

/* ── Rendering ─────────────────────────────────────────────── */

const KIND_META = {
  text: { icon: "#i-text", label: "Text" },
  link: { icon: "#i-link", label: "Link" },
  image: { icon: "#i-image", label: "Image" },
  file: { icon: "#i-file", label: "File" },
};

function matchesFilter(item) {
  if (state.filter === "all") return true;
  if (state.filter === "text") return item.kind === "text" || item.kind === "link";
  return item.kind === state.filter;
}

// Cards are cached by item id and moved rather than rebuilt, so an arriving
// item never makes the other images flash or collapses an expanded paste.
const cardCache = new Map();

function renderStream({ highlight } = {}) {
  const stream = $("stream");
  const items = [...state.items.values()].sort(
    (a, b) => new Date(b.created_at) - new Date(a.created_at)
  );
  const visible = items.filter(matchesFilter);

  for (const id of cardCache.keys()) {
    if (!state.items.has(id)) cardCache.delete(id);
  }

  stream.replaceChildren(...visible.map((item) => {
    let card = cardCache.get(item.id);
    if (!card) {
      card = buildCard(item, item.id === highlight);
      cardCache.set(item.id, card);
    }
    return card;
  }));
  $("empty").hidden = visible.length > 0;

  const bytes = items.reduce((total, item) => total + (item.size_bytes || 0), 0);
  $("statItems").textContent = String(items.length);
  $("statBytes").textContent = formatBytes(bytes);
}

function buildCard(item, isNew) {
  const node = $("tpl-card").content.firstElementChild.cloneNode(true);
  const meta = KIND_META[item.kind] || KIND_META.file;

  node.dataset.id = item.id;
  node.classList.add(`card-${item.kind}`);
  if (isNew) {
    node.classList.add("is-new");
    setTimeout(() => node.classList.remove("is-new"), 1600);
  }

  const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
  use.setAttribute("href", meta.icon);
  node.querySelector(".kind svg").append(use);
  node.querySelector(".kind-label").textContent = meta.label;

  const ago = node.querySelector(".ago");
  ago.dateTime = item.created_at;
  ago.textContent = formatAgo(item.created_at);

  node.querySelector(".device").textContent = item.device || "unknown device";
  node.querySelector(".size").textContent = item.size_bytes ? formatBytes(item.size_bytes) : "";

  renderBody(node, item);

  const download = node.querySelector(".act-download");
  if (item.kind === "image" || item.kind === "file") {
    download.addEventListener("click", () => {
      window.location.href = `/api/items/${item.id}/download`;
    });
  } else {
    download.remove();
  }

  node.querySelector(".act-copy").addEventListener("click", () => {
    if (item.kind === "image") copyImage(item);
    else if (item.kind === "file") copyText(new URL(`/api/items/${item.id}/download`, location.origin).href);
    else copyText(item.content);
  });

  node.querySelector(".act-delete").addEventListener("click", () => {
    node.classList.add("leaving");
    setTimeout(() => deleteItem(item.id), 140);
  });

  return node;
}

function renderBody(node, item) {
  const body = node.querySelector(".card-body");

  if (item.kind === "image") {
    const img = document.createElement("img");
    img.className = "thumb";
    img.loading = "lazy";
    img.decoding = "async";
    img.alt = item.file_name || "Pasted image";
    img.src = `/api/items/${item.id}/raw`;
    if (item.width && item.height) {
      img.width = item.width;
      img.height = item.height;
    }
    img.addEventListener("click", () => openLightbox(item));
    body.append(img);
    return;
  }

  if (item.kind === "file") {
    const row = document.createElement("div");
    row.className = "file-row";

    const icon = document.createElement("span");
    icon.className = "file-icon";
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
    use.setAttribute("href", "#i-file");
    svg.append(use);
    icon.append(svg);

    const metaBox = document.createElement("div");
    metaBox.className = "file-meta";
    const name = document.createElement("div");
    name.className = "file-name";
    name.textContent = item.file_name || "file";
    const sub = document.createElement("div");
    sub.className = "file-sub";
    sub.textContent = item.mime_type || "";
    metaBox.append(name, sub);

    row.append(icon, metaBox);
    body.append(row);
    return;
  }

  if (item.kind === "link") {
    const row = document.createElement("div");
    row.className = "link-row";
    const anchor = document.createElement("a");
    anchor.href = item.content;
    anchor.target = "_blank";
    anchor.rel = "noopener noreferrer";
    anchor.textContent = item.content;
    row.append(anchor);
    body.append(row);
    return;
  }

  const pre = document.createElement("pre");
  pre.textContent = item.content;
  body.append(pre);

  // Long pastes get clipped with a toggle rather than swallowing the grid.
  requestAnimationFrame(() => {
    if (pre.scrollHeight <= pre.clientHeight + 4) return;
    node.classList.add("clipped");
    const more = document.createElement("button");
    more.className = "more";
    more.textContent = "Show more";
    more.addEventListener("click", () => {
      const open = node.classList.toggle("open");
      pre.classList.toggle("expanded", open);
      more.textContent = open ? "Show less" : "Show more";
    });
    body.append(more);
  });
}

function renderPeers() {
  const list = $("peerList");
  list.replaceChildren(
    ...state.peers.map((peer) => {
      const li = document.createElement("li");
      const dot = document.createElement("span");
      dot.className = "dot";
      const name = document.createElement("span");
      name.textContent = peer.device;
      li.append(dot, name);
      if (peer.id === state.clientId) {
        const you = document.createElement("span");
        you.className = "you";
        you.textContent = "this device";
        li.append(you);
      }
      return li;
    })
  );
  $("peerCount").textContent = String(state.peers.length);
  $("peerCountSide").textContent = String(state.peers.length);
}

function renderLimits() {
  $("statRetention").textContent = formatRetention(state.limits.retention);
  $("statMaxUpload").textContent = formatBytes(state.limits.max_upload_bytes);
}

// Keep "2 minutes ago" honest without re-rendering the whole grid.
setInterval(() => {
  for (const el of document.querySelectorAll(".ago[datetime]")) {
    el.textContent = formatAgo(el.dateTime);
  }
}, 30000);

/* ── Lightbox ──────────────────────────────────────────────── */

function openLightbox(item) {
  const box = $("lightbox");
  $("lightboxImg").src = `/api/items/${item.id}/raw`;
  $("lightboxImg").alt = item.file_name || "Pasted image";

  const bar = $("lightboxBar");
  const copy = document.createElement("button");
  copy.className = "btn btn-ghost btn-sm";
  copy.textContent = "Copy image";
  copy.addEventListener("click", () => copyImage(item));

  const download = document.createElement("a");
  download.className = "btn btn-ghost btn-sm";
  download.href = `/api/items/${item.id}/download`;
  download.textContent = "Download";

  bar.replaceChildren(copy, download);
  box.hidden = false;
}

function closeLightbox() {
  $("lightbox").hidden = true;
  $("lightboxImg").src = "";
}

/* ── QR modal ──────────────────────────────────────────────── */

// Hosts a phone on the same wifi cannot resolve. Worth calling out, because a
// QR pointing at localhost fails in a way that looks like a broken app.
const UNREACHABLE_HOSTS = new Set(["localhost", "127.0.0.1", "[::1]", "::1", "0.0.0.0"]);

let qrLastFocus = null;

function openQR() {
  if (!state.room) return;
  const link = `${location.origin}/r/${state.room}`;

  // The server builds the QR from the request host, so it matches whatever
  // address this page was opened on.
  $("qrImage").src = `/api/rooms/${encodeURIComponent(state.room)}/qr.svg`;
  $("qrURL").textContent = link;

  const warning = $("qrWarning");
  if (UNREACHABLE_HOSTS.has(location.hostname)) {
    warning.textContent =
      "This page is open on " + location.hostname + ", which your phone cannot reach. " +
      "Reopen the dashboard at your computer's network address, then scan again.";
    warning.hidden = false;
  } else {
    warning.hidden = true;
  }

  $("qrShare").hidden = !navigator.share;

  // On a phone the sidebar is a drawer; leaving it open behind the modal makes
  // closing the QR feel like it did not work.
  $("sidebar").classList.remove("open");
  $("scrim").hidden = true;

  qrLastFocus = document.activeElement;
  $("qrModal").hidden = false;
  $("qrClose").focus();
}

function closeQR() {
  $("qrModal").hidden = true;
  if (qrLastFocus instanceof HTMLElement) qrLastFocus.focus();
  qrLastFocus = null;
}

function wireQR() {
  const link = () => `${location.origin}/r/${state.room}`;

  $("qrBtn").addEventListener("click", openQR);
  $("showQR").addEventListener("click", openQR);
  $("qrClose").addEventListener("click", closeQR);
  $("qrCopy").addEventListener("click", () => copyText(link()));

  $("qrShare").addEventListener("click", async () => {
    try {
      await navigator.share({ title: "Clipboard room", text: `Room ${state.room}`, url: link() });
    } catch {
      /* the user dismissed the share sheet */
    }
  });

  $("qrModal").addEventListener("click", (event) => {
    if (event.target === $("qrModal")) closeQR();
  });

  $("qrImage").addEventListener("error", () => {
    toast("Could not load the QR code", "error");
  });
}

/* ── Paste, drag and drop ──────────────────────────────────── */

function wirePasteAndDrop() {
  document.addEventListener("paste", (event) => {
    if (!state.room) return;
    const data = event.clipboardData;
    if (!data) return;

    const files = collectFiles(data);
    if (files.length) {
      event.preventDefault();
      uploadFiles(files);
      return;
    }

    // Text pasted into the composer behaves like a normal paste so it can be
    // edited first; anywhere else it goes straight to the room.
    if (document.activeElement === $("composerInput")) return;

    const text = data.getData("text/plain");
    if (text && text.trim()) {
      event.preventDefault();
      sendText(text);
    }
  });

  let dragDepth = 0;
  const dropzone = $("dropzone");

  window.addEventListener("dragenter", (event) => {
    if (!state.room || !hasFiles(event)) return;
    dragDepth += 1;
    dropzone.hidden = false;
  });
  window.addEventListener("dragover", (event) => {
    if (!state.room || !hasFiles(event)) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
  });
  window.addEventListener("dragleave", () => {
    dragDepth = Math.max(0, dragDepth - 1);
    if (dragDepth === 0) dropzone.hidden = true;
  });
  window.addEventListener("drop", (event) => {
    if (!state.room) return;
    event.preventDefault();
    dragDepth = 0;
    dropzone.hidden = true;
    uploadFiles(event.dataTransfer?.files);
  });
}

function hasFiles(event) {
  return Array.from(event.dataTransfer?.types || []).includes("Files");
}

// Clipboard payloads arrive as files on some platforms and as items on others,
// so read both and de-duplicate.
function collectFiles(data) {
  const files = Array.from(data.files || []);
  if (files.length) return files;
  return Array.from(data.items || [])
    .filter((entry) => entry.kind === "file")
    .map((entry) => entry.getAsFile())
    .filter(Boolean);
}

/* ── Composer ──────────────────────────────────────────────── */

function wireComposer() {
  const input = $("composerInput");

  const autoGrow = () => {
    input.style.height = "auto";
    input.style.height = `${input.scrollHeight}px`;
  };
  input.addEventListener("input", autoGrow);

  const submit = () => {
    const text = input.value;
    if (!text.trim()) return;
    sendText(text);
    input.value = "";
    autoGrow();
  };

  $("sendText").addEventListener("click", submit);
  input.addEventListener("keydown", (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key === "Enter") {
      event.preventDefault();
      submit();
    }
  });

  $("pickFiles").addEventListener("click", () => $("fileInput").click());
  $("fileInput").addEventListener("change", (event) => {
    uploadFiles(event.target.files);
    event.target.value = "";
  });
}

/* ── Chrome: sidebar, theme, share, filters ────────────────── */

function wireChrome() {
  const sidebar = $("sidebar");
  const scrim = $("scrim");
  const openSidebar = (open) => {
    sidebar.classList.toggle("open", open);
    scrim.hidden = !open;
  };
  $("menuBtn").addEventListener("click", () => openSidebar(true));
  $("closeSidebar").addEventListener("click", () => openSidebar(false));
  scrim.addEventListener("click", () => openSidebar(false));

  const roomLink = () => `${location.origin}/r/${state.room}`;
  $("roomChip").addEventListener("click", () => copyText(roomLink()));
  $("copyLink").addEventListener("click", () => copyText(roomLink()));

  $("shareBtn").addEventListener("click", async () => {
    if (navigator.share) {
      try {
        await navigator.share({ title: "Clipboard room", text: `Room ${state.room}`, url: roomLink() });
        return;
      } catch {
        return; // the user dismissed the share sheet
      }
    }
    copyText(roomLink());
  });

  $("leaveRoom").addEventListener("click", () => {
    closeRoom();
    history.pushState({}, "", "/");
    route();
  });

  $("clearRoom").addEventListener("click", () => {
    if (!confirm("Delete every item in this room? This cannot be undone.")) return;
    if (!send("room.clear")) {
      api(`/api/rooms/${state.room}/items`, { method: "DELETE" })
        .then(() => {
          state.items.clear();
          renderStream();
        })
        .catch((err) => toast(err.message, "error"));
    }
  });

  $("filters").addEventListener("click", (event) => {
    const chip = event.target.closest(".chip");
    if (!chip) return;
    state.filter = chip.dataset.filter;
    for (const other of $("filters").children) {
      other.classList.toggle("is-active", other === chip);
    }
    renderStream();
  });

  $("lightboxClose").addEventListener("click", closeLightbox);
  $("lightbox").addEventListener("click", (event) => {
    if (event.target === $("lightbox")) closeLightbox();
  });

  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    if (!$("qrModal").hidden) closeQR();
    else if (!$("lightbox").hidden) closeLightbox();
    else openSidebar(false);
  });

  wireTheme();
}

function wireTheme() {
  const stored = localStorage.getItem("clipboard.theme");
  if (stored) document.documentElement.dataset.theme = stored;
  syncThemeIcon();

  $("themeBtn").addEventListener("click", () => {
    const prefersDark = matchMedia("(prefers-color-scheme: dark)").matches;
    const current = document.documentElement.dataset.theme || (prefersDark ? "dark" : "light");
    const next = current === "dark" ? "light" : "dark";
    document.documentElement.dataset.theme = next;
    localStorage.setItem("clipboard.theme", next);
    syncThemeIcon();
  });
}

function syncThemeIcon() {
  const prefersDark = matchMedia("(prefers-color-scheme: dark)").matches;
  const dark = (document.documentElement.dataset.theme || (prefersDark ? "dark" : "light")) === "dark";
  $("themeBtn").querySelector("use").setAttribute("href", dark ? "#i-sun" : "#i-moon");
}

/* ── Landing ───────────────────────────────────────────────── */

function wireLanding() {
  $("createRoom").addEventListener("click", async () => {
    const button = $("createRoom");
    button.disabled = true;
    try {
      const room = await api("/api/rooms", { method: "POST" });
      goToRoom(room.code);
    } catch (err) {
      toast(err.message, "error");
    } finally {
      button.disabled = false;
    }
  });

  $("joinForm").addEventListener("submit", (event) => {
    event.preventDefault();
    const code = $("joinCode").value.trim().toLowerCase().replace(/[^a-z0-9-]/g, "");
    if (code.length < 4) {
      toast("That code looks too short", "error");
      return;
    }
    goToRoom(code);
  });

  const last = localStorage.getItem("clipboard.lastRoom");
  if (last) {
    $("landingFinePrint").textContent = `You were last in room ${last}.`;
    $("joinCode").value = last;
  }
}

/* ── PWA ───────────────────────────────────────────────────── */

function wirePWA() {
  if ("serviceWorker" in navigator) {
    window.addEventListener("load", () => {
      navigator.serviceWorker.register("/sw.js").catch(() => {
        /* offline support is a bonus, not a requirement */
      });
    });
  }

  let installPrompt = null;
  window.addEventListener("beforeinstallprompt", (event) => {
    event.preventDefault();
    installPrompt = event;
    $("installPanel").hidden = false;
  });

  $("installBtn").addEventListener("click", async () => {
    if (!installPrompt) return;
    installPrompt.prompt();
    await installPrompt.userChoice;
    installPrompt = null;
    $("installPanel").hidden = true;
  });

  window.addEventListener("appinstalled", () => {
    $("installPanel").hidden = true;
  });
}

/* ── Boot ──────────────────────────────────────────────────── */

state.device = deviceName();
renderLimits();
wireLanding();
wireChrome();
wireComposer();
wireQR();
wirePasteAndDrop();
wirePWA();

window.addEventListener("popstate", route);

// A sleeping laptop closes sockets silently; re-check as soon as we are visible.
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "visible" && state.room && !state.connected) {
    state.retry = 0;
    connect();
  }
});
window.addEventListener("online", () => {
  if (state.room && !state.connected) {
    state.retry = 0;
    connect();
  }
});

route();
