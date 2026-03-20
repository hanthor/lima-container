#!/usr/bin/env bash
set -Eeuo pipefail

: "${WEB_PORT:=8006}"
: "${WSS_PORT:=5700}"
: "${LIMA_TEMPLATE:=default}"
: "${AUTO_START_LIMA:=N}"
: "${LIMA_HOME:=/var/lib/lima}"
: "${LIMA_VNC_PORT:=5901}"
: "${LIMA_VNC_PORT_FILE:=/run/lima-vnc-port}"

mkdir -p /etc/nginx/conf.d /var/log/nginx "${LIMA_HOME}"
echo "${LIMA_VNC_PORT}" > "${LIMA_VNC_PORT_FILE}"

echo "noVNC bridge target initialized on VNC port ${LIMA_VNC_PORT}" >&2

/usr/local/bin/lima-preflight || true

cat > /etc/nginx/conf.d/default.conf <<NGINX
server {
  listen ${WEB_PORT} default_server;
  server_name _;

  location / {
    root /usr/share/novnc;
    index vnc.html;
  }

  location /websockify {
    proxy_http_version 1.1;
    proxy_read_timeout 61s;
    proxy_set_header Upgrade \$http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_pass http://127.0.0.1:${WSS_PORT}/;
  }
}
NGINX

nginx -t
nginx

websocketd --address 127.0.0.1 --port="${WSS_PORT}" /usr/local/bin/lima-websocket-bridge >/var/log/websocketd.log 2>&1 &
ws_pid="$!"

if [[ "${AUTO_START_LIMA}" =~ ^([Yy]|1)$ ]]; then
  # Resolve LIMA_TEMPLATE to a target path for lima-up.
  #   Name only  → look up bundled template (e.g. "default" → /opt/lima/templates/default.yaml)
  #   *.yaml     → pass through as a YAML template path
  #   *.qcow2 / *.img / *.raw → pass through; lima-up auto-generates a template
  case "${LIMA_TEMPLATE}" in
    *.yaml|*.qcow2|*.img|*.raw)
      lima_target="${LIMA_TEMPLATE}"
      ;;
    *)
      lima_target="/opt/lima/templates/${LIMA_TEMPLATE}.yaml"
      ;;
  esac

  if [ -e "$lima_target" ]; then
    echo "Auto-starting Lima: ${lima_target}" >&2
    /usr/local/bin/lima-up "$lima_target" || true
  else
    echo "Lima target not found: ${lima_target}" >&2
    echo "Available templates:" >&2
    ls /opt/lima/templates/ >&2 || true
  fi
fi

cleanup() {
  kill "$ws_pid" 2>/dev/null || true
  nginx -s stop 2>/dev/null || true
}
trap cleanup EXIT INT TERM

wait "$ws_pid"
