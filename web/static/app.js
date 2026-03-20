/* ── Helpers ──────────────────────────────────────────────── */

function humanBytes(bytes) {
  if (bytes == null) return "—";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let i = 0;
  let v = bytes;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return (i === 0 ? v : v.toFixed(1).replace(/\.0$/, "")) + " " + units[i];
}

function statusClass(status) {
  const s = (status || "").toLowerCase();
  if (s === "running") return "badge-running";
  if (s === "stopped") return "badge-stopped";
  return "badge-other";
}

/* ── Toast Notifications ─────────────────────────────────── */

const toastArea = document.getElementById("toast-area");

function showToast(message, type) {
  type = type || "info";
  const el = document.createElement("div");
  el.className = "toast toast-" + type;
  el.textContent = message;
  toastArea.appendChild(el);
  setTimeout(function () {
    el.remove();
  }, 5000);
}

/* ── API Utility ─────────────────────────────────────────── */

async function api(method, path, body) {
  var opts = {
    method: method,
    headers: {},
  };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  var res;
  try {
    res = await fetch(path, opts);
  } catch (err) {
    throw new Error("Network error: " + err.message);
  }
  var json;
  try {
    json = await res.json();
  } catch (_) {
    throw new Error("Invalid JSON response (HTTP " + res.status + ")");
  }
  if (!res.ok) {
    throw new Error(json.error || "Request failed (HTTP " + res.status + ")");
  }
  return json.data;
}

/* ── Instance Actions ────────────────────────────────────── */

async function startInstance(name) {
  try {
    await api("POST", "/api/instances/" + encodeURIComponent(name) + "/start");
    showToast("Starting " + name + "…", "success");
  } catch (err) {
    showToast("Start failed: " + err.message, "error");
  }
  refreshInstances();
}

async function stopInstance(name) {
  try {
    await api("POST", "/api/instances/" + encodeURIComponent(name) + "/stop");
    showToast("Stopped " + name, "success");
  } catch (err) {
    showToast("Stop failed: " + err.message, "error");
  }
  refreshInstances();
}

async function restartInstance(name) {
  try {
    showToast("Restarting " + name + "…", "info");
    await api("POST", "/api/instances/" + encodeURIComponent(name) + "/restart");
    showToast("Restarted " + name, "success");
  } catch (err) {
    showToast("Restart failed: " + err.message, "error");
  }
  refreshInstances();
}

async function deleteInstance(name) {
  if (!confirm('Delete instance "' + name + '"? This cannot be undone.')) return;
  try {
    await api("DELETE", "/api/instances/" + encodeURIComponent(name));
    showToast("Deleted " + name, "success");
  } catch (err) {
    showToast("Delete failed: " + err.message, "error");
  }
  refreshInstances();
}

async function openConsole(name) {
  try {
    var data = await api(
      "GET",
      "/api/instances/" + encodeURIComponent(name) + "/vnc"
    );
    if (data && data.url) {
      window.open(data.url, "_blank");
    } else {
      showToast("No VNC URL returned", "error");
    }
  } catch (err) {
    showToast("Console error: " + err.message, "error");
  }
}

/* ── Terminal (xterm.js + WebSocket) ──────────────────────── */

var termOverlay = document.getElementById("terminal-modal-overlay");
var termContainer = document.getElementById("terminal-container");
var termTitle = document.getElementById("terminal-modal-title");
var termCloseBtn = document.getElementById("terminal-modal-close");
var activeTerm = null;
var activeFit = null;
var activeWs = null;
var termResizeHandler = null;

