# Backup

One SQLite file: `DATA_DIR/pocket-network.db` (WAL mode). Take a consistent copy with `pocket-network backup` (writes `DATA_DIR/backups/pocket-network-<time>.db` with `VACUUM INTO`) or Settings → Backup & data → Download database (admin). Back up that copy, not the live file. Also keep `DATA_DIR/secret.key`: it seals the passwords of your sources (AdGuard Home, the router, a Tailscale API key); without it you type them again. `DATA_DIR/core.token*` is the Pocket Core registration.

Restore: stop the app, replace `pocket-network.db` (remove `-wal`/`-shm` files), start.

Every person can also export their own devices as JSON (`/api/v1/export.json`, re-importable, matched by MAC address) or as CSV.
