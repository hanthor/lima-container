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

## Documentation

| Topic | Link |
|-------|------|
| Templates, env vars, persistence | [docs/configuration.md](docs/configuration.md) |
| Bootc builder, customization, REST API | [docs/bootc.md](docs/bootc.md) |
| Quadlet / systemd service files | [docs/quadlet.md](docs/quadlet.md) |
| Full REST API reference | [docs/api.md](docs/api.md) |
| Internal architecture | [docs/architecture.md](docs/architecture.md) |

## Caveats

- Designed for development/experimentation, not production virtualization.
- `--cap-add NET_ADMIN` and `--device /dev/net/tun` are required for Lima networking.
- Rootful Podman (`sudo podman`) is recommended over rootless — rootless has issues with `/dev/net/tun` and SELinux volume mounts.
- `lima-bootc` requires `--privileged` and `--device /dev/fuse` — `bootc-image-builder` and `fuse-overlayfs` both need them.
- Tested on Linux x86_64 with Docker and rootful Podman.
