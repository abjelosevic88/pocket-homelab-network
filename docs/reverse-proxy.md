# Reverse proxy

Set `BASE_URL` to the public address and `SECURE_COOKIES=true` behind HTTPS. See [deploy/reverse-proxy/Caddyfile](../deploy/reverse-proxy/Caddyfile). The container uses host networking (to scan the LAN), so proxy to the host's `PORT`. `/api/*` must stay reachable for agents (`POST /api/v1/agent/report`), Homepage widgets, shortcuts and linked apps; they send their own bearer token.
