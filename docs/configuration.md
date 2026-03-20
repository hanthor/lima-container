# Configuration

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `LIMA_TEMPLATE` | `default` | VM to boot: template name, path to YAML, or path to disk image |
| `LIMA_ACCEL_MODE` | `auto` | Acceleration: `auto` (KVM if available, else TCG), `kvm`, or `tcg` |
| `AUTO_START_LIMA` | `Y` | Set to `N` to disable automatic VM boot on container start |

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

```bash
podman run -d \
  --name lima \
  --device /dev/kvm --device /dev/net/tun --cap-add NET_ADMIN \
  -p 8006:8006 \
  -e LIMA_TEMPLATE=/images/myvm.qcow2 \
  -v $PWD/images:/images \
  ghcr.io/<your-org>/lima-web:latest
```

To list bundled templates:

```bash
podman exec lima ls /opt/lima/templates/
```

## Custom Lima templates

Create a YAML file following the [Lima template format](https://lima-vm.io/docs/config/). Minimum for VNC use:

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

## Persistent VM state

Mount `/var/lib/lima` to preserve VM state across container restarts:

```yaml
volumes:
  - ./lima-state:/var/lib/lima
```

If Lima is installed on the host, mount `~/.lima` directly to share instances:

```bash
# Docker
docker run -d ... -v ~/.lima:/var/lib/lima ghcr.io/<your-org>/lima-web:latest

# Rootful Podman (recommended — avoids SELinux and device permission issues)
sudo podman run -d ... --security-opt label=disable -v ~/.lima:/var/lib/lima ghcr.io/<your-org>/lima-web:latest
```

The container's `lima` user runs as UID 1000. Ensure the mounted directory is owned by UID 1000.

## Shared workspace

Mount a host directory to `/workspace` — it is shared into the VM automatically:

```yaml
volumes:
  - ./workspace:/workspace
```

## Manual VM management

Disable auto-start with `AUTO_START_LIMA=N`, then manage VMs from the host:

```bash
podman exec lima lima-up /opt/lima/templates/default.yaml
podman exec lima lima-as-user limactl list
podman exec lima lima-as-user limactl stop default
podman exec lima lima-as-user limactl delete default
```

## KVM acceleration

`--device /dev/kvm` enables hardware virtualization (~10x faster). Without it, QEMU TCG software emulation is used. Force a mode with `LIMA_ACCEL_MODE=kvm` or `LIMA_ACCEL_MODE=tcg`.

## Direct VNC access

QEMU VNC is also available on port 5900 inside the container:

```yaml
ports:
  - "8006:8006"
  - "5900:5900"
```
