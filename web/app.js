"use strict";

// The secret token is in the page address after "#". It never leaves this
// computer: the address fragment isn't sent with requests.
const token = location.hash.slice(1);

// A new address from the app means a new token: start fresh.
window.addEventListener("hashchange", () => location.reload());

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
  if (!res.ok) {
    const err = new Error(data.error || "Something went wrong (" + res.status + ").");
    err.status = res.status;
    throw err;
  }
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

const platformToolsURL = "https://developer.android.com/tools/releases/platform-tools";

const installCommand = {
  mac: "brew install --cask android-platform-tools",
  windows: "scoop install adb",
  linux: "sudo apt install adb",
};

// The steps to turn on USB debugging on the C69 M Star.
function usbDebugSteps() {
  return el("ol", { class: "steps" },
    el("li", {}, "On the tablet, open ", el("strong", {}, "Apps"), ", tap the menu at the top right, then ", el("strong", {}, "App management"), "."),
    el("li", {}, "Turn on ", el("strong", {}, "USB Debug Mode"), "."),
    el("li", {}, "Connect the tablet with a USB cable that carries data."),
    el("li", {}, "Tap ", el("strong", {}, "Allow"), " when the tablet asks to allow USB debugging, and tick ", el("strong", {}, "Always allow from this computer"), "."));
}

// Shown when adb is missing: install with one click, or download it.
function adbInstallBlock(status) {
  const parts = [el("p", {}, "boox-debloat uses Android platform tools (adb) to talk to the tablet.")];
  if (status.installer) {
    const via = status.installer === "brew" ? "Homebrew" : "Scoop";
    const out = el("p", { class: "install-out", hidden: true });
    const btn = el("button", { type: "button", class: "button" }, "Install Android platform tools");
    btn.addEventListener("click", async () => {
      state.busy = true; // keep the page from redrawing while it installs
      btn.disabled = true;
      btn.textContent = "Installing…";
      out.hidden = false;
      out.classList.remove("bad");
      out.textContent = `Installing with ${via}. This can take a few minutes.`;
      try {
        await postJSON("install-adb");
        out.textContent = "Installed. Looking for your tablet…";
        state.busy = false;
        await refreshStatus();
      } catch (err) {
        out.classList.add("bad");
        out.textContent = err.message;
        btn.disabled = false;
        btn.textContent = "Try again";
      } finally {
        state.busy = false;
      }
    });
    parts.push(el("div", { class: "install-row" }, btn, el("span", { class: "row-why" }, `Uses ${via}`)), out);
  } else {
    parts.push(el("p", {}, "Install it with this command, then press Refresh:"), el("p", {}, el("code", {}, installCommand[status.os] || installCommand.linux)));
  }
  parts.push(el("p", {}, "Or ", el("a", { href: platformToolsURL, target: "_blank", rel: "noopener noreferrer" }, "download Android platform tools from Google"), ", then press Refresh."));
  return parts;
}

function renderMessage(status) {
  const box = $("message");
  let title, body = [];
  if (!token) {
    title = "This page needs to be opened from the app.";
    body = [el("p", {}, "Run boox-debloat again; it opens the right address for you.")];
  } else if (status.expired) {
    title = "This page has expired.";
    body = [el("p", {}, "Close it and run boox-debloat again. Each run opens a new address.")];
  } else if (status.stopped) {
    title = "boox-debloat isn't running.";
    body = [el("p", {}, "Run boox-debloat again to reopen the app.")];
  } else if (!status.adb) {
    title = "Install Android platform tools.";
    body = adbInstallBlock(status);
  } else if (status.state === "unauthorized") {
    title = "Allow USB debugging on the tablet.";
    body = [el("p", {}, "A prompt is waiting on the tablet's screen. Unlock it, tap Allow, and tick Always allow from this computer.")];
  } else if (status.state === "multiple") {
    title = "More than one Android device is connected.";
    body = [el("p", {}, "Unplug the others, or start the app with boox-debloat -serial <serial>.")];
  } else if (status.state === "offline") {
    title = "The tablet isn't responding.";
    body = [el("p", {}, "Unplug it, plug it back in, and unlock the screen.")];
  } else {
    title = "Plug in your tablet and turn on USB debugging.";
    body = [usbDebugSteps(), el("p", { class: "row-why" }, "This page finds the tablet by itself. Press Refresh to check now.")];
  }
  // Only redraw when the message changes, so buttons and links stay put.
  const key = title + (status.installer || "");
  if (box.dataset.key === key) return;
  box.dataset.key = key;
  box.replaceChildren(el("h2", {}, title), ...body,
    el("p", { class: "row-why" }, "Stuck? Read the ", el("a", { href: "help.html", target: "_blank", rel: "noopener" }, "help and FAQ"), "."));
}

function renderDevice(status) {
  show("demo", status.demo);
  const connected = token && status.adb && status.state === "device";
  renderConnection(status, connected);
  if (!connected) {
    $("deviceName").textContent = "Boox Debloat";
    show("deviceMeta", false);
    show("sleepStatus", false);
    show("pictureForm", false);
    show("restartBox", false);
    return;
  }
  show("restartBox", true);
  $("deviceName").textContent = status.model === "C69_M_STAR" ? "C69 M Star" : status.model || "Android device";
  const fw = (status.firmware.match(/(\d+\.\d+\.\d+)/) || [])[1] || status.firmware;
  const meta = $("deviceMeta");
  meta.textContent = `Firmware ${fw}, Android ${status.android}`;
  meta.title = status.firmware; // full build name on hover
  show("deviceMeta", true);
  show("pictureForm", status.supported);
}

