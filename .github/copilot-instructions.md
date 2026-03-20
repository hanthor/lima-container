# Copilot Instructions

## What this repo is

A Docker image that runs Linux VMs with a graphical desktop accessible in the browser. Built on `ghcr.io/qemus/qemu`. The VM boots inside the container; users connect via a web dashboard at port 8006 that provides VM management (start/stop/create/delete) and VNC console access. Supports amd64 and arm64.

## Build and lint

```bash
# Build the image (includes Go build stage for lima-web)
docker build -t lima:test .

# Build just the Go server (for local development)
cd web && go build -o /dev/null .

# Lint all shell scripts
shellcheck scripts/*.sh

# Smoke test (build + verify dashboard endpoint serves)
docker build -t lima:smoke . && \
  docker run -d --name smoke -p 18006:8006 lima:smoke && \
  sleep 5 && curl -fsS http://127.0.0.1:18006/dashboard/ >/dev/null && \
  docker rm -f smoke
```

There are no unit tests. CI runs shellcheck, a Docker build, and a smoke test on every PR.

## Architecture

```
Browser
  ├── /              → redirect to /dashboard/
  ├── /dashboard/    → nginx → lima-web Go server (VM management UI)
  ├── /api/*         → nginx → lima-web Go server (REST API)
  └── /vnc/vnc.html  → nginx → noVNC client
       └── /websockify/<instance> → nginx → websocketd → nc → QEMU VNC
```

- **nginx** is the front door: proxies `/api/*` and `/dashboard/` to `lima-web`, serves noVNC at `/vnc/`, routes `/websockify/<instance>` to per-instance websocketd processes.
- **lima-web** is a Go HTTP server (stdlib only, no deps) that wraps `limactl` as a REST API and manages per-instance websocketd lifecycle. Compiled in a multi-stage Docker build.
- **websocketd** processes (one per running VM) bridge VNC. Each spawned with `--binary` — VNC/RFB is a binary protocol, corrupted in websocketd's default line mode.
- **VNC config** is auto-generated at `/etc/nginx/lima-vnc-locations.conf` by lima-web and included inside the nginx server block. nginx is reloaded on changes.
- Lima runs QEMU as the unprivileged `lima` user (UID 1000). All Lima commands must go through `lima-as-user`.
- Once a VM starts, `lima-web` detects VNC port from `$LIMA_HOME/<instance>/vncdisplay`, spawns websocketd on a unique port (5710-5799), and generates the nginx routing config.

## Go server (`web/`)

- `main.go` — entry point, route registration, startup VNC scan for running instances
- `lima.go` — LimaCtl wrapper: shells out to `lima-as-user limactl`, parses NDJSON output (one JSON object per line, NOT a JSON array), write mutex for mutations
- `handlers.go` — REST API handlers with consistent `{"data": ...}` / `{"error": ...}` JSON response format
- `vnc.go` — VNCManager: per-instance websocketd lifecycle, port allocation, nginx config generation
- `static/` — vanilla HTML/JS/CSS dashboard (designed for future swap to htmx or React/Vue)

Key design decisions:
- No external Go dependencies — stdlib only (`net/http`, `os/exec`, `encoding/json`)
- Write mutex serializes start/stop/delete; reads are concurrent
- Frontend polls every 5s (simpler than SSE/websockets for the dashboard)

## Script conventions

All scripts in `scripts/` use `#!/usr/bin/env bash` with `set -Eeuo pipefail`.

Environment variable defaults use the `: "${VAR:=default}"` pattern, not `export`.

Scripts are installed into the container under `/usr/local/bin/` **without the `.sh` extension** (e.g., `scripts/lima-up.sh` → `/usr/local/bin/lima-up`). Update `Dockerfile` when adding a new script.

## TCG (software emulation) mode

When KVM is unavailable, `lima-up` sets `QEMU_SYSTEM_X86_64=qemu-tcg-wrapper` in the QEMU environment. `qemu-tcg-wrapper` rewrites Lima's QEMU args at launch:
- `-cpu host`/`kvm64` → `-cpu max`
- `-accel kvm` → `-accel tcg,thread=multi,tb-size=512`
- Injects a writable OVMF VARS pflash copy if only a read-only CODE pflash is present.

Other architectures (arm64, riscv64, etc.) get their TCG overrides via direct `QEMU_SYSTEM_*` env vars in `lima-up.sh`.

## Lima templates

Templates live in `templates/*.yaml` and are copied to `/opt/lima/templates/` in the image. `lima-up` also accepts:
- A bare name (`default`, `k8s`) → resolved to `/opt/lima/templates/<name>.yaml`
- A path to a `.yaml` file
- A path to a `.qcow2`, `.img`, or `.raw` disk image → a minimal Lima YAML is auto-generated in `/tmp/`

All Lima templates used with VNC must include `video: {display: "vnc"}` and `containerd: {system: false, user: false}`.

## Key environment variables

| Variable | Default | Notes |
|---|---|---|
| `LIMA_TEMPLATE` | `default` | Template name, YAML path, or disk image path |
| `LIMA_ACCEL_MODE` | `auto` | `auto` \| `kvm` \| `tcg` |
| `AUTO_START_LIMA` | `Y` | Set to `N` to disable auto-boot |
| `LIMA_HOME` | `/var/lib/lima` | Lima state directory — mount `~/.lima` here to reuse host instances |
| `LIMA_WEB_PORT` | `8080` | Internal port for the Go web server |
| `WEB_PORT` | `8006` | External HTTP port (nginx) |

## Running the container

Requires `--device /dev/net/tun` (Lima networking) and `--cap-add NET_ADMIN`. **Rootful Podman is recommended** — rootless Podman cannot pass `/dev/net/tun` properly. When mounting `~/.lima` with rootful Podman, add `--security-opt label=disable` to avoid SELinux denials.

```bash
# Docker
docker run -d --device /dev/kvm --device /dev/net/tun --cap-add NET_ADMIN \
  -p 8006:8006 -v ~/.lima:/var/lib/lima -e LIMA_TEMPLATE=centos-stream-10-gnome lima:test

# Rootful Podman
sudo podman run -d --device /dev/kvm --device /dev/net/tun --cap-add NET_ADMIN \
  --security-opt label=disable \
  -p 8006:8006 -v ~/.lima:/var/lib/lima -e LIMA_TEMPLATE=centos-stream-10-gnome lima:test
```

## Release

Images are published to `ghcr.io/<org>/lima` as multi-platform (amd64 + arm64) on pushes to `v*.*.*` tags via the release workflow. Dependency updates are managed by Renovate, grouping Dockerfile and GitHub Actions updates together.
