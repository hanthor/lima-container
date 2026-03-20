/* ── Toast Notifications ─────────────────────────────────── */

var toastArea = document.getElementById("toast-area");

function showToast(message, level) {
  level = level || "info";
  var el = document.createElement("div");
  el.className = "toast toast-" + level;
  el.textContent = message;
  toastArea.appendChild(el);
  setTimeout(function () { el.remove(); }, 5000);
}

/* ── htmx Event Listeners ───────────────────────────────── */

document.body.addEventListener("showToast", function (e) {
  showToast(e.detail.message, e.detail.level);
});

/* ── VNC Console ─────────────────────────────────────────── */

async function openConsole(name) {
  try {
    var res = await fetch("/api/instances/" + encodeURIComponent(name) + "/vnc");
    var json = await res.json();
    if (json.data && json.data.url) {
      window.open(json.data.url, "_blank");
    } else {
      showToast("No VNC URL returned", "error");
    }
  } catch (err) {
    showToast("Console error: " + err.message, "error");
  }
}

/* ── RDP ─────────────────────────────────────────────────── */

function openRDP(name) {
  window.open("/rdp/rdp.html?instance=" + encodeURIComponent(name), "rdp-" + name);
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

/* ── Build Log Viewer ────────────────────────────────────── */

function viewBuildLog(buildId) {
  var overlay = document.createElement("div");
  overlay.style.cssText = "position:fixed;inset:0;background:rgba(0,0,0,0.8);z-index:300;display:flex;flex-direction:column;padding:1rem;";

  var header = document.createElement("div");
  header.style.cssText = "display:flex;justify-content:space-between;align-items:center;margin-bottom:0.75rem;";
  header.innerHTML = '<span style="color:var(--text);font-weight:600;">Build Log</span>';
  var closeBtn = document.createElement("button");
  closeBtn.className = "btn btn-secondary";
  closeBtn.textContent = "Close";
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
      return;
    }
    pre.textContent += e.data + "\n";
    pre.scrollTop = pre.scrollHeight;
  };
  evtSource.onerror = function () { evtSource.close(); };

  closeBtn.onclick = function () { evtSource.close(); document.body.removeChild(overlay); };
}

/* ── bootc Form Helper ───────────────────────────────────── */

async function submitBootcForm(form) {
  var image = form.querySelector("#bootc-image-input").value.trim();
  if (!image) { showToast("Enter a container image URI", "error"); return false; }

  var body = {
    image: image,
    vm_name: form.querySelector("#bootc-name-input").value.trim() || undefined
  };

  var ssh = form.querySelector("#bootc-ssh-check").checked;
  var rdp = form.querySelector("#bootc-rdp-check").checked;
  var pkgs = form.querySelector("#bootc-packages-input").value.trim().split(/\s+/).filter(Boolean);
  var cf = form.querySelector("#bootc-containerfile-input").value.trim();

  if (ssh || rdp || pkgs.length || cf) {
    body.customizations = {
      enable_ssh: ssh,
      enable_rdp: rdp,
      extra_packages: pkgs,
      extra_containerfile: cf
    };
  }

  document.getElementById("bootc-modal-overlay").classList.add("hidden");
  try {
    var res = await fetch("/api/bootc/builds", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
    var json = await res.json();
    if (!res.ok) throw new Error(json.error || "Build failed");
    showToast("Build started: " + json.data.id, "success");
    viewBuildLog(json.data.id);
  } catch (err) {
    showToast("Build failed: " + err.message, "error");
  }
  return false;
}

/* ── Keyboard Shortcuts ──────────────────────────────────── */

document.addEventListener("keydown", function (e) {
  if (e.key !== "Escape") return;
  var termEl = document.getElementById("terminal-modal-overlay");
  if (!termEl.classList.contains("hidden")) { closeTerminal(); return; }
  var bootcEl = document.getElementById("bootc-modal-overlay");
  if (bootcEl && !bootcEl.classList.contains("hidden")) { bootcEl.classList.add("hidden"); return; }
  var createEl = document.getElementById("modal-overlay");
  if (!createEl.classList.contains("hidden")) { createEl.classList.add("hidden"); }
});