function openTerminal(name) {
  termTitle.textContent = "Terminal — " + name;
  termOverlay.classList.remove("hidden");
  termContainer.innerHTML = "";

  var term = new Terminal({
    cursorBlink: true,
    fontSize: 14,
    theme: { background: "#0d1117", foreground: "#e6edf3" },
  });
  var fitAddon = new FitAddon.FitAddon();
  var webLinksAddon = new WebLinksAddon.WebLinksAddon();
  term.loadAddon(fitAddon);
  term.loadAddon(webLinksAddon);
  term.open(termContainer);

  // Small delay lets the DOM settle so fitAddon measures correctly
  setTimeout(function () { fitAddon.fit(); }, 50);

  var wsProto = location.protocol === "https:" ? "wss:" : "ws:";
  var ws = new WebSocket(wsProto + "//" + location.host + "/api/instances/" + encodeURIComponent(name) + "/shell");
  ws.binaryType = "arraybuffer";

  ws.onopen = function () {
    // Send initial size once connected
    fitAddon.fit();
    var dims = fitAddon.proposeDimensions();
    if (dims) {
      ws.send(JSON.stringify({ type: "resize", cols: dims.cols, rows: dims.rows }));
    }
  };

  ws.onmessage = function (e) {
    term.write(new Uint8Array(e.data));
  };

  ws.onerror = function () {
    term.write("\r\n\x1b[31m[connection error]\x1b[0m\r\n");
  };

  ws.onclose = function () {
    term.write("\r\n\x1b[33m[connection closed]\x1b[0m\r\n");
  };

  term.onData(function (data) {
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(new TextEncoder().encode(data));
    }
  });

  term.onResize(function (size) {
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: "resize", cols: size.cols, rows: size.rows }));
    }
  });

  termResizeHandler = function () {
    fitAddon.fit();
  };
  window.addEventListener("resize", termResizeHandler);

  activeTerm = term;
  activeFit = fitAddon;
  activeWs = ws;

  term.focus();
}

function closeTerminal() {
  if (activeWs) {
    activeWs.close();
    activeWs = null;
  }
  if (activeTerm) {
    activeTerm.dispose();
    activeTerm = null;
  }
  activeFit = null;
  if (termResizeHandler) {
    window.removeEventListener("resize", termResizeHandler);
    termResizeHandler = null;
  }
  termContainer.innerHTML = "";
  termOverlay.classList.add("hidden");
}

termCloseBtn.addEventListener("click", closeTerminal);
termOverlay.addEventListener("click", function (e) {
  if (e.target === termOverlay) closeTerminal();
});

/* ── Render Instances ────────────────────────────────────── */

var listEl = document.getElementById("instance-list");

function renderInstances(instances) {
  if (!instances || instances.length === 0) {
    listEl.innerHTML =
      '<p class="placeholder">No instances found. Click <strong>+ Create VM</strong> to get started.</p>';
    return;
  }

  listEl.innerHTML = "";

  instances.forEach(function (inst) {
    var card = document.createElement("div");
    card.className = "instance-card";

    var isRunning = (inst.status || "").toLowerCase() === "running";
    var isStopped = (inst.status || "").toLowerCase() === "stopped";

    card.innerHTML =
      '<div class="card-header">' +
      "  <h3>" +
      escapeHtml(inst.name) +
      "</h3>" +
      '  <span class="badge ' +
      statusClass(inst.status) +
      '">' +
      escapeHtml(inst.status || "Unknown") +
      "</span>" +
      "</div>" +
      '<div class="card-meta">' +
      "  <span><span class='meta-label'>Arch</span> " +
      escapeHtml(inst.arch || "—") +
      "</span>" +
      "  <span><span class='meta-label'>CPUs</span> " +
      (inst.cpus != null ? inst.cpus : "—") +
      "</span>" +
      "  <span><span class='meta-label'>Mem</span> " +
      humanBytes(inst.memory) +
      "</span>" +
      "  <span><span class='meta-label'>Disk</span> " +
      humanBytes(inst.disk) +
      "</span>" +
      "</div>" +
      '<div class="card-actions"></div>';

    var actions = card.querySelector(".card-actions");

    if (isRunning) {
      actions.appendChild(
        makeBtn("Stop", "btn btn-red", function () {
          stopInstance(inst.name);
        })
      );
      actions.appendChild(
        makeBtn("Restart", "btn btn-secondary", function () {
          restartInstance(inst.name);
        })
      );
      actions.appendChild(
        makeBtn("Console", "btn btn-blue", function () {
          openConsole(inst.name);
        })
      );
      actions.appendChild(
        makeBtn("Terminal", "btn btn-secondary", function () {
          openTerminal(inst.name);
        })
      );
    } else if (isStopped) {
      actions.appendChild(
        makeBtn("Start", "btn btn-green", function () {
          startInstance(inst.name);
        })
      );
      actions.appendChild(
        makeBtn("Delete", "btn btn-outline-red", function () {
          deleteInstance(inst.name);
        })
      );
    } else {
      // Transitional state — show limited actions
      actions.appendChild(
        makeBtn("Stop", "btn btn-red", function () {
          stopInstance(inst.name);
        })
      );
    }

    listEl.appendChild(card);
  });
}

