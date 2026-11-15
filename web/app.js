"use strict";

// The secret token is in the page address after "#". It never leaves this
// computer: the address fragment isn't sent with requests.
const token = location.hash.slice(1);

const $ = (id) => document.getElementById(id);

const state = {
  status: null,
  apps: null,      // { remove, disable, protected }
  pending: new Map(), // id -> action
  busy: false,
  picture: null,   // File
};

// ---------------------------------------------------------------------------
// API
// ---------------------------------------------------------------------------

async function api(path, options = {}) {
  const res = await fetch("/api/" + path, {
    ...options,
    headers: { "X-Token": token, ...(options.headers || {}) },
  });
  let data = {};
  try { data = await res.json(); } catch (_) { /* empty body */ }
  if (!res.ok) throw new Error(data.error || "Something went wrong (" + res.status + ").");
  return data;
}

const postJSON = (path, body) =>
  api(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body || {}) });

// ---------------------------------------------------------------------------
// Small DOM helpers
// ---------------------------------------------------------------------------

function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (v === false || v == null) continue;
    if (k === "class") node.className = v;
    else if (k.startsWith("on")) node.addEventListener(k.slice(2), v);
    else node.setAttribute(k, v === true ? "" : v);
  }
  for (const c of children.flat()) {
    if (c == null || c === false) continue;
    node.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return node;
}

function show(id, visible) { $(id).hidden = !visible; }

function flash() {
  document.body.classList.remove("flash");
  void document.body.offsetWidth; // restart the animation
  document.body.classList.add("flash");
  setTimeout(() => document.body.classList.remove("flash"), 500);
}

// ---------------------------------------------------------------------------
// Connection status
// ---------------------------------------------------------------------------

const installHelp = {
  mac: ["Install it with Homebrew:", "brew install --cask android-platform-tools"],
  windows: ["Install it with Scoop:", "scoop install adb"],
  linux: ["Install it with your package manager, for example:", "sudo apt install adb"],
};

function renderMessage(status) {
  const box = $("message");
  box.replaceChildren();
  let title, lines = [];
  if (!token) {
    title = "This page needs to be opened from the app.";
    lines = ["Run boox-debloat again; it opens the right address for you."];
  } else if (!status.adb) {
    const help = installHelp[status.os] || installHelp.linux;
    title = "adb isn't installed.";
    lines = ["boox-debloat uses adb to talk to the tablet.", help[0], el("code", {}, help[1]), "Then run boox-debloat again."];
  } else if (status.state === "unauthorized") {
    title = "Allow USB debugging on the tablet.";
    lines = ["A prompt is waiting on the tablet's screen. Tap Allow, and tick Always allow from this computer."];
  } else if (status.state === "multiple") {
    title = "More than one Android device is connected.";
    lines = ["Unplug the others, or start the app with boox-debloat -serial <serial>."];
  } else if (status.state === "offline") {
    title = "The tablet isn't responding.";
    lines = ["Unplug it, plug it back in, and unlock the screen."];
  } else {
    title = "Plug in your tablet.";
    lines = [
      "Connect it with a USB cable that carries data.",
      "On the tablet, turn on USB Debug Mode: Apps, then the menu, then App management.",
      "This page updates by itself once the tablet is found.",
    ];
  }
  box.append(el("h2", {}, title), ...lines.map((l) => (l instanceof Node ? el("p", {}, l) : el("p", {}, l))));
}

function renderDevice(status) {
  show("demo", status.demo);
  const connected = status.adb && status.state === "device";
  if (!connected) {
    $("deviceName").textContent = "Boox Debloat";
    $("deviceMeta").textContent = "No tablet connected";
    show("sleepStatus", false);
    show("pictureForm", false);
    return;
  }
  $("deviceName").textContent = status.model === "C69_M_STAR" ? "C69 M Star" : status.model || "Android device";
  const fw = (status.firmware.match(/(\d+\.\d+\.\d+)/) || [])[1] || status.firmware;
  const date = (status.firmware.match(/^(\d{4}-\d{2}-\d{2})/) || [])[1];
  $("deviceMeta").textContent = `Firmware ${fw}${date ? " (" + date + ")" : ""}, Android ${status.android}`;
  show("pictureForm", status.supported);
}

