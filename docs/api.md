# API

JSON over HTTP under `/api/v1`. Authenticate with the session cookie, a personal token (`Authorization: Bearer pnet_…`, Settings → API tokens), a Pocket Core token, or a Pocket Core link token (`pcx_…`, only the routes of its scope). Everything is per person: other people's ids answer 404.

## Devices

| | |
|---|---|
| `GET /devices?online=1&review=new&watch=1&owner=&kind=&q=` | The list, pinned first then by name |
| `POST /devices` | Add one by hand: `{"name", "kind", "owner", "location", "notes", "mac", "ip"}` |
| `GET /devices/{id}` | `{device, ids, addresses, events, presence, similar, services, can_wake}` |
| `PUT /devices/{id}` | Any of `name, owner, kind, location, notes, link_url, review (new/known/guest), watch, muted, pinned` |
| `DELETE /devices/{id}` | Forget it (it comes back as new if seen again) |
| `POST /devices/{id}/merge` | `{"ids": [..]}` — these are the same device: fold them into this one |
| `DELETE /devices/{id}/ids/{ident}` | This MAC / Tailscale node is not this device |
| `POST /devices/{id}/services` | Check common services on its address (own networks only) |
| `POST /devices/{id}/wake` | Wake-on-LAN (devices on this server's own network) |
| `POST /devices/{id}/identify` | AI guess: `{kind, name, confidence, why, check}` |
| `GET /devices/{id}/linked` | Its Pocket Inventory item and Pocket Monitoring server, live |
| `PUT /devices/{id}/inventory`, `PUT /devices/{id}/monitoring` | `{"id", "name"}`; `id` 0 unlinks |
| `POST /devices/review` | `{"ids": [..], "review": "known", "owner"?, "muted"?}` |
| `GET /review` | New devices: `{groups (look-alikes), devices, owners, locations, total}` |
| `GET /devices/meta` | Owners, places, kinds for pickers |

Kinds: `phone tablet laptop computer watch tv speaker console printer camera smart_home router access_point switch server nas car other`; `kind_guess` is the app's guess while `kind` is empty.

## Activity and overview

| | |
|---|---|
| `GET /events?kind=new,offline&device=&before=&limit=` | Newest first. Kinds: `new online offline ip address merged renamed wan_ip source_failed source_ok imported wake` |
| `GET /overview` | The home page in one call |
| `GET /dashboard` | Headline counts, networks, who is home — for Homepage widgets |
| `GET /summary/YYYY-MM-DD` | One day: new devices, watched outages, internet address — what Pocket Day reads |

## Sources

| | |
|---|---|
| `GET /sources` | `{sources, can, kinds}` |
| `POST /sources` | `{"kind": "scan"\|"agent"\|"tailscale"\|"adguard"\|"openwrt", "name", "network", "cfg": {...}, "secret", "interval_s", "presence"}`; an agent answers with its `token` once |
| `PUT/DELETE /sources/{id}` | Change or remove (devices stay) |
| `POST /sources/{id}/run` | Run now: `{ok, found, new, error}` |
| `POST /scan` | Run every switched-on source now |
| `POST /sources/{id}/token` | New agent token |
| `GET /sources/{id}/scans` | Recent runs |
| `POST /agent/report` | For agents, with `Authorization: Bearer pna_…`: `{"subnet", "method", "took_ms", "hosts": [{"ip", "mac", "hostname", "name_source", "vendor", "model"}], "error"}`; answers `{interval_s}` |

`cfg`: scan `{interface, subnet, names, dns}` (admins only), tailscale `{socket}` (admins) or a `secret` API key with `{tailnet}`, adguard `{url, username}` + `secret` or `{via_core: true}`, openwrt `{url, username}` + `secret`.

## Moving over and data

| | |
|---|---|
| `POST /import/netalertx` | Multipart `file` = NetAlertX `devices.csv`: `{created, updated, skipped}`; idempotent |
| `GET /export.json`, `POST /import` | Your devices with identifiers and addresses; idempotent by MAC / Tailscale node |
| `GET /export.csv` | The device list |
| `GET /backup` | The whole database (admins) |

## Pocket Core

Offers `network:read` (devices, events, overview, dashboard, review, summary) and `network:write` (add devices; rename and describe them — reviews and alerts only of devices that app added). Publishes `network.device_new`, `network.device_offline`, `network.device_back` (watched devices) and `network.wan_ip_changed`, each with `user_email`. Uses `items:read` in Pocket Inventory and `monitoring:read` in Pocket Monitoring, and Pocket AI.
