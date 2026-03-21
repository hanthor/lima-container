#!/usr/bin/env bash
# First-boot setup for GNOME Remote Desktop system service (RDP remote login).
# Runs once via gnome-rdp-setup.service before gnome-remote-desktop starts.
set -euo pipefail

CERT_DIR=/etc/gnome-remote-desktop
MARKER="$CERT_DIR/.rdp-configured"

# Idempotent: skip if already configured (e.g. after a reboot).
[[ -f "$MARKER" ]] && exit 0

mkdir -p "$CERT_DIR"

# Generate a self-signed TLS certificate for RDP.
openssl req -new -x509 -days 3650 -nodes \
    -out  "$CERT_DIR/rdp-tls.crt" \
    -keyout "$CERT_DIR/rdp-tls.key" \
    -subj "/CN=lima-rdp"

chown gnome-remote-desktop:gnome-remote-desktop \
    "$CERT_DIR/rdp-tls.crt" \
    "$CERT_DIR/rdp-tls.key"

# Configure grdctl in --system mode (remote login / GDM screen).
grdctl --system rdp enable
grdctl --system rdp set-tls-cert "$CERT_DIR/rdp-tls.crt"
grdctl --system rdp set-tls-key  "$CERT_DIR/rdp-tls.key"
grdctl --system rdp set-credentials lima lima
grdctl --system rdp disable-view-only

touch "$MARKER"
