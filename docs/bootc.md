# Bootc image builder

The `lima-bootc` image builds Lima VMs from [bootc](https://containers.github.io/bootc/) container image URIs using `bootc install to-disk` inside a nested container.

## Running

```bash
# Docker
docker run -d \
  --name lima-bootc \
  --privileged \
  --pid=host \
  --cgroupns=host \
  --device /dev/kvm \
  --device /dev/fuse \
  -p 8006:8006 \
  -v lima-bootc-builds:/var/lib/lima-bootc-builds \
  ghcr.io/<your-org>/lima-bootc:latest

# Rootful Podman
sudo podman run -d \
  --name lima-bootc \
  --privileged \
  --pid=host \
  --cgroupns=host \
  --device /dev/kvm \
  --device /dev/fuse \
  -p 8006:8006 \
  -v lima-bootc-builds:/var/lib/lima-bootc-builds \
  ghcr.io/<your-org>/lima-bootc:latest
```

**`--privileged`** — required for loop/NBD devices and KVM.  
**`--pid=host`** — required by `bootc install` to verify it is not overwriting a live OS.  
**`--cgroupns=host`** — required for inner podman (customisation builds).  
**`--device /dev/fuse`** — required for `fuse-overlayfs` nested container storage.

> **Volume permissions**: The `/var/lib/lima` directory must be owned by UID 1000 (the `lima` user).
> The entrypoint auto-corrects this, but if you pre-create the host directory as root you may need:
> `sudo chown 1000:1000 /path/to/lima-data`

## Building a VM from the UI

1. Open [http://localhost:8006](http://localhost:8006)
2. Click **"Build from bootc image"**
3. Enter a bootc container image URI, e.g. `quay.io/fedora/fedora-bootc:42`
4. Optionally expand **Customizations** (see below)
5. Click **Start Build** — the log streams live in the browser
6. On completion the new VM appears in the instance list

> **Note**: bootc VMs do not run cloud-init, so Lima's SSH provisioning step will
> time out after ~3 minutes. This is expected — the VM is still fully running and
> accessible via the VNC console in the dashboard.

## Customizing before build

Before calling `bootc install`, the backend can apply customizations by generating a derived `Containerfile` and building a local image:

1. Generates `FROM <your-image>` + customization layers
2. Builds it with `podman build` into a temporary local image
3. Passes the derived image to `bootc install to-disk`
4. Cleans up the temporary image after the build completes

**UI options** (expand "Customizations (optional)" in the build modal):

| Option | Effect |
|--------|--------|
| Enable SSH | Ensures `sshd` is installed and enabled (`systemctl enable sshd`) |
| Enable RDP | Installs `xrdp` and enables it for Remote Desktop access |
| Extra packages | Space-separated list of dnf/apt packages to install |
| Custom Containerfile | Arbitrary instructions appended verbatim to the generated Containerfile |

## REST API

See [api.md](api.md#bootc-builds-lima-bootc-image-only) for the full bootc build API reference, including request/response shapes and SSE log streaming.

Quick reference:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/bootc/builds` | Start a build |
| `GET` | `/api/bootc/builds` | List all builds with status |
| `GET` | `/api/bootc/builds/:id` | Get build status |
| `GET` | `/api/bootc/builds/:id/log` | Stream build log (SSE) |

## Storage

Built qcow2 images are stored at `/var/lib/lima-bootc-builds/<id>/disk.qcow2`. Mount a named volume to persist them across container restarts:

```bash
-v lima-bootc-builds:/var/lib/lima-bootc-builds
```

## How it works internally

```
User submits image URI
       │
       ▼
[Optional] Generate Containerfile from customizations
       │   podman build → localhost/derived:<id>
       ▼
truncate -s 20G disk.raw
qemu-nbd --connect=/dev/nbd0 --format=raw disk.raw
       │
       ▼
podman run --privileged --pid=host <bootc-image> \
  bootc install to-disk \
    --source-imgref docker://<image>   # or oci-archive: for derived images
    --filesystem xfs \
    /dev/nbd0
       │
       ▼
qemu-nbd --disconnect /dev/nbd0
qemu-img convert -f raw -O qcow2 disk.raw disk.qcow2
       │
       ▼
limactl start --timeout=3m disk.qcow2
       │
       ▼
VM appears in dashboard (VNC console available immediately)
```

`qemu-nbd` is used instead of `losetup` because it correctly exposes partition
devices (`/dev/nbd0p1`, `/dev/nbd0p2`, …) to the kernel after `bootc` writes the
partition table — `losetup`'s `BLKRRPART` ioctl fails inside nested containers.


## Running

```bash
# Docker
docker run -d \
  --name lima-bootc \
  --privileged \
  --pid=host \
  --cgroupns=host \
  --device /dev/kvm \
  --device /dev/net/tun \
  --device /dev/fuse \
  --cap-add NET_ADMIN \
  -p 8006:8006 \
  -v lima-bootc-builds:/var/lib/lima-bootc-builds \
  -e AUTO_START_LIMA=N \
  ghcr.io/<your-org>/lima-bootc:latest

# Rootful Podman
sudo podman run -d \
  --name lima-bootc \
  --privileged \
  --pid=host \
  --cgroupns=host \
  --device /dev/kvm \
  --device /dev/net/tun \
  --device /dev/fuse \
  --cap-add NET_ADMIN \
  -p 8006:8006 \
  -v lima-bootc-builds:/var/lib/lima-bootc-builds \
  -e AUTO_START_LIMA=N \
  ghcr.io/<your-org>/lima-bootc:latest
```

**`--privileged`** — required for loop devices and KVM.  
**`--pid=host`** — required by `bootc install` to verify it is not overwriting a live OS.  
**`--cgroupns=host`** — required for inner podman (customisation builds).  
**`--device /dev/fuse`** — required for `fuse-overlayfs` nested container storage.

## Building a VM from the UI

1. Open [http://localhost:8006](http://localhost:8006)
2. Click **"Build from bootc image"**
3. Enter a bootc container image URI, e.g. `quay.io/fedora/fedora-bootc:42`
4. Optionally expand **Customizations** (see below)
5. Click **Start Build** — the log streams live in the browser
6. On completion the new VM appears in the instance list

## Customizing before build

Before calling `bootc-image-builder`, the backend can apply customizations by generating a derived `Containerfile` and building a local image:

1. Generates `FROM <your-image>` + customization layers
2. Builds it with `podman build` into a temporary local image
3. Passes the derived image to `bootc-image-builder`
4. Cleans up the temporary image after the build completes

**UI options** (expand "Customizations (optional)" in the build modal):

| Option | Effect |
|--------|--------|
| Enable SSH | Ensures `sshd` is installed and enabled (`systemctl enable sshd`) |
| Enable RDP | Installs `xrdp` and enables it for Remote Desktop access |
| Extra packages | Space-separated list of dnf/apt packages to install |
| Custom Containerfile | Arbitrary instructions appended verbatim to the generated Containerfile |

## REST API

See [api.md](api.md#bootc-builds-lima-bootc-image-only) for the full bootc build API reference.

## How it works internally

```
User submits image URI
       │
       ▼
[Optional] Generate Containerfile from customizations
       │   podman build --network=host → localhost/derived:<id>
       ▼
podman run --privileged bootc-image-builder \
  -v /var/lib/containers/storage:/var/lib/containers/storage \
  --device /dev/fuse \
  → qcow2 written to /output/qcow2/disk.qcow2
       │
       ▼
limactl create --disk <qcow2> <vm-name>
       │
       ▼
VM appears in dashboard
```

The `/var/lib/containers/storage` volume share lets `bootc-image-builder`'s internal podman access locally-built derived images without pushing to a registry. `fuse-overlayfs` is configured as podman's storage mount program so the nested overlay works correctly inside the Docker/Podman container.
