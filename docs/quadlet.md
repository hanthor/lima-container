# Quadlet (systemd)

All three images ship [Podman Quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html) unit files in the `quadlet/` directory. These let you manage the container as a systemd service — auto-start on login or boot, restart on failure, and `journalctl` logs.

## Files

| File | Image | Notes |
|------|-------|-------|
| `quadlet/lima.container` | `lima:latest` | Plain, single VM |
| `quadlet/lima-web.container` | `lima-web:latest` | Web dashboard (recommended) |
| `quadlet/lima-bootc.container` | `lima-bootc:latest` | Web dashboard + bootc builder |

Edit each file and replace `YOUR_ORG` with your GitHub organisation or registry namespace.

## User service (no root required)

Runs as your login user. VMs and the dashboard start when you log in.

```bash
cp quadlet/lima-web.container ~/.config/containers/systemd/
# edit: replace YOUR_ORG
systemctl --user daemon-reload
systemctl --user start lima-web
systemctl --user enable lima-web     # start automatically on login
```

## System service (root, starts at boot)

```bash
sudo cp quadlet/lima-bootc.container /etc/containers/systemd/
# edit: replace YOUR_ORG
sudo systemctl daemon-reload
sudo systemctl start lima-bootc
sudo systemctl enable lima-bootc
```

## Managing the service

```bash
systemctl --user status lima-web
systemctl --user stop lima-web
systemctl --user restart lima-web
journalctl --user -u lima-web -f     # follow logs
```

## Notes

- `lima-bootc.container` uses `PodmanArgs=--privileged` and `AddDevice=/dev/fuse` — both are required by `bootc-image-builder` and `fuse-overlayfs`.
- `lima.container` and `lima-web.container` do **not** need `--privileged`.
- The `%h` variable in `Volume=` expands to your home directory, so `~/.lima` is mounted automatically.
- `AUTO_START_LIMA=N` is the default in the Quadlet files — use the web dashboard to start VMs rather than auto-starting one on container launch.
