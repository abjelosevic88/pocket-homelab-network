# Configuration

Environment variables; every one has a default. Personal settings (offline time, alerts, quiet hours, connections, the household) live in the UI per person — the `*_URL`/`*_TOKEN`/`OPENAI_*`/`NOTIFY_*` values below are copied once into the first admin's settings, and the first admin gets the first sources (a scan, Tailscale, AdGuard Home) once each.

| Variable | Default | What |
|---|---|---|
| `PORT`, `HOST` | `8080`, `0.0.0.0` | Listen address (with host networking: the host's port) |
| `BASE_URL` | – | Public URL (links in alerts, agent commands, the OIDC callback) |
| `DATA_DIR` | `./data` | Database, `secret.key`, Core token, backups |
| `DB_PATH` | `$DATA_DIR/pocket-network.db` | SQLite file |
| `MAX_BODY_BYTES` | `67108864` | Largest JSON request, e.g. an export being imported (64 MB) |
| `TZ` | – | Time zone for history, quiet hours and the day summary |
| `AUTH_MODE` | `local` | `local` (accounts), `proxy` (trust `AUTH_PROXY_HEADER`/`AUTH_PROXY_EMAIL_HEADER`), `none` |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD`, `ADMIN_NAME` | – | Seed the first admin while the database is empty |
| `SESSION_TTL`, `SECURE_COOKIES` | `720h`, `false` | Set `SECURE_COOKIES=true` behind HTTPS |
| `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_NAME`, `OIDC_AUTO_CREATE` | – | Sign in with an OpenID Connect provider (not needed with Pocket Core) |
| `LOCAL_SCAN` | `true` | Let admins scan the networks this machine is on (host networking + `CAP_NET_RAW` in a container) |
| `SCAN_INTERFACE`, `SCAN_SUBNET`, `SCAN_INTERVAL` | default route, its subnet, `5m` | The owner's first scan |
| `TAILSCALE_SOCKET` | `/var/run/tailscale/tailscaled.sock` | When it exists, the owner gets a Tailscale source reading this machine's peers |
| `ADGUARD_URL`, `ADGUARD_USER`, `ADGUARD_PASSWORD` | – | AdGuard Home as the owner's first DNS source (or use your login in Pocket Core) |
| `WAN_CHECK_URL` | `https://1.1.1.1/cdn-cgi/trace` | Where the public address is asked (hourly; text with `ip=` or the bare address) |
| `NOTIFY_URL`, `NOTIFY_FORMAT` | – | Apprise (`…/notify/key`), ntfy topic URL or JSON webhook; format `apprise`/`ntfy`/`json` |
| `INVENTORY_URL`, `INVENTORY_TOKEN` | – | Pocket Inventory, direct connection (or use Pocket Core) |
| `MONITORING_URL`, `MONITORING_TOKEN` | – | Pocket Monitoring, direct connection (or use Pocket Core) |
| `OPENAI_API_KEY`, `OPENAI_MODEL`, `OPENAI_BASE_URL` | – | "What could this be?" and questions without Pocket AI |
| `CORE_URL`, `CORE_REGISTRATION_KEY`, `CORE_APP_URL` | – | Pocket Core |
| `TRUST_PROXY`, `METRICS_ENABLED`, `LOG_LEVEL`, `LOG_FORMAT` | `true`, `true`, `info`, `text` | |
| `DEMO_DATA` | `false` | Seed a household with two months of history into the owner account; nothing is scanned |

Commands: `pocket-network agent -server URL -token pna_…`, `pocket-network scan [-subnet] [-interface]`, `pocket-network backup [-o FILE]`, `pocket-network reset-password [-email] [-password]`, `pocket-network version`.

The agent alone, for routers and small boxes: `pocket-network-agent -server URL -token pna_… [-subnet CIDR] [-interface NAME] [-every 5m] [-once]` (also `NETWORK_SERVER`, `NETWORK_AGENT_TOKEN`). It reads dnsmasq's lease file for names when it runs on the router.