function makeBtn(label, className, onclick) {
  var b = document.createElement("button");
  b.className = className;
  b.textContent = label;
  b.addEventListener("click", onclick);
  return b;
}

function escapeHtml(str) {
  var d = document.createElement("div");
  d.appendChild(document.createTextNode(str));
  return d.innerHTML;
}

/* ── Refresh Loop ────────────────────────────────────────── */

var refreshTimer = null;

async function refreshInstances() {
  try {
    var instances = await api("GET", "/api/instances");
    renderInstances(instances);
  } catch (err) {
    listEl.innerHTML =
      '<p class="placeholder">Failed to load instances: ' +
      escapeHtml(err.message) +
      "</p>";
  }
}

function startAutoRefresh() {
  refreshInstances();
  refreshTimer = setInterval(refreshInstances, 5000);
}

/* ── Create VM Modal ─────────────────────────────────────── */

var overlay = document.getElementById("modal-overlay");
var selectEl = document.getElementById("template-select");
var createStatus = document.getElementById("create-status");
var btnCreate = document.getElementById("btn-create");
var btnClose = document.getElementById("modal-close");
var btnCancel = document.getElementById("btn-cancel");
var btnSubmit = document.getElementById("btn-submit-create");

function openModal() {
  overlay.classList.remove("hidden");
  createStatus.classList.add("hidden");
  btnSubmit.disabled = false;
  loadTemplates();
}

function closeModal() {
  overlay.classList.add("hidden");
}

async function loadTemplates() {
  selectEl.innerHTML = '<option value="">Loading…</option>';
  try {
    var templates = await api("GET", "/api/templates");
    if (!templates || templates.length === 0) {
      selectEl.innerHTML =
        '<option value="">No templates available</option>';
      return;
    }
    selectEl.innerHTML = "";
    templates.forEach(function (t) {
      var opt = document.createElement("option");
      opt.value = t.name;
      opt.textContent = t.name;
      selectEl.appendChild(opt);
    });
  } catch (err) {
    selectEl.innerHTML = '<option value="">Failed to load templates</option>';
    showToast("Templates: " + err.message, "error");
  }
}

async function submitCreate() {
  var template = selectEl.value;
  if (!template) {
    showToast("Select a template first", "error");
    return;
  }

  btnSubmit.disabled = true;
  createStatus.classList.remove("hidden");

  try {
    var result = await api("POST", "/api/instances/create", {
      template: template,
    });
    showToast(
      'Instance "' + (result.name || template) + '" created!',
      "success"
    );
    closeModal();
    refreshInstances();
    pollUntilRunning(result.name || template);
  } catch (err) {
    showToast("Create failed: " + err.message, "error");
    createStatus.classList.add("hidden");
    btnSubmit.disabled = false;
  }
}

