#!/usr/bin/env bash
set -Eeuo pipefail

: "${LIMA_HOME:=/var/lib/lima}"

# First, try reading the VNC display file written by Lima's hostagent.
# Format: "127.0.0.1:<display>" where display 0 = port 5900, 1 = 5901, etc.
vncdisplay_file=""
for f in "$LIMA_HOME"/*/vncdisplay; do
  [ -f "$f" ] && vncdisplay_file="$f" && break
done

if [ -n "$vncdisplay_file" ]; then
  display="$(cat "$vncdisplay_file" | sed 's/.*://')"
  # Lima VNC display number — convert to TCP port (5900 + display)
  port=$((5900 + display))
  if [ "$port" -ge 5900 ] && [ "$port" -le 65535 ]; then
    echo "$port"
    exit 0
  fi
fi

# Fallback: scan listening sockets in the VNC range attributed to qemu-system.
port="$(ss -ltnp 2>/dev/null | awk '/LISTEN/ {
  split($4, a, ":");
  p = a[length(a)];
  if (p >= 5900 && p <= 5999) print p
}' | sort -n | tail -n 1)"

if [ -z "$port" ]; then
  echo "VNC port not found" >&2
  exit 1
fi

echo "$port"
