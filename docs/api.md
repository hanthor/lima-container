# REST API reference

The Go server (`lima-web`) exposes a REST API at `/api/*`. All responses are JSON with the shape `{"data": ...}` on success or `{"error": "..."}` on failure.

Available in `lima-web` and `lima-bootc` images only (not the plain `lima` image).

## Instance management

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/instances` | List all VMs |
| `GET` | `/api/instances/:name` | Get VM details |
| `POST` | `/api/instances/:name/start` | Start a stopped VM |
| `POST` | `/api/instances/:name/stop` | Stop a running VM |
| `POST` | `/api/instances/:name/restart` | Restart a VM |
| `DELETE` | `/api/instances/:name` | Delete a VM |
| `GET` | `/api/instances/:name/vnc` | Get VNC connection info |
| `POST` | `/api/instances/create` | Create a new VM from a template |
| `GET` | `/api/templates` | List available templates |
| `GET` | `/api/info` | Host diagnostics |

### Examples

```bash
# Create a VM
curl -X POST http://localhost:8006/api/instances/create \
  -H 'Content-Type: application/json' \
  -d '{"template": "default"}'

# Start a VM
curl -X POST http://localhost:8006/api/instances/default/start

# Get VNC console URL
curl http://localhost:8006/api/instances/default/vnc
# → {"data": {"port": 5710, "password": "abc123", "url": "/vnc/vnc.html?autoconnect=1&..."}}

# Check bootc support
curl http://localhost:8006/api/info | jq .data.bootc_enabled
```

## Bootc builds (`lima-bootc` only)

See [bootc.md](bootc.md#rest-api) for the full bootc build API.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/bootc/builds` | Start a build |
| `GET` | `/api/bootc/builds` | List all builds |
| `GET` | `/api/bootc/builds/:id` | Get build status |
| `GET` | `/api/bootc/builds/:id/log` | Stream build log (SSE) |
| `DELETE` | `/api/bootc/builds/:id` | Delete a build |
