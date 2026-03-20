# lima-container

Run Linux VMs with a graphical desktop, accessible in your browser — no host setup required.

Built on [`ghcr.io/qemus/qemu`](https://github.com/qemus/qemu). Open your browser and connect via the web dashboard or direct noVNC.

## Image variants

Three images are published from this repo — pick the one that fits your use case:

| Image | Description |
|-------|-------------|
| `ghcr.io/<org>/lima:latest` | **Plain** — minimal image, boots a single VM via env var, noVNC only |
| `ghcr.io/<org>/lima-web:latest` | **Web** — adds the Go web dashboard for multi-VM management (recommended) |
| `ghcr.io/<org>/lima-bootc:latest` | **Bootc** — web dashboard + ability to build Lima VMs from bootc container image URIs |

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

Open [http://localhost:8006](http://localhost:8006) — you'll land on the **web dashboard** where you can manage VMs, create new ones, and open VNC consoles.

No KVM? Drop `--device /dev/kvm` — the container falls back to software emulation (~10x slower but fully functional).

Prefer Docker instead? Replace `podman` with `docker` in the same commands.

## Bootc image builder

The `lima-bootc` image can build Lima VMs from [bootc](https://containers.github.io/bootc/) container image URIs using [`bootc-image-builder`](https://github.com/osbuild/bootc-image-builder). This lets you point at any bootc-compatible OCI image and get a running VM with no manual image preparation.

**Requirements:** the container must run with `--privileged` (bootc-image-builder uses loop devices and osbuild internally).

```bash
podman run -d \
  --name lima-bootc \
  --privileged \
  --device /dev/kvm \
  --device /dev/net/tun \
  --cap-add NET_ADMIN \
  -p 8006:8006 \
  -v lima-bootc-builds:/var/lib/lima-bootc-builds \
  ghcr.io/<your-org>/lima-bootc:latest
```

Once running, the dashboard shows a **"Build from bootc image"** button. Enter a bootc container image URI (e.g. `quay.io/fedora/fedora-bootc:42`) and an optional VM name. The build log streams live in the browser; on completion the new VM appears in the instance list.

### Customizing before build

Expand **"Customizations (optional)"** in the build modal to:
- **Enable SSH** — ensures `sshd` is installed and enabled in the VM
- **Enable RDP** — installs and enables `xrdp` for Remote Desktop access
- **Extra packages** — space-separated list of additional packages to install
- **Custom Containerfile instructions** — arbitrary `RUN`/`COPY`/etc. appended to the generated Containerfile

When any customization is selected, the backend:
1. Generates a derived `Containerfile` (`FROM <your-image>` + customization layers)
2. Builds it locally with `podman build` into a temporary image
3. Passes the derived image to `bootc-image-builder` instead of the original
4. Cleans up the temporary image after the build

**REST API for bootc builds:**

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/bootc/builds` | Start a build (`{image, vm_name, customizations}`) |
| `GET` | `/api/bootc/builds` | List all builds with status |
| `GET` | `/api/bootc/builds/:id` | Get build status |
| `GET` | `/api/bootc/builds/:id/log` | Stream build log (SSE) |
| `DELETE` | `/api/bootc/builds/:id` | Cancel / clean up a build |

**`customizations` object (all fields optional):**
```json
{
  "enable_ssh": true,
  "enable_rdp": false,
  "extra_packages": ["vim", "curl"],
  "extra_containerfile": "RUN echo 'welcome' > /etc/motd"
}
```

Built qcow2 images are stored at `/var/lib/lima-bootc-builds/<id>/qcow2/disk.qcow2` inside the container — mount a volume there to persist builds across container restarts.

## Quadlet (systemd)

All three images ship ready-to-use [Podman Quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html) unit files in the `quadlet/` directory. Copy (or symlink) the relevant file to enable the container as a systemd service.

**User service** (runs as your login user, no root required):

```bash
# lima-web (recommended)
cp quadlet/lima-web.container ~/.config/containers/systemd/
# Edit the file to set YOUR_ORG, then:
systemctl --user daemon-reload
systemctl --user start lima-web
systemctl --user enable lima-web   # start on login
```

**System service** (runs at boot, managed by root):

```bash
sudo cp quadlet/lima-bootc.container /etc/containers/systemd/
# Edit the file to set YOUR_ORG, then:
sudo systemctl daemon-reload
sudo systemctl start lima-bootc
sudo systemctl enable lima-bootc
```

**Managing the service:**

```bash
systemctl --user status lima-web
systemctl --user stop lima-web
journalctl --user -u lima-web -f   # follow logs
```

> **Note:** `lima-bootc.container` uses `PodmanArgs=--privileged` because `bootc-image-builder` requires loop devices and osbuild. The other two images do not need elevated privileges beyond `NET_ADMIN` + `/dev/kvm`.

## Podman Compose

```yaml
services:
  lima:
    image: ghcr.io/<your-org>/lima-web:latest
    container_name: lima
    cap_add:
      - NET_ADMIN
    devices:
      - /dev/kvm
      - /dev/net/tun
    environment:
      LIMA_TEMPLATE: "default"   # template name, path to .yaml, or path to .qcow2/.img/.raw
      LIMA_ACCEL_MODE: "auto"    # auto | kvm | tcg
    ports:
      - "8006:8006"
    volumes:
      - ./workspace:/workspace
      - ./lima-state:/var/lib/lima
    restart: unless-stopped
```

## Choosing a VM

Set `LIMA_TEMPLATE` to select what boots:

| `LIMA_TEMPLATE` | Description |
|-----------------|-------------|
| `default` | Ubuntu Noble — lightweight, fast boot (default) |
| `k8s` | Ubuntu Noble with k3s pre-installed |
| `centos-stream-10-gnome` | CentOS Stream 10 with GNOME desktop + Firefox |
| `/images/myvm.qcow2` | Boot your own disk image (qcow2, img, or raw) |
| `/custom/myvm.yaml` | Use a custom Lima YAML template |

Disk images and custom YAMLs must be mounted into the container.

**Example — custom disk image:**

```bash
podman run -d \
  --name lima \
  --device /dev/kvm \
  --device /dev/net/tun \
  --cap-add NET_ADMIN \
  -p 8006:8006 \
  -e LIMA_TEMPLATE=/images/myvm.qcow2 \
  -v $PWD/images:/images \
  ghcr.io/<your-org>/lima:latest
```

**Example — k8s template:**

```bash
podman run -d \
  --name lima \
  --device /dev/kvm \
  --device /dev/net/tun \
  --cap-add NET_ADMIN \
  -p 8006:8006 \
  -e LIMA_TEMPLATE=k8s \
  ghcr.io/<your-org>/lima:latest
```

To list bundled templates inside the container:

```bash
podman exec lima ls /opt/lima/templates/
```

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `LIMA_TEMPLATE` | `default` | VM to boot: template name, path to YAML, or path to disk image |
| `LIMA_ACCEL_MODE` | `auto` | Acceleration: `auto` (KVM if available, else TCG), `kvm`, or `tcg` |
| `AUTO_START_LIMA` | `Y` | Set to `N` to disable automatic VM boot on container start |

<details>
<summary>Advanced: manual VM management, persistent state, port access, architecture</summary>

### Manual VM management

Disable auto-start with `AUTO_START_LIMA=N`, then manage VMs from the host:

```bash
# Start a VM
podman exec lima lima-up /opt/lima/templates/default.yaml

# List instances
podman exec lima lima-as-user limactl list

# Stop an instance
podman exec lima lima-as-user limactl stop default

# Delete an instance
podman exec lima lima-as-user limactl delete default
```

### Persistent VM state

Mount `/var/lib/lima` to preserve VM disk state across container restarts:

```yaml
volumes:
  - ./lima-state:/var/lib/lima
```

If Lima is already installed on the host, you can mount `~/.lima` directly to share instances with the host:

```bash
# Docker
docker run -d ... -v ~/.lima:/var/lib/lima ghcr.io/<your-org>/lima:latest

# Rootful Podman (recommended — avoids SELinux and device permission issues)
sudo podman run -d ... --security-opt label=disable -v ~/.lima:/var/lib/lima ghcr.io/<your-org>/lima:latest
```

The container's `lima` user runs as UID 1000. Ensure the mounted directory is owned by UID 1000 to avoid permission errors.

### Shared workspace

Mount a host directory to `/workspace` — it is shared into the VM automatically:

```yaml
volumes:
  - ./workspace:/workspace
```

### Direct VNC access

Lima's QEMU VNC is also available on port 5900 inside the container. To expose it:

```yaml
ports:
  - "8006:8006"
  - "5900:5900"
```

### KVM acceleration

`--device /dev/kvm` enables hardware virtualization (~10x faster). Without it, the container uses QEMU TCG software emulation. Force a specific mode with `LIMA_ACCEL_MODE=kvm` or `LIMA_ACCEL_MODE=tcg`.

### Architecture

```
Browser
  ├── /              → redirect to /dashboard/
  ├── /dashboard/    → nginx → lima-web Go server (VM management UI)
  ├── /api/*         → nginx → lima-web Go server (REST API)
  └── /vnc/vnc.html  → nginx → noVNC client
       └── /websockify/<instance> → nginx → websocketd → nc → QEMU VNC
```

The Go server (`lima-web`) wraps `limactl` as a REST API and manages per-instance websocketd processes for VNC bridging. nginx is the front door — proxying API calls, serving noVNC static files, and routing websocket connections.

### REST API

All endpoints are available at `/api/*`:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/instances` | List all VMs |
| `GET` | `/api/instances/:name` | Get VM details |
| `POST` | `/api/instances/:name/start` | Start a stopped VM |
| `POST` | `/api/instances/:name/stop` | Stop a running VM |
| `POST` | `/api/instances/:name/restart` | Restart a VM |
| `DELETE` | `/api/instances/:name` | Delete a VM |
| `GET` | `/api/instances/:name/vnc` | Get VNC connection info (port, password, URL) |
| `POST` | `/api/instances/create` | Create a new VM from a template |
| `GET` | `/api/templates` | List available templates |
| `GET` | `/api/info` | Lima host diagnostics (`bootc_enabled` field indicates bootc support) |

**Create a VM:**
```bash
curl -X POST http://localhost:8006/api/instances/create \
  -H 'Content-Type: application/json' \
  -d '{"template": "default"}'
```

**Get VNC console URL:**
```bash
curl http://localhost:8006/api/instances/default/vnc
# → {"data": {"port": 5710, "password": "...", "url": "/vnc/vnc.html?autoconnect=1&..."}}
```

### Custom Lima templates

Create a YAML file following the [Lima template format](https://lima-vm.io/docs/config/). The minimum for VNC use:

```yaml
images:
  - location: "https://..."
    arch: "x86_64"
cpus: 2
memory: "4GiB"
video:
  display: "vnc"
containerd:
  system: false
  user: false
```

Mount it into the container and set `LIMA_TEMPLATE=/custom/myvm.yaml`.

</details>

## Caveats

- Designed for development and experimentation; not a production virtualization platform.
- Nested virtualization performance depends on host kernel and container runtime.
- Tested on Linux with Podman (primary) and Docker. Rootless mode requires cgroup v2.
- `--cap-add NET_ADMIN` is required for Lima's networking stack.
- `--device /dev/net/tun` is required for Lima's networking — Lima will start but VMs will lose network access without it.
- Rootful Podman (`sudo podman`) is recommended over rootless. Rootless Podman has issues passing `/dev/net/tun` and requires `--security-opt label=disable` when mounting `~/.lima` due to SELinux.
- The `lima-bootc` image requires `--privileged` — `bootc-image-builder` uses loop devices and osbuild, which cannot run in an unprivileged container.
