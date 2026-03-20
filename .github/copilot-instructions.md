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
  ├── /api/*         → nginx → lima-web Go server (REST API + OpenAPI spec)
  ├── /vnc/vnc.html  → nginx → noVNC static files
  ├── /websockify/*  → nginx → lima-web Go WebSocket→TCP proxy → QEMU VNC
  └── /api/instances/{name}/shell → nginx → lima-web WebSocket→PTY proxy
```

- **nginx** is the front door: proxies `/api/*`, `/dashboard/`, and `/websockify/` to `lima-web`, and serves noVNC static files at `/vnc/`. The nginx config is fully static — no generated includes.
- **lima-web** is a Go HTTP server that wraps `limactl` as a REST API and handles WebSocket proxying for both VNC and shell terminals. Compiled in a multi-stage Docker build. Uses `gorilla/websocket` for WebSocket upgrade + proxying and `creack/pty` for shell PTY management.
- **VNC proxy** — `vnc.go` upgrades to WebSocket (negotiating the `binary` subprotocol that noVNC requires), then dials the QEMU VNC TCP port and bidirectionally copies frames. No per-instance subprocesses; connections are proxied on-demand.
- **Shell proxy** — `shell.go` upgrades to WebSocket, starts `limactl shell` inside a PTY (`creack/pty`), and proxies terminal I/O. Uses a simple binary+JSON framing protocol: binary frames for terminal data, text frames for JSON resize events (`{"type":"resize","cols":N,"rows":N}`).
- Lima runs QEMU as the unprivileged `lima` user (UID 1000). All Lima commands must go through `lima-as-user`.
- On startup, `lima-web` scans for already-running instances and registers their VNC ports (read from `$LIMA_HOME/<instance>/vncdisplay`).

## Go server (`web/`)

- `main.go` — entry point, route registration, OpenAPI spec embed (`//go:embed static/openapi.yaml`), startup VNC scan for running instances
- `lima.go` — LimaCtl wrapper: shells out to `lima-as-user limactl`, parses NDJSON output (one JSON object per line, NOT a JSON array), write mutex for mutations
- `handlers.go` — REST API handlers with consistent `{"data": ...}` / `{"error": ...}` JSON response format
- `vnc.go` — VNCManager: WebSocket→TCP VNC proxy using `gorilla/websocket`, `binary` subprotocol negotiation, on-demand per-connection proxying (no subprocesses)
- `shell.go` — WebSocket shell terminal: upgrades to WebSocket, starts `limactl shell` in a PTY (`creack/pty`), proxies binary terminal I/O + JSON resize events
- `bootc.go` — bootc image builder: Containerfile generation, `podman build`, `bootc install to-disk`, raw→qcow2 conversion, auto-start Lima VM from built image
- `static/` — vanilla HTML/JS/CSS dashboard + `openapi.yaml` (embedded at build time)

External Go dependencies:
- `github.com/gorilla/websocket v1.5.3` — WebSocket upgrade + proxying for VNC and shell. Required for `binary` subprotocol negotiation (noVNC requires this; stdlib `net/http` doesn't support WebSocket).
- `github.com/creack/pty v1.1.24` — PTY allocation and resize for shell terminal sessions.

Key design decisions:
- Replaced per-instance websocketd subprocesses with a Go WebSocket→TCP proxy. This eliminated dynamic nginx config generation, port allocation, and process lifecycle management. The Go proxy negotiates the `binary` subprotocol directly, which websocketd could not do (noVNC requires it for the RFB protocol).
- Write mutex serializes start/stop/delete; reads are concurrent
- Frontend polls every 5s (simpler than SSE/websockets for the dashboard)
- OpenAPI spec is embedded via `//go:embed` and served at `GET /api/openapi.yaml`

## Dockerfile variants

Three image variants, all based on `ghcr.io/qemus/qemu`:
- **`Dockerfile`** — plain Lima container (no web dashboard, no Go server)
- **`Dockerfile.web`** — adds the lima-web Go server, nginx, and noVNC dashboard
- **`Dockerfile.bootc`** — everything in `.web` plus bootc-image-builder support (podman, bootc, qemu-nbd)

`Dockerfile.web` and `Dockerfile.bootc` use a multi-stage build with `golang:1.24-alpine` to compile `lima-web`.

## bootc image builder

The `Dockerfile.bootc` variant enables building bootc-based disk images from OCI container images. The builder generates a Containerfile for customizations, builds with `podman build`, installs to a raw disk via `bootc install to-disk`, converts to qcow2, and auto-starts a Lima VM. Builds run asynchronously with SSE log streaming. See `docs/bootc.md` for full documentation.

## API and OpenAPI spec

The REST API is documented in `docs/api.md`. An OpenAPI 3.1 spec lives at `web/static/openapi.yaml`, is embedded into the Go binary via `//go:embed`, and served at `GET /api/openapi.yaml`.

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