// The line under the tablet's name: a dot, what's happening, and Refresh.
function renderConnection(status, connected) {
  let text;
  if (connected) text = "Connected";
  else if (status.expired || status.stopped) text = "Not connected to the app";
  else if (status.adb === false) text = "adb isn't installed";
  else if (status.state === "unauthorized") text = "Waiting for USB permission";
  else if (status.state === "multiple") text = "More than one device connected";
  else text = "No tablet connected";
  $("connText").textContent = text;
  $("conn").classList.toggle("on", !!connected);
  // Refresh works whenever the app is running: it re-checks the connection
  // and, once connected, reloads the app list.
  show("refreshBtn", !!token && !status.expired && !status.stopped);
}

// Re-reads the tablet: connection, firmware and every app's state.
async function refreshAll() {
  const n = state.pending.size;
  if (n > 0) {
    const body = [el("p", {}, `Refreshing reloads the app list from the tablet and drops your ${n} unapplied change${n === 1 ? "" : "s"}.`)];
    if (!(await confirmDialog("Discard unapplied changes?", body, "Discard and refresh"))) return;
  }
  const btn = $("refreshBtn");
  btn.disabled = true;
  btn.textContent = "Refreshing…";
  state.pending.clear();
  state.apps = null; // makes refreshStatus reload the list
  try {
    await refreshStatus();
  } finally {
    btn.disabled = false;
    btn.textContent = "Refresh";
  }
}

$("refreshBtn").addEventListener("click", refreshAll);

async function refreshStatus() {
  if (state.busy) return;
  let status;
  try {
    status = token ? await api("status") : { adb: true, state: "none" };
  } catch (err) {
    status = {
      adb: true,
      state: "none",
      error: err.message,
      expired: err.status === 401,
      stopped: err.status === undefined, // the request never reached the app
    };
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

function confirmDialog(title, body, okText, { cancelText = "Cancel", note = "" } = {}) {
  $("dialogTitle").textContent = title;
  $("dialogBody").replaceChildren(...body);
  $("dialogOk").textContent = okText;
  $("dialogCancel").textContent = cancelText;
  $("dialogNote").textContent = note;
  $("dialogNote").hidden = !note;
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
  const note = "Your current app list is saved as a backup first.";
  if (!(await confirmDialog("Review changes", body, "Apply to tablet", { note }))) return;
  const changes = [...state.pending].map(([id, action]) => ({ id, action }));
  await apply(() => postJSON("apply", { changes }));
});

$("restoreAll").addEventListener("click", async () => {
  const body = [el("p", {}, "Every app this tool can remove comes back, and NFC is turned on again. Anything you changed by hand stays as it is.")];
  const note = "Your current app list is saved as a backup first.";
  if (!(await confirmDialog("Restore all apps?", body, "Restore all apps", { note }))) return;
  await apply(() => postJSON("restore-all"));
});

async function apply(call) {
  state.busy = true;
  setBusy(true, "Applying…");
  let changed = false;
  try {
    const res = await call();
    state.pending.clear();
    flash();
    reportResults(res);
    changed = res.results.some((r) => r.ok);
  } catch (err) {
    showResult([el("p", { class: "bad" }, err.message)]);
  } finally {
    state.busy = false;
    setBusy(false);
    await loadApps();
  }
  if (changed) await askToRestart();
}

// After changes, offer a restart: removed apps only fully go away after one.
async function askToRestart() {
  const body = [el("p", {}, "Your changes are applied. They finish taking effect after a restart, which takes about a minute.")];
  if (await confirmDialog("Restart the tablet now?", body, "Restart now", { cancelText: "Later" })) {
    await reboot();
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
  showResult(parts);
}

function showResult(parts) {
  const box = $("result");
  box.replaceChildren(...parts);
  box.hidden = false;
  box.scrollIntoView({ behavior: "smooth", block: "nearest" });
}

async function reboot() {
  $("restartBtn").disabled = true;
  try {
    await postJSON("reboot");
    showResult([el("p", {}, "Restarting. The tablet reconnects here when it's back on.")]);
  } catch (err) {
    showResult([el("p", { class: "bad" }, err.message)]);
  } finally {
    // Leave the button off for a while; the tablet takes about a minute.
    setTimeout(() => { $("restartBtn").disabled = false; }, 30000);
  }
}

$("restartBtn").addEventListener("click", async () => {
  const body = [el("p", {}, "The tablet turns off and on again, which takes about a minute. This page reconnects by itself.")];
  if (await confirmDialog("Restart the tablet?", body, "Restart tablet")) await reboot();
});

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
// Copy logs
// ---------------------------------------------------------------------------

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch (_) {
    // Older browsers: copy through a hidden text box.
    const box = el("textarea", { readonly: true, style: "position:fixed;opacity:0" });
    box.value = text;
    document.body.append(box);
    box.select();
    const ok = document.execCommand("copy");
    box.remove();
    return ok;
  }
}

let copyTimer;
$("copyLogs").addEventListener("click", async () => {
  const note = $("copyNote");
  note.classList.remove("bad");
  try {
    const res = await fetch("/api/logs", { headers: { "X-Token": token } });
    if (!res.ok) throw new Error(res.status === 401 ? "Page expired. Run boox-debloat again." : "Couldn't read the logs.");
    if (!(await copyText(await res.text()))) throw new Error("Couldn't copy. Try again.");
    note.textContent = "Copied";
  } catch (err) {
    note.classList.add("bad");
    note.textContent = err.message === "Failed to fetch" ? "The app isn't running." : err.message;
  }
  clearTimeout(copyTimer);
  copyTimer = setTimeout(() => { note.textContent = ""; }, 4000);
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