async function pollUntilRunning(name) {
  var attempts = 0;
  var maxAttempts = 60; // 5 minutes at 5s intervals
  var poll = setInterval(async function () {
    attempts++;
    try {
      var instances = await api("GET", "/api/instances");
      var inst = instances.find(function (i) {
        return i.name === name;
      });
      if (inst && inst.status.toLowerCase() === "running") {
        clearInterval(poll);
        showToast('"' + name + '" is now running', "success");
        refreshInstances();
      } else if (attempts >= maxAttempts) {
        clearInterval(poll);
        showToast('"' + name + '" creation timed out — check manually', "error");
      }
    } catch (_) {
      // Silently retry
    }
  }, 5000);
}

/* ── bootc Feature Detection ─────────────────────────────── */

async function detectBootc() {
  try {
    var info = await api("GET", "/api/info");
    if (info && info.bootc_enabled) {
      document.getElementById("btn-bootc").classList.remove("hidden");
      document.getElementById("bootc-section").classList.remove("hidden");
      startBootcRefresh();
    }
  } catch (_) {}
}

/* ── bootc Builds ────────────────────────────────────────── */

var bootcRefreshTimer = null;

function startBootcRefresh() {
  refreshBootcBuilds();
  bootcRefreshTimer = setInterval(refreshBootcBuilds, 5000);
}

async function refreshBootcBuilds() {
  try {
    var builds = await api("GET", "/api/bootc/builds");
    renderBootcBuilds(builds || []);
  } catch (_) {}
}

function renderBootcBuilds(builds) {
  var el = document.getElementById("bootc-builds-list");
  if (!builds || builds.length === 0) {
    el.innerHTML = '<p class="placeholder">No bootc builds yet.</p>';
    return;
  }
  el.innerHTML = "";
  // Sort newest first
  builds.slice().reverse().forEach(function (b) {
    var card = document.createElement("div");
    card.className = "instance-card";
    var statusCls = b.status === "complete" ? "badge-running"
                  : b.status === "failed" ? "badge-stopped"
                  : "badge-other";
    card.innerHTML =
      '<div class="card-header">' +
        '<h3>' + escapeHtml(b.vm_name || b.id) + '</h3>' +
        '<span class="badge ' + statusCls + '">' + escapeHtml(b.status) + '</span>' +
      '</div>' +
      '<div class="card-meta">' +
        '<span><span class="meta-label">Image</span> ' + escapeHtml(b.source_image) + '</span>' +
      '</div>' +
      (b.error ? '<div style="font-size:0.8rem;color:var(--red);margin-top:0.25rem;">' + escapeHtml(b.error) + '</div>' : '') +
      '<div class="card-actions">' +
        '<button class="btn btn-secondary" onclick="viewBuildLog(\'' + escapeHtml(b.id) + '\')">View Log</button>' +
      '</div>';
    el.appendChild(card);
  });
}

function viewBuildLog(buildId) {
  var overlay = document.createElement("div");
  overlay.style.cssText = "position:fixed;inset:0;background:rgba(0,0,0,0.8);z-index:300;display:flex;flex-direction:column;padding:1rem;";

  var header = document.createElement("div");
  header.style.cssText = "display:flex;justify-content:space-between;align-items:center;margin-bottom:0.75rem;";
  header.innerHTML = '<span style="color:var(--text);font-weight:600;">Build Log: ' + escapeHtml(buildId) + '</span>';
  var closeBtn = document.createElement("button");
  closeBtn.className = "btn btn-secondary";
  closeBtn.textContent = "Close";
  closeBtn.onclick = function () { document.body.removeChild(overlay); };
  header.appendChild(closeBtn);

  var pre = document.createElement("pre");
  pre.style.cssText = "flex:1;overflow:auto;background:var(--bg);color:var(--text);padding:1rem;border-radius:var(--radius);font-size:0.8rem;line-height:1.5;border:1px solid var(--border);";
  pre.textContent = "Loading log...\n";

  overlay.appendChild(header);
  overlay.appendChild(pre);
  document.body.appendChild(overlay);

  var evtSource = new EventSource("/api/bootc/builds/" + encodeURIComponent(buildId) + "/log");
  evtSource.onmessage = function (e) {
    if (pre.textContent === "Loading log...\n") pre.textContent = "";
    if (e.data.startsWith("[DONE")) {
      evtSource.close();
      refreshBootcBuilds();
      refreshInstances();
      return;
    }
    pre.textContent += e.data + "\n";
    pre.scrollTop = pre.scrollHeight;
  };
  evtSource.onerror = function () { evtSource.close(); };

  var origClose = closeBtn.onclick;
  closeBtn.onclick = function () { evtSource.close(); origClose(); };
}