async function refreshStatus() {
  if (state.busy) return;
  let status;
  try {
    status = token ? await api("status") : { adb: true, state: "none" };
  } catch (err) {
    status = { adb: true, state: "none", error: err.message };
  }
  const was = state.status;
  state.status = status;
  renderDevice(status);

  const connected = token && status.adb && status.state === "device";
  show("message", !connected);
  show("lists", connected);
  show("bar", connected);
  if (!connected) {
    renderMessage(status);
    state.apps = null;
    state.pending.clear();
    show("result", false);
    return;
  }
  show("unsupported", !status.supported);
  if (!status.supported) {
    $("unsupported").textContent =
      `This tool is made for the C69 M Star. This device reports "${status.model}", so changes are turned off.`;
  }
  if (!was || was.state !== "device" || was.serial !== status.serial || !state.apps) {
    await loadApps();
  }
}

// ---------------------------------------------------------------------------
// App lists
// ---------------------------------------------------------------------------

async function loadApps() {
  try {
    state.apps = await api("apps");
  } catch (err) {
    showResult([el("p", { class: "bad" }, err.message)]);
    return;
  }
  render();
}

const installed = (s) => s === "enabled" || s === "disabled";

// What the switch shows: the app's state once pending changes are applied.
function desiredOn(app) {
  const a = state.pending.get(app.id);
  if (a === "remove" || a === "disable") return false;
  if (a === "restore" || a === "enable") return true;
  return app.state === "enabled" || (app.state === "disabled" && app._group === "remove");
}

function toggle(app) {
  const on = desiredOn(app);
  state.pending.delete(app.id);
  if (app._group === "remove") {
    if (on && installed(app.state)) state.pending.set(app.id, "remove");
    if (!on && app.state === "removed") state.pending.set(app.id, "restore");
  } else if (app._group === "disable") {
    if (on && app.state === "enabled") state.pending.set(app.id, "disable");
    if (!on && app.state === "disabled") state.pending.set(app.id, "enable");
    if (!on && app.state === "removed") state.pending.set(app.id, "restore");
  }
  render();
}

const pendingText = {
  remove: "Will be removed",
  restore: "Will come back",
  disable: "Will be turned off",
  enable: "Will be turned on",
};

function appRow(app) {
  const supported = state.status && state.status.supported;
  const pending = state.pending.get(app.id);
  const lines = [
    el("span", { class: "row-name" }, app.name),
    el("span", { class: "row-why" }, app.why),
    el("span", { class: "row-id" }, app.id),
  ];
  if (pending) lines.push(el("span", { class: "row-pending" }, pendingText[pending]));
  const text = el("div", { class: "row-text" }, ...lines);

  if (app.state === "missing") {
    return el("li", { class: "row missing" }, text, el("span", { class: "lock" }, "Not on this firmware"));
  }

  if (app._group === "protected") {
    const problem = app.state === "removed" || app.state === "disabled";
    if (!problem) return el("li", { class: "row locked" }, text, el("span", { class: "lock" }, "Kept"));
    const action = app.state === "removed" ? "restore" : "enable";
    const queued = state.pending.has(app.id);
    const btn = el("button", {
      type: "button",
      class: "text-button",
      disabled: !supported,
      onclick: () => {
        if (queued) state.pending.delete(app.id); else state.pending.set(app.id, action);
        render();
      },
    }, queued ? "Undo" : "Bring back");
    return el("li", { class: "row problem" }, text, btn);
  }

  const on = desiredOn(app);
  const sw = el("button", {
    type: "button",
    class: "switch",
    role: "switch",
    "aria-checked": String(on),
    "aria-label": `${app.name} ${app._group === "disable" ? "turned on" : "installed"}`,
    disabled: !supported,
    onclick: () => toggle(app),
  });
  return el("li", { class: "row" }, text, sw);
}

