#!/usr/bin/env bash
set -Eeuo pipefail

: "${WEB_PORT:=8006}"
: "${LIMA_WEB_PORT:=8080}"
: "${LIMA_TEMPLATE:=default}"
: "${AUTO_START_LIMA:=N}"
: "${LIMA_HOME:=/var/lib/lima}"
: "${TLS_CERT:=}"
: "${TLS_KEY:=}"

# Build nginx listen directive and optional SSL directives
if [ -n "${TLS_CERT}" ] && [ -n "${TLS_KEY}" ]; then
  LISTEN_DIRECTIVE="listen ${WEB_PORT} ssl default_server"
  SSL_DIRECTIVES="
  ssl_certificate ${TLS_CERT};
  ssl_certificate_key ${TLS_KEY};
  ssl_protocols TLSv1.2 TLSv1.3;
  ssl_ciphers HIGH:!aNULL:!MD5;
  ssl_prefer_server_ciphers on;"
else
  LISTEN_DIRECTIVE="listen ${WEB_PORT} default_server"
  SSL_DIRECTIVES=""
fi

mkdir -p /etc/nginx/conf.d /var/log/nginx "${LIMA_HOME}"
# Ensure the lima user (uid 1000) owns its home directory (important when the
# directory comes from a host volume mounted as root).
chown lima:lima "${LIMA_HOME}" 2>/dev/null || true

/usr/local/bin/lima-preflight || true

cat > /etc/nginx/conf.d/default.conf <<NGINX
map \$http_upgrade \$connection_upgrade {
  default upgrade;
  ''      close;
}

server {
  ${LISTEN_DIRECTIVE};
  ${SSL_DIRECTIVES}
  server_name _;
  absolute_redirect off;

  # Dashboard and API — proxy to lima-web Go server
  location /dashboard/ {
    proxy_pass http://127.0.0.1:${LIMA_WEB_PORT}/dashboard/;
  }

  location /api/ {
    proxy_http_version 1.1;
    proxy_pass http://127.0.0.1:${LIMA_WEB_PORT}/api/;
    proxy_read_timeout 600s;
    proxy_set_header Upgrade \$http_upgrade;
    proxy_set_header Connection \$connection_upgrade;
    proxy_set_header Host \$host;
  }

  # noVNC static files — shared by all instances
  location /vnc/ {
    root /usr/share/novnc;
    rewrite ^/vnc/(.*)\$ /\$1 break;
  }

  # WebSocket→VNC proxy — handled directly by the Go server
  location /websockify/ {
    proxy_http_version 1.1;
    proxy_read_timeout 3600s;
    proxy_set_header Upgrade \$http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host \$host;
    proxy_pass http://127.0.0.1:${LIMA_WEB_PORT}/websockify/;
  }

  # RDP static files (IronRDP WASM client)
  location /rdp/ {
    alias /usr/share/lima-web/static/rdp/;
    try_files \$uri @rdp_proxy;
  }

  # RDCleanPath WebSocket proxy for IronRDP
  location @rdp_proxy {
    proxy_http_version 1.1;
    proxy_read_timeout 3600s;
    proxy_set_header Upgrade \$http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host \$host;
    proxy_pass http://127.0.0.1:${LIMA_WEB_PORT};
  }

  # Root redirect to dashboard
  location = / {
    return 302 /dashboard/;
  }
}
NGINX

nginx -t
nginx

# Start the Go web server in the background.
export LIMA_HOME LIMA_WEB_PORT
lima-web >/var/log/lima-web.log 2>&1 &
web_pid="$!"

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
  kill "$web_pid" 2>/dev/null || true
  nginx -s stop 2>/dev/null || true
}
trap cleanup EXIT INT TERM

wait "$web_pid"