/* ── bootc Modal ─────────────────────────────────────────── */

var bootcOverlay = document.getElementById("bootc-modal-overlay");
var bootcImageInput = document.getElementById("bootc-image-input");
var bootcNameInput = document.getElementById("bootc-name-input");
var bootcSshCheck = document.getElementById("bootc-ssh-check");
var bootcRdpCheck = document.getElementById("bootc-rdp-check");
var bootcPackagesInput = document.getElementById("bootc-packages-input");
var bootcContainerfileInput = document.getElementById("bootc-containerfile-input");

function openBootcModal() {
  bootcOverlay.classList.remove("hidden");
  bootcImageInput.value = "";
  bootcNameInput.value = "";
  bootcSshCheck.checked = false;
  bootcRdpCheck.checked = false;
  bootcPackagesInput.value = "";
  bootcContainerfileInput.value = "";
  bootcImageInput.focus();
}

function closeBootcModal() {
  bootcOverlay.classList.add("hidden");
}

async function submitBootcBuild() {
  var image = bootcImageInput.value.trim();
  if (!image) {
    showToast("Enter a container image URI", "error");
    return;
  }
  var vmName = bootcNameInput.value.trim();

  // Collect customizations (only include if any are set)
  var extraPackages = bootcPackagesInput.value.trim().split(/\s+/).filter(Boolean);
  var extraContainerfile = bootcContainerfileInput.value.trim();
  var enableSSH = bootcSshCheck.checked;
  var enableRDP = bootcRdpCheck.checked;
  var hasCustomizations = enableSSH || enableRDP || extraPackages.length > 0 || extraContainerfile;

  var body = { image: image, vm_name: vmName };
  if (hasCustomizations) {
    body.customizations = {
      enable_ssh: enableSSH,
      enable_rdp: enableRDP,
      extra_packages: extraPackages,
      extra_containerfile: extraContainerfile,
    };
  }

  closeBootcModal();
  try {
    var result = await api("POST", "/api/bootc/builds", body);
    showToast("Build started: " + result.id, "success");
    refreshBootcBuilds();
    viewBuildLog(result.id);
  } catch (err) {
    showToast("Build failed to start: " + err.message, "error");
  }
}

document.getElementById("btn-bootc").addEventListener("click", openBootcModal);
document.getElementById("bootc-modal-close").addEventListener("click", closeBootcModal);
document.getElementById("bootc-btn-cancel").addEventListener("click", closeBootcModal);
document.getElementById("bootc-btn-submit").addEventListener("click", submitBootcBuild);
bootcOverlay.addEventListener("click", function (e) { if (e.target === bootcOverlay) closeBootcModal(); });

/* ── Event Listeners ─────────────────────────────────────── */
btnCreate.addEventListener("click", openModal);
btnClose.addEventListener("click", closeModal);
btnCancel.addEventListener("click", closeModal);
btnSubmit.addEventListener("click", submitCreate);

overlay.addEventListener("click", function (e) {
  if (e.target === overlay) closeModal();
});

document.addEventListener("keydown", function (e) {
  if (e.key === "Escape" && !overlay.classList.contains("hidden")) {
    closeModal();
  }
  if (e.key === "Escape" && !bootcOverlay.classList.contains("hidden")) {
    closeBootcModal();
  }
  if (e.key === "Escape" && !termOverlay.classList.contains("hidden")) {
    closeTerminal();
  }
});

/* ── Init ────────────────────────────────────────────────── */

startAutoRefresh();
detectBootc();
