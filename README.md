# lima-container

Run Linux VMs with a graphical desktop, accessible in your browser — no host setup required.

Built on [`ghcr.io/qemus/qemu`](https://github.com/qemus/qemu). A VM boots automatically when the container starts. Open your browser and connect.

## Quick start

```bash
podman run -d \
  --name lima \
  --device /dev/kvm \
  --cap-add NET_ADMIN \
  -p 8006:8006 \
  ghcr.io/<your-org>/lima:latest
```

Open [http://localhost:8006](http://localhost:8006). The default Ubuntu VM starts automatically.

No KVM? Drop `--device /dev/kvm` — the container falls back to software emulation (~10x slower but fully functional).

Prefer Docker instead? Replace `podman` with `docker` in the same commands.

## Podman Compose

```yaml
services:
  lima:
    image: ghcr.io/<your-org>/lima:latest
    container_name: lima
    cap_add:
      - NET_ADMIN
    devices:
      - /dev/kvm
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
| `/images/myvm.qcow2` | Boot your own disk image (qcow2, img, or raw) |
| `/custom/myvm.yaml` | Use a custom Lima YAML template |

Disk images and custom YAMLs must be mounted into the container.

**Example — custom disk image:**

```bash
podman run -d \
  --name lima \
  --device /dev/kvm \
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
Browser → noVNC (port 8006) → nginx → websocketd → nc → Lima QEMU VNC (port 5900)
```

Data flows from your browser through noVNC over HTTP, proxied through nginx as a websocket, then bridged via `nc` into the Lima VM's QEMU VNC socket.

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
