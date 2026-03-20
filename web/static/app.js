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
    vm_name: form.querySelector("#bootc-name-input").value.trim() || undefined,
    disk_size: form.querySelector("#bootc-disk-size").value.trim() || undefined,
    cpus: parseInt(form.querySelector("#bootc-cpus").value, 10) || undefined,
    memory: form.querySelector("#bootc-memory").value.trim() || undefined
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

/* ── Create VM Tabs + Upload ──────────────────────────────── */

function switchCreateTab(tab) {
  document.querySelectorAll('.modal-tabs .tab').forEach(function(t) { t.classList.remove('active'); });
  event.target.classList.add('active');
  document.getElementById('tab-template').style.display = tab === 'template' ? '' : 'none';
  document.getElementById('tab-image').style.display = tab === 'image' ? '' : 'none';
}

function submitCreateVM() {
  var templateTab = document.getElementById('tab-template');
  var name = document.getElementById('create-vm-name').value.trim();

  if (templateTab.style.display !== 'none') {
    var template = document.getElementById('template-select').value;
    if (!template) { showToast('Select a template', 'error'); return; }
    fetch('/api/instances/create', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({template: template, name: name || undefined})
    }).then(function(r) { return r.json(); }).then(function(json) {
      if (json.error) { showToast(json.error, 'error'); return; }
      showToast('Created ' + (json.data.instance || 'VM'), 'success');
      document.getElementById('modal-overlay').classList.add('hidden');
    }).catch(function(e) { showToast('Create failed: ' + e.message, 'error'); });
    return;
  }

  var fileInput = document.getElementById('image-file');
  var urlInput = document.getElementById('image-url').value.trim();

  if (fileInput.files.length > 0) {
    uploadImage(fileInput.files[0], name);
  } else if (urlInput) {
    fetchImage(urlInput, name);
  } else {
    showToast('Select a file or enter a URL', 'error');
  }
}

function uploadImage(file, name) {
  var validExts = ['.qcow2', '.img', '.raw', '.iso', '.yaml', '.yml'];
  var ext = file.name.substring(file.name.lastIndexOf('.')).toLowerCase();
  if (validExts.indexOf(ext) === -1) {
    showToast('Unsupported format. Use: qcow2, img, raw, yaml', 'error');
    return;
  }

  var form = new FormData();
  form.append('file', file);
  if (name) form.append('name', name);

  var progress = document.getElementById('upload-progress');
  var fill = document.getElementById('progress-fill');
  var text = document.getElementById('progress-text');
  progress.classList.remove('hidden');

  var xhr = new XMLHttpRequest();
  xhr.open('POST', '/api/images/upload');
  xhr.upload.onprogress = function(e) {
    if (e.lengthComputable) {
      var pct = Math.round(e.loaded / e.total * 100);
      fill.style.width = pct + '%';
      text.textContent = 'Uploading… ' + pct + '%';
    }
  };
  xhr.onload = function() {
    progress.classList.add('hidden');
    fill.style.width = '0%';
    try {
      var json = JSON.parse(xhr.responseText);
      if (xhr.status >= 400 || json.error) {
        showToast(json.error || 'Upload failed', 'error');
        return;
      }
      showToast('VM created from ' + file.name, 'success');
      document.getElementById('modal-overlay').classList.add('hidden');
    } catch(e) {
      showToast('Upload failed', 'error');
    }
  };
  xhr.onerror = function() {
    progress.classList.add('hidden');
    showToast('Upload failed: network error', 'error');
  };
  xhr.send(form);
}

function fetchImage(url, name) {
  showToast('Downloading image…', 'info');
  document.getElementById('modal-overlay').classList.add('hidden');
  fetch('/api/images/fetch', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({url: url, name: name || undefined})
  }).then(function(r) { return r.json(); }).then(function(json) {
    if (json.error) { showToast(json.error, 'error'); return; }
    showToast('Download started — VM will appear when ready', 'success');
  }).catch(function(e) { showToast('Fetch failed: ' + e.message, 'error'); });
}

(function() {
  var dz = document.getElementById('drop-zone');
  if (!dz) return;
  var fi = document.getElementById('image-file');

  dz.addEventListener('dragover', function(e) { e.preventDefault(); dz.classList.add('drag-over'); });
  dz.addEventListener('dragleave', function() { dz.classList.remove('drag-over'); });
  dz.addEventListener('drop', function(e) {
    e.preventDefault();
    dz.classList.remove('drag-over');
    if (e.dataTransfer.files.length > 0) {
      fi.files = e.dataTransfer.files;
      dz.querySelector('.drop-zone-text p').textContent = e.dataTransfer.files[0].name;
    }
  });

  fi.addEventListener('change', function() {
    if (fi.files.length > 0) {
      dz.querySelector('.drop-zone-text p').textContent = fi.files[0].name;
    }
  });
})();

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
