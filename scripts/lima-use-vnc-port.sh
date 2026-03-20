#!/usr/bin/env bash
set -Eeuo pipefail

: "${LIMA_VNC_PORT_FILE:=/run/lima-vnc-port}"

if [ $# -ne 1 ]; then
  echo "Usage: lima-use-vnc-port <port>" >&2
  exit 1
fi

port="${1}"
if ! [[ "$port" =~ ^[0-9]+$ ]]; then
  echo "Invalid port: $port" >&2
  exit 1
fi

if [ "$port" -lt 5900 ] || [ "$port" -gt 65535 ]; then
  echo "Port out of expected range: $port" >&2
  exit 1
fi

echo "$port" > "$LIMA_VNC_PORT_FILE"
echo "noVNC bridge now targets VNC port $port"
