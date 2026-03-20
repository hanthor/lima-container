# Architecture

## Data flow

1. Browser connects to noVNC over HTTP on `WEB_PORT` (default 8006).
2. noVNC opens websocket path `/websockify`.
3. nginx proxies `/websockify` to websocketd on `WSS_PORT`.
4. websocketd runs `lima-websocket-bridge`, which reads `/run/lima-vnc-port` and connects to that VNC port using `nc`.
5. Lima VM VNC stream is rendered in browser.

```
Browser → noVNC (port 8006) → nginx → websocketd → nc → Lima QEMU VNC (port 5900)
```

## Runtime model

- Container starts noVNC bridge services (nginx + websocketd).
- By default (`AUTO_START_LIMA=Y`), the container auto-boots the VM selected by `LIMA_TEMPLATE`.
- `LIMA_TEMPLATE` accepts a template name (`default`, `k8s`), a path to a YAML file, or a path to a disk image (`.qcow2`, `.img`, `.raw`). Disk images get a minimal template auto-generated.
- `lima-up` starts Lima, detects the QEMU VNC port, and retargets noVNC automatically.
- Set `AUTO_START_LIMA=N` for manual control; then start VMs with `docker exec lima-qemu lima-up <target>`.
- v1 supports one active VNC endpoint for simplicity.

## Script overview

| Script | Purpose |
|--------|---------|
| `lima-entrypoint` | Container entrypoint: starts nginx, websocketd, and optionally lima-up |
| `lima-up` | Start a Lima VM from a template name, YAML path, or disk image path |
| `lima-as-user` | Run a command as the unprivileged Lima user |
| `lima-detect-vnc-port` | Find the QEMU VNC port for the running Lima instance |
| `lima-use-vnc-port` | Write the active VNC port to `/run/lima-vnc-port` (retargets noVNC) |
| `lima-websocket-bridge` | Bridge websocket connection to Lima's QEMU VNC port via `nc` |
| `lima-preflight` | Validate host kernel/device prerequisites at container startup |
| `qemu-tcg-wrapper` | Rewrite KVM-only QEMU args to TCG-safe equivalents for software emulation |

## KVM vs TCG

| Mode | `LIMA_ACCEL_MODE` | Condition | Speed |
|------|--------------------|-----------|-------|
| KVM | `auto` or `kvm` | `/dev/kvm` present and writable | Native |
| TCG | `auto` or `tcg` | `/dev/kvm` absent | ~10x slower |

In TCG mode, `qemu-tcg-wrapper` intercepts Lima's QEMU invocation and rewrites:
- `-cpu host` / `-cpu kvm64` → `-cpu max`
- `-accel kvm` → `-accel tcg,thread=multi,tb-size=512`
- Missing OVMF VARS pflash → auto-generated writable copy
