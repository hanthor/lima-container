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
});

/* ── Init ────────────────────────────────────────────────── */

startAutoRefresh();
