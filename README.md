# lima-container

Run Linux VMs with a graphical desktop, accessible in your browser — no host setup required.

Built on [`ghcr.io/qemus/qemu`](https://github.com/qemus/qemu). Open your browser and connect via the web dashboard or direct noVNC.

## Image variants

| Image | Description |
|-------|-------------|
| `ghcr.io/<org>/lima:latest` | **Plain** — single VM via env var, noVNC only |
| `ghcr.io/<org>/lima-web:latest` | **Web** — multi-VM dashboard (recommended) |
| `ghcr.io/<org>/lima-bootc:latest` | **Bootc** — web dashboard + build VMs from bootc image URIs |

## Quick start

```bash
podman run -d \
  --name lima \
  --device /dev/kvm \
  --device /dev/net/tun \
  --cap-add NET_ADMIN \
  -p 8006:8006 \
  ghcr.io/<your-org>/lima-web:latest
```

Open [http://localhost:8006](http://localhost:8006) — the web dashboard lets you manage VMs, create new ones, and open VNC consoles.

No KVM? Drop `--device /dev/kvm` — falls back to TCG software emulation (~10x slower but functional).

## Bootc image builder

The `lima-bootc` image builds Lima VMs from [bootc](https://containers.github.io/bootc/) container image URIs. Requires `--privileged` and `--device /dev/fuse`.

```bash
docker run -d \
  --name lima-bootc \
  --privileged \
  --device /dev/kvm \
  --device /dev/net/tun \
  --device /dev/fuse \
  --cap-add NET_ADMIN \
  -p 8006:8006 \
  -v lima-bootc-builds:/var/lib/lima-bootc-builds \
  ghcr.io/<your-org>/lima-bootc:latest
```

Click **"Build from bootc image"** in the dashboard, enter an image URI (e.g. `quay.io/fedora/fedora-bootc:42`), optionally enable SSH/RDP or add extra packages, and watch the build log stream live. See [docs/bootc.md](docs/bootc.md) for full details.

## Compose

```yaml
services:
  lima:
    image: ghcr.io/<your-org>/lima-web:latest
    cap_add: [NET_ADMIN]
    devices: [/dev/kvm, /dev/net/tun]
    ports: ["8006:8006"]
    volumes:
      - ./lima-state:/var/lib/lima
    restart: unless-stopped
```

## REST API

The `lima-web` and `lima-bootc` images expose a REST API at `http://localhost:8006/api/`. The machine-readable OpenAPI spec is at `/api/openapi.yaml`.

All responses use a JSON envelope: `{"data": …}` on success, `{"error": "…"}` on failure.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/instances` | List all VMs |
| `GET` | `/api/instances/{name}` | Get VM details |
| `POST` | `/api/instances/create` | Create a VM from a template |
| `POST` | `/api/instances/{name}/start` | Start a VM |
| `POST` | `/api/instances/{name}/stop` | Stop a VM |
| `POST` | `/api/instances/{name}/restart` | Restart a VM |
| `DELETE` | `/api/instances/{name}` | Delete a VM |
| `GET` | `/api/instances/{name}/vnc` | VNC connection info + noVNC URL |
| `GET` | `/api/instances/{name}/rdp/status` | RDP availability + type (`grd`/`xrdp`/`none`) |
| `POST` | `/api/instances/{name}/rdp/enable` | Enable RDP with credentials |
| `GET` | `/api/instances/{name}/shell` | Interactive shell (WebSocket) |
| `POST` | `/api/images/upload` | Upload a `.qcow2`/`.img`/`.raw`/`.yaml` and start a VM |
| `POST` | `/api/images/fetch` | Fetch an image from a URL and start a VM (async) |
| `GET` | `/api/templates` | List built-in VM templates |
| `GET` | `/api/info` | Lima version + feature flags |
| `GET` | `/api/bootc/builds` | List bootc builds (`lima-bootc` only) |
| `POST` | `/api/bootc/builds` | Start a bootc build (async, `lima-bootc` only) |
| `GET` | `/api/bootc/builds/{id}` | Get build status |
| `GET` | `/api/bootc/builds/{id}/log` | Stream build log (Server-Sent Events) |

```bash
BASE=http://localhost:8006

# Check which features are enabled
curl -s $BASE/api/info | jq '{bootc: .data.bootc_enabled}'

# List VMs
curl -s $BASE/api/instances | jq '.data[].name'

# Create a VM from a built-in template, then open its VNC console
curl -s -X POST $BASE/api/instances/create \
  -H 'Content-Type: application/json' \
  -d '{"template": "default", "name": "my-vm"}'
curl -s $BASE/api/instances/my-vm/vnc | jq -r '.data.url'

# Lifecycle
curl -X POST   $BASE/api/instances/my-vm/start
curl -X POST   $BASE/api/instances/my-vm/stop
curl -X DELETE $BASE/api/instances/my-vm

# Upload a local qcow2 and create a VM from it
curl -X POST $BASE/api/images/upload \
  -F file=@./my-disk.qcow2 -F name=my-vm

# Fetch a remote image and create a VM (returns 202 immediately)
curl -X POST $BASE/api/images/fetch \
  -H 'Content-Type: application/json' \
  -d '{"url": "https://example.com/my-disk.qcow2", "name": "my-vm"}'

# Start a bootc build and tail the log
BUILD=$(curl -s -X POST $BASE/api/bootc/builds \
  -H 'Content-Type: application/json' \
  -d '{"image": "quay.io/fedora/fedora-bootc:42", "vm_name": "fedora"}' \
  | jq -r '.data.id')
curl -N $BASE/api/bootc/builds/$BUILD/log
```

See [docs/api.md](docs/api.md) for the full reference including request/response shapes and WebSocket protocol details.

## Documentation

| Topic | Link |
|-------|------|
| Templates, env vars, persistence | [docs/configuration.md](docs/configuration.md) |
| Bootc builder, customization | [docs/bootc.md](docs/bootc.md) |
| Quadlet / systemd service files | [docs/quadlet.md](docs/quadlet.md) |
| Full REST API reference | [docs/api.md](docs/api.md) |
| Internal architecture | [docs/architecture.md](docs/architecture.md) |

## TLS / HTTPS

**TLS is enabled by default.** On first start, a self-signed certificate is auto-generated so noVNC, xterm.js, and RDP all work over a secure context without extra configuration. Your browser will show a certificate warning — accept it to proceed.

To use your own certificate, mount it and set `TLS_CERT`/`TLS_KEY`:

```bash
docker run -d \
  --name lima-web \
  --device /dev/kvm \
  --device /dev/net/tun \
  --cap-add NET_ADMIN \
  -p 8006:8006 \
  -v ./certs:/certs:ro \
  -e TLS_CERT=/certs/cert.pem \
  -e TLS_KEY=/certs/key.pem \
  ghcr.io/<your-org>/lima-web:latest
```

To **disable TLS** (plain HTTP), set `TLS=off`:

```bash
docker run -d -e TLS=off ...
```

**Let's Encrypt / reverse proxy**: If you already have a TLS-terminating reverse proxy (Caddy, Traefik, nginx), set `TLS=off` and let the proxy handle HTTPS.

**Tailscale** (zero-config alternative): Run the container on a Tailscale node and access it via [Tailscale HTTPS](https://tailscale.com/kb/1153/enabling-https) or MagicDNS — no certificate management needed.

When TLS is enabled, WebSocket connections (noVNC, xterm.js, RDP) automatically upgrade to `wss://`.

## Caveats

- Designed for development/experimentation, not production virtualization.
- `--cap-add NET_ADMIN` and `--device /dev/net/tun` are required for Lima networking.
- Rootful Podman (`sudo podman`) is recommended over rootless — rootless has issues with `/dev/net/tun` and SELinux volume mounts.
- `lima-bootc` requires `--privileged` and `--device /dev/fuse` — `bootc-image-builder` and `fuse-overlayfs` both need them.
- Tested on Linux x86_64 with Docker and rootful Podman.
