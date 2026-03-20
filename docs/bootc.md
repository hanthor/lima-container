# Bootc image builder

The `lima-bootc` image can build Lima VMs from [bootc](https://containers.github.io/bootc/) container image URIs using [`bootc-image-builder`](https://github.com/osbuild/bootc-image-builder).

## Running

```bash
# Docker
docker run -d \
  --name lima-bootc \
  --privileged \
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
  --device /dev/kvm \
  --device /dev/net/tun \
  --device /dev/fuse \
  --cap-add NET_ADMIN \
  -p 8006:8006 \
  -v lima-bootc-builds:/var/lib/lima-bootc-builds \
  -e AUTO_START_LIMA=N \
  ghcr.io/<your-org>/lima-bootc:latest
```

**`--privileged`** is required — `bootc-image-builder` uses loop devices and osbuild internally.  
**`--device /dev/fuse`** is required — `fuse-overlayfs` is used for nested container storage.

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

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/bootc/builds` | Start a build |
| `GET` | `/api/bootc/builds` | List all builds with status |
| `GET` | `/api/bootc/builds/:id` | Get build status |
| `GET` | `/api/bootc/builds/:id/log` | Stream build log (SSE) |
| `DELETE` | `/api/bootc/builds/:id` | Cancel / clean up a build |

**Request body for `POST /api/bootc/builds`:**

```json
{
  "image": "quay.io/fedora/fedora-bootc:42",
  "vm_name": "my-fedora-vm",
  "customizations": {
    "enable_ssh": true,
    "enable_rdp": false,
    "extra_packages": ["vim", "curl"],
    "extra_containerfile": "RUN echo 'welcome' > /etc/motd"
  }
}
```

All `customizations` fields are optional. Omit the `customizations` key entirely to skip the customization step and pass the image directly to `bootc-image-builder`.

## Storage

Built qcow2 images are stored at `/var/lib/lima-bootc-builds/<id>/qcow2/disk.qcow2`. Mount a named volume to persist them across container restarts:

```bash
-v lima-bootc-builds:/var/lib/lima-bootc-builds
```

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
