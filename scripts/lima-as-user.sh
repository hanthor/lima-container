#!/usr/bin/env bash
set -Eeuo pipefail

if [ "$#" -lt 1 ]; then
  echo "Usage: lima-as-user <command> [args...]" >&2
  exit 1
fi

: "${LIMA_USER:=lima}"
: "${LIMA_HOME:=/var/lib/lima}"

if ! id "$LIMA_USER" >/dev/null 2>&1; then
  echo "Configured LIMA_USER '$LIMA_USER' does not exist." >&2
  exit 1
fi

if [ "$(id -u)" -eq 0 ] && [ "$LIMA_USER" != "root" ]; then
  exec su - "$LIMA_USER" -s /bin/bash -c "export LIMA_HOME=\"$LIMA_HOME\"; cd \"$LIMA_HOME\" && exec $(printf '%q ' "$@")"
fi

exec "$@"
