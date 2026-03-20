# API Reference

The `lima-web` Go server exposes a REST API on port 8080, proxied by nginx at the container's public port (default `8006`). All API routes are under `/api/`.

Available in the `lima-web` and `lima-bootc` image variants (not the plain `lima` image).

## Base URL

```
http://<host>:8006
```

`GET /` redirects to `/dashboard/`. The dashboard SPA polls the API directly.

The machine-readable OpenAPI 3.1 schema is served at:

```
GET /api/openapi.yaml
```

## Response format

All API responses use a consistent JSON envelope:

**Success:**
```json
{ "data": <payload> }
```

**Error:**
```json
{ "error": "human-readable message" }
```

HTTP status codes: `200 OK`, `202 Accepted` (async), `400 Bad Request`, `404 Not Found`, `503 Service Unavailable`, `500 Internal Server Error`.

---

## Quick reference

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/instances` | List all VMs |
| `GET` | `/api/instances/{name}` | Get VM details |
| `POST` | `/api/instances/create` | Create and start a VM |
| `POST` | `/api/instances/{name}/start` | Start a VM |
| `POST` | `/api/instances/{name}/stop` | Stop a VM |
| `POST` | `/api/instances/{name}/restart` | Restart a VM |
| `DELETE` | `/api/instances/{name}` | Delete a VM |
| `GET` | `/api/instances/{name}/vnc` | VNC connection info |
| `GET` | `/api/instances/{name}/shell` | Shell terminal (WebSocket) |
| `GET` | `/api/templates` | List VM templates |
| `GET` | `/api/info` | System info + feature flags |
| `GET` | `/api/bootc/builds` | List bootc builds |
| `POST` | `/api/bootc/builds` | Start a bootc build (async) |
| `GET` | `/api/bootc/builds/{id}` | Get build status |
| `GET` | `/api/bootc/builds/{id}/log` | Stream build log (SSE) |

---

## Instances

### List instances

```
GET /api/instances
```

Returns all Lima VM instances.

**Response:**
```json
{
  "data": [
    {
      "name": "my-vm",
      "status": "Running",
      "dir": "/var/lib/lima/my-vm",
      "arch": "x86_64",
      "cpus": 4,
      "memory": 4294967296,
      "disk": 107374182400
    }
  ]
}
```

`status` values: `Running`, `Stopped`, `Broken`, `Unknown`.

---

### Get instance

```
GET /api/instances/{name}
```

Returns details for a single instance. Same shape as one element from `GET /api/instances`.

**Errors:** `404` if not found.

---

### Create instance

```
POST /api/instances/create
```

Create and start a new VM from a template.

**Request body:**
```json
{
  "template": "default",
  "name": "my-vm"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `template` | string | No | Template name (without `.yaml`). Defaults to `"default"`. |
| `name` | string | No | Instance name. Defaults to the template name. |

**Response:**
```json
{
  "data": {
    "status": "created",
    "instance": "my-vm",
    "vnc_warning": "..."
  }
}
```

`vnc_warning` is only present if the VNC bridge failed to start (non-fatal; the VM still runs).

**Errors:** `400` if template file not found or body is invalid.

---

### Start instance

```
POST /api/instances/{name}/start
```

Start a stopped instance and its VNC bridge.

**Response:**
```json
{ "data": { "status": "started" } }
```

---

### Stop instance

```
POST /api/instances/{name}/stop
```

Stop a running instance and tear down its VNC bridge.

**Response:**
```json
{ "data": { "status": "stopped" } }
```

---

### Restart instance

```
POST /api/instances/{name}/restart
```

Stop then start the instance and its VNC bridge.

**Response:**
```json
{ "data": { "status": "restarted" } }
```

---

### Delete instance

```
DELETE /api/instances/{name}
```

Force-delete an instance and stop its VNC bridge.

**Response:**
```json
{ "data": { "status": "deleted" } }
```

---

## VNC console

### Get VNC info

```
GET /api/instances/{name}/vnc
```

Returns VNC connection details for a running instance.

**Response:**
```json
{
  "data": {
    "port": 5710,
    "password": "secret",
    "url": "/vnc/vnc.html?autoconnect=1&resize=remote&path=websockify/my-vm&password=secret"
  }
}
```

`url` is ready to open in a browser tab — it loads noVNC connected to this instance's display.

The underlying WebSocket endpoint is `ws://<host>:8006/websockify/{name}`, proxied by nginx to a per-instance `websocketd` bridge on ports 5710–5799.

**Errors:** `404` if no VNC bridge is running (VM may be stopped or VNC not yet ready).

---

## Shell terminal (WebSocket)

```
GET /api/instances/{name}/shell
```

Upgrades to a WebSocket connection that proxies an interactive SSH PTY session into the instance.

**Protocol:** Binary WebSocket frames.

| Direction | Frame type | Content |
|-----------|------------|---------|
| Server → Client | Binary | Raw terminal output bytes |
| Client → Server | Binary | Keyboard input / paste |
| Client → Server | Text (JSON) | Terminal resize event |

**Resize event (client → server, text frame):**
```json
{ "type": "resize", "cols": 220, "rows": 50 }
```

**Errors:**
- `404` — instance not found
- `503` — instance not in `Running` state

**xterm.js example:**
```js
const ws = new WebSocket(`ws://${location.host}/api/instances/my-vm/shell`);
ws.binaryType = 'arraybuffer';
ws.onmessage = e => term.write(new Uint8Array(e.data));
term.onData(data => ws.send(data));
term.onResize(({ cols, rows }) =>
  ws.send(JSON.stringify({ type: 'resize', cols, rows }))
);
```

---

## Templates

### List templates

```
GET /api/templates
```

Lists available VM templates from `/opt/lima/templates/`.

**Response:**
```json
{
  "data": [
    {
      "name": "default",
      "filename": "default.yaml",
      "path": "/opt/lima/templates/default.yaml"
    }
  ]
}
```

Pass `name` to `POST /api/instances/create` to use a template.

---

## System info

```
GET /api/info
```

Returns Lima system information and feature flags for the current image variant.

**Response:**
```json
{
  "data": {
    "lima": { "...": "raw limactl info output" },
    "bootc_enabled": true
  }
}
```

`bootc_enabled` is `true` only in the `lima-bootc` image. Use this to conditionally show bootc UI elements.

---

## Bootc builds (`lima-bootc` image only)

These endpoints are only available when `bootc_enabled` is `true`. They manage the pipeline that builds a raw disk image from a bootc container image reference and launches it as a Lima VM.

See [bootc.md](bootc.md) for a full explanation of the pipeline.

### List builds

```
GET /api/bootc/builds
```

**Response:**
```json
{
  "data": [
    {
      "id": "build-1774021161021",
      "source_image": "quay.io/fedora/fedora-bootc:42",
      "vm_name": "fedora-bootc",
      "customizations": {
        "enable_ssh": false,
        "enable_rdp": false,
        "extra_packages": [],
        "extra_containerfile": ""
      },
      "status": "complete",
      "started_at": "2026-03-20T15:39:21Z",
      "finished_at": "2026-03-20T15:52:10Z",
      "error": null,
      "output_path": "/var/lib/lima-bootc-builds/build-1774021161021/disk.raw"
    }
  ]
}
```

`status` values: `pending`, `running`, `complete`, `failed`.

---

### Create build

```
POST /api/bootc/builds
```

Starts an async build. Returns `202 Accepted` immediately.

**Request body:**
```json
{
  "image": "quay.io/fedora/fedora-bootc:42",
  "vm_name": "my-fedora",
  "customizations": {
    "enable_ssh": false,
    "enable_rdp": false,
    "extra_packages": ["vim", "htop"],
    "extra_containerfile": "RUN echo welcome > /etc/motd"
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `image` | string | **Yes** | Source bootc container image URI |
| `vm_name` | string | No | Lima VM name to create on success. Auto-generated if omitted. |
| `customizations.enable_ssh` | bool | No | Inject SSH keys into the image |
| `customizations.enable_rdp` | bool | No | Enable RDP in the image |
| `customizations.extra_packages` | string[] | No | Additional RPM packages to install |
| `customizations.extra_containerfile` | string | No | Extra Containerfile lines appended during derivation |

Omit `customizations` entirely to use the image as-is.

**Response:** `202 Accepted` with the build object (status `"pending"`).

**Errors:** `400` if `image` is missing or body is invalid JSON.

**Build pipeline (async):**
1. If customizations are set, build a derived container image and save as OCI archive
2. Allocate a raw disk file and attach it via `qemu-nbd`
3. Run `bootc install to-disk` in a privileged inner container
4. Disconnect the NBD device
5. Create and start a Lima VM from the disk image

---

### Get build

```
GET /api/bootc/builds/{id}
```

Returns the current state of a build. Same shape as a single element from `GET /api/bootc/builds`.

**Errors:** `404` if build ID not found.

---

### Stream build log

```
GET /api/bootc/builds/{id}/log
```

Streams the build log as [Server-Sent Events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events).

**Response headers:**
```
Content-Type: text/event-stream
Cache-Control: no-cache
```

**SSE stream:**
```
data: [INFO] Pulling quay.io/fedora/fedora-bootc:42

data: [INFO] Running bootc install to-disk

data: [DONE] status=complete
```

Each `data:` line is one log entry. The final `[DONE]` line carries `status=complete` or `status=failed`. For already-completed builds, the full log is streamed then the connection closes.

**JavaScript example:**
```js
const es = new EventSource(`/api/bootc/builds/${id}/log`);
es.onmessage = e => {
  if (e.data.startsWith('[DONE]')) { es.close(); return; }
  appendToLog(e.data);
};
```

---

## nginx-level routes

These are handled by nginx in front of the Go server:

| Route | Target | Description |
|-------|--------|-------------|
| `GET /` | — | Redirect to `/dashboard/` |
| `GET /dashboard/*` | Go server :8080 | Dashboard SPA and static assets |
| `GET /api/*` | Go server :8080 | REST API (proxy timeout: 600 s) |
| `GET /vnc/*` | Static files | noVNC HTML/JS client |
| `WS /websockify/{name}` | websocketd :571x | Per-instance VNC WebSocket bridge |

VNC routes are written to `/etc/nginx/lima-vnc-locations.conf` dynamically when VNC bridges start/stop; nginx is reloaded automatically.

---

## curl examples

```bash
# Check which features are available
curl http://localhost:8006/api/info | jq '{bootc: .data.bootc_enabled}'

# List VMs
curl http://localhost:8006/api/instances | jq '.data[].name'

# Create a VM from a template
curl -X POST http://localhost:8006/api/instances/create \
  -H 'Content-Type: application/json' \
  -d '{"template": "default", "name": "my-vm"}'

# Start / stop / delete
curl -X POST   http://localhost:8006/api/instances/my-vm/start
curl -X POST   http://localhost:8006/api/instances/my-vm/stop
curl -X DELETE http://localhost:8006/api/instances/my-vm

# Open VNC in a browser
curl http://localhost:8006/api/instances/my-vm/vnc | jq -r '.data.url'

# Start a bootc build and tail the log
BUILD=$(curl -s -X POST http://localhost:8006/api/bootc/builds \
  -H 'Content-Type: application/json' \
  -d '{"image": "quay.io/fedora/fedora-bootc:42"}' | jq -r '.data.id')

curl -N http://localhost:8006/api/bootc/builds/$BUILD/log
```
