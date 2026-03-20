#!/usr/bin/env bash
set -Eeuo pipefail

: "${LIMA_VNC_HOST:=127.0.0.1}"
: "${LIMA_VNC_PORT:=5901}"
: "${LIMA_VNC_PORT_FILE:=/run/lima-vnc-port}"

if [ -s "$LIMA_VNC_PORT_FILE" ]; then
  detected_port="$(tr -dc '0-9' < "$LIMA_VNC_PORT_FILE")"
  if [ -n "$detected_port" ]; then
    LIMA_VNC_PORT="$detected_port"
  fi
fi

if ! command -v nc >/dev/null 2>&1; then
  echo "nc is required for websocket bridge" >&2
  exit 1
fi

exec nc "$LIMA_VNC_HOST" "$LIMA_VNC_PORT"