function render() {
  if (!state.apps) return;
  const tag = (list, group) => list.map((a) => ({ ...a, _group: group }));
  const remove = tag(state.apps.remove, "remove");
  const disable = tag(state.apps.disable, "disable");
  const keep = tag(state.apps.protected, "protected");

  $("removeList").replaceChildren(...remove.map(appRow));
  $("disableList").replaceChildren(...disable.map(appRow));
  $("protectedList").replaceChildren(...keep.map(appRow));

  // Sleep screen health: the apps it needs must be enabled.
  const needed = ["com.onyx", "com.onyx.android.ksync", "com.onyx.screenmanager", "com.android.providers.media"];
  const broken = keep.filter((a) => needed.includes(a.id) && (a.state === "removed" || a.state === "disabled"));
  const status = $("sleepStatus");
  status.hidden = false;
  status.classList.toggle("bad", broken.length > 0);
  status.textContent = broken.length
    ? `Sleep screen: needs ${broken.map((a) => a.name).join(" and ")}. Bring it back under Always kept.`
    : "Sleep screen: ready";

  const supported = state.status && state.status.supported;
  const removable = remove.filter((a) => installed(a.state) && state.pending.get(a.id) !== "remove");
  $("removeAll").disabled = !supported || removable.length === 0;

  const n = state.pending.size;
  $("review").disabled = n === 0 || !supported;
  $("review").textContent = n === 0 ? "Review changes" : `Review ${n} change${n === 1 ? "" : "s"}`;
  show("discard", n > 0);
  $("restoreAll").disabled = !supported;
}

$("removeAll").addEventListener("click", () => {
  for (const a of state.apps.remove) {
    if (installed(a.state)) state.pending.set(a.id, "remove");
  }
  render();
});

$("discard").addEventListener("click", () => {
  state.pending.clear();
  render();
});

// ---------------------------------------------------------------------------
// Review and apply
// ---------------------------------------------------------------------------

function nameOf(id) {
  for (const list of Object.values(state.apps)) {
    const a = list.find((x) => x.id === id);
    if (a) return a.name;
  }
  return id;
}

const groupTitles = {
  remove: "Remove",
  restore: "Bring back",
  disable: "Turn off",
  enable: "Turn on",
};

function confirmDialog(title, body, okText) {
  $("dialogTitle").textContent = title;
  $("dialogBody").replaceChildren(...body);
  $("dialogOk").textContent = okText;
  const dialog = $("dialog");
  return new Promise((resolve) => {
    dialog.addEventListener("close", () => resolve(dialog.returnValue === "ok"), { once: true });
    dialog.showModal();
  });
}

$("review").addEventListener("click", async () => {
  const byAction = {};
  for (const [id, action] of state.pending) (byAction[action] ||= []).push(id);
  const body = Object.entries(byAction).map(([action, ids]) =>
    el("div", {},
      el("h3", {}, `${groupTitles[action]} (${ids.length})`),
      el("ul", {}, ids.map((id) => el("li", {}, nameOf(id))))));
  if (!(await confirmDialog("Review changes", body, "Apply to tablet"))) return;
  const changes = [...state.pending].map(([id, action]) => ({ id, action }));
  await apply(() => postJSON("apply", { changes }));
});

$("restoreAll").addEventListener("click", async () => {
  const body = [el("p", {}, "Every app this tool can remove comes back, and NFC is turned on again. Anything you changed by hand stays as it is.")];
  if (!(await confirmDialog("Restore all apps?", body, "Restore all apps"))) return;
  await apply(() => postJSON("restore-all"));
});

async function apply(call) {
  state.busy = true;
  setBusy(true, "Applying…");
  try {
    const res = await call();
    state.pending.clear();
    flash();
    reportResults(res);
  } catch (err) {
    showResult([el("p", { class: "bad" }, err.message)]);
  } finally {
    state.busy = false;
    setBusy(false);
    await loadApps();
  }
}

function setBusy(on, text) {
  for (const id of ["review", "restoreAll", "removeAll", "discard"]) $(id).disabled = on;
  if (on) $("review").textContent = text;
  document.body.style.cursor = on ? "progress" : "";
}

function reportResults(res) {
  const ok = res.results.filter((r) => r.ok);
  const failed = res.results.filter((r) => !r.ok);
  const parts = [];
  if (res.results.length === 0) {
    parts.push(el("p", {}, "Nothing to change. Every app is already back."));
  } else {
    parts.push(el("p", {}, el("strong", {}, `Applied ${ok.length} change${ok.length === 1 ? "" : "s"}.`), " Restart the tablet to finish."));
  }
  if (failed.length) {
    parts.push(el("p", { class: "bad" }, `${failed.length} didn't work:`));
    parts.push(el("ul", {}, failed.map((r) => el("li", { class: "bad" }, `${nameOf(r.id)}: ${r.message || "no answer from the tablet"}`))));
  }
  if (res.backup) parts.push(el("p", { class: "row-why" }, "Backup: ", res.backup));
  if (res.results.length) {
    parts.push(el("button", { type: "button", class: "button", onclick: reboot }, "Restart tablet"));
  }
  showResult(parts);
}

function showResult(parts) {
  const box = $("result");
  box.replaceChildren(...parts);
  box.hidden = false;
  box.scrollIntoView({ behavior: "smooth", block: "nearest" });
}

async function reboot() {
  try {
    await postJSON("reboot");
    showResult([el("p", {}, "Restarting. The tablet reconnects here when it's back on.")]);
  } catch (err) {
    showResult([el("p", { class: "bad" }, err.message)]);
  }
}

// ---------------------------------------------------------------------------
// Sleep picture
// ---------------------------------------------------------------------------

function choosePicture(file) {
  if (!file || !file.type.startsWith("image/")) {
    setPictureInfo("That isn't a picture. Use PNG or JPG.", true);
    return;
  }
  state.picture = file;
  show("pictureDone", false);
  const url = URL.createObjectURL(file);
  const img = $("preview");
  img.onload = () => {
    const w = img.naturalWidth, h = img.naturalHeight;
    const mb = (file.size / 1e6).toFixed(1);
    const exact = w === 758 && h === 1024;
    let text = `${file.name}, ${w} × ${h}, ${mb} MB.`;
    if (!exact && $("fit").checked) text += " It will be resized to 758 × 1024.";
    if (!exact && !$("fit").checked) text += " The best size is 758 × 1024.";
    setPictureInfo(text, !$("fit").checked && file.size > 10e6);
  };
  img.src = url;
  img.hidden = false;
  show("screenHint", false);
  $("sendPicture").disabled = false;
}

function setPictureInfo(text, bad) {
  $("pictureInfo").textContent = text;
  $("pictureInfo").classList.toggle("bad", !!bad);
}

$("pictureInput").addEventListener("change", (e) => choosePicture(e.target.files[0]));
$("fit").addEventListener("change", () => state.picture && choosePicture(state.picture));
$("screen").addEventListener("click", () => {
  if (!$("pictureForm").hidden) $("pictureInput").click();
});

const tablet = $("tablet");
tablet.addEventListener("dragover", (e) => {
  if ($("pictureForm").hidden) return;
  e.preventDefault();
  tablet.classList.add("dragging");
});
tablet.addEventListener("dragleave", () => tablet.classList.remove("dragging"));
tablet.addEventListener("drop", (e) => {
  e.preventDefault();
  tablet.classList.remove("dragging");
  if (!$("pictureForm").hidden) choosePicture(e.dataTransfer.files[0]);
});

$("pictureForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  if (!state.picture) return;
  const form = new FormData();
  form.append("picture", state.picture);
  form.append("fit", $("fit").checked ? "1" : "0");
  const btn = $("sendPicture");
  btn.disabled = true;
  btn.textContent = "Sending…";
  state.busy = true;
  try {
    const res = await api("picture", { method: "POST", body: form });
    flash();
    const done = $("pictureDone");
    done.textContent = `Sent as ${res.name}. On the tablet: open Storage, then Pictures, long-press the picture and set it as the screensaver.`;
    done.hidden = false;
  } catch (err) {
    setPictureInfo(err.message, true);
  } finally {
    state.busy = false;
    btn.disabled = false;
    btn.textContent = "Send to tablet";
  }
});

// ---------------------------------------------------------------------------
// Start
// ---------------------------------------------------------------------------

refreshStatus();
setInterval(refreshStatus, 5000);
if (token) {
  postJSON("ping").catch(() => {});
  setInterval(() => postJSON("ping").catch(() => {}), 30000);
}
