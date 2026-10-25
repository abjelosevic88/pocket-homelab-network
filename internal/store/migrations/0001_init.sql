CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);


-- Where devices are seen. kind = scan (this server sweeps a local subnet;
-- admins only, it uses the box's network), agent (a pocket-network agent on
-- another network reports in), tailscale (the tailnet's peers), adguard
-- (AdGuard Home clients and their DNS activity) or openwrt (an OpenWrt or
-- GL.iNet router: DHCP leases and Wi-Fi clients). config is JSON (subnet,
-- interface, url, username, label …); secret is sealed (secret.go); an
-- agent's token is stored hashed.
CREATE TABLE sources (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL,
  name        TEXT NOT NULL DEFAULT '',
  network     TEXT NOT NULL DEFAULT '',            -- plain label of the network it sees ("Home Wi-Fi")
  config      TEXT NOT NULL DEFAULT '{}',
  secret      TEXT NOT NULL DEFAULT '',
  token_hash  TEXT NOT NULL DEFAULT '',            -- agent token (sha256)
  token_hint  TEXT NOT NULL DEFAULT '',
  enabled     INTEGER NOT NULL DEFAULT 1,
  interval_s  INTEGER NOT NULL DEFAULT 300,
  presence    INTEGER NOT NULL DEFAULT 1,          -- what it reports counts as "online"
  last_run    TEXT NOT NULL DEFAULT '',
  last_ok     TEXT NOT NULL DEFAULT '',
  last_error  TEXT NOT NULL DEFAULT '',
  last_count  INTEGER NOT NULL DEFAULT 0,
  last_ms     INTEGER NOT NULL DEFAULT 0,
  sort        INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX sources_user ON sources(user_id);
CREATE UNIQUE INDEX sources_token ON sources(token_hash) WHERE token_hash <> '';

-- A device: one physical thing, however many addresses it uses. review =
-- new (not looked at yet) | known | guest (a visitor's; never alerts when it
-- leaves). watch = tell me when it goes offline or comes back; muted = never
-- alert about it.
CREATE TABLE devices (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name             TEXT NOT NULL DEFAULT '',
  hostname         TEXT NOT NULL DEFAULT '',        -- best name the network gave it
  owner            TEXT NOT NULL DEFAULT '',        -- whose it is ("Aleksandar", "Guests")
  kind             TEXT NOT NULL DEFAULT '',        -- phone | laptop | computer | tablet | tv | ... | other ("" = not known)
  kind_guess       TEXT NOT NULL DEFAULT '',
  vendor           TEXT NOT NULL DEFAULT '',
  model            TEXT NOT NULL DEFAULT '',
  os               TEXT NOT NULL DEFAULT '',
  location         TEXT NOT NULL DEFAULT '',
  notes            TEXT NOT NULL DEFAULT '',
  link_url         TEXT NOT NULL DEFAULT '',        -- its own web page (router admin, printer)
  review           TEXT NOT NULL DEFAULT 'new',
  watch            INTEGER NOT NULL DEFAULT 0,
  muted            INTEGER NOT NULL DEFAULT 0,
  pinned           INTEGER NOT NULL DEFAULT 0,
  online           INTEGER NOT NULL DEFAULT 0,
  status_at        TEXT NOT NULL DEFAULT '',        -- when online last changed
  first_seen       TEXT NOT NULL DEFAULT '',
  last_seen        TEXT NOT NULL DEFAULT '',
  last_ip          TEXT NOT NULL DEFAULT '',
  last_mac         TEXT NOT NULL DEFAULT '',
  last_network     TEXT NOT NULL DEFAULT '',
  last_source      TEXT NOT NULL DEFAULT '',        -- source kind that saw it last
  random_mac       INTEGER NOT NULL DEFAULT 0,      -- uses a private (changing) address
  dns_queries      INTEGER NOT NULL DEFAULT 0,      -- AdGuard: queries in the last 24 hours
  dns_blocked      INTEGER NOT NULL DEFAULT 0,
  ports            TEXT NOT NULL DEFAULT '',        -- last "what's running" check (JSON)
  inventory_id     INTEGER NOT NULL DEFAULT 0,      -- Pocket Inventory item
  inventory_name   TEXT NOT NULL DEFAULT '',
  monitoring_id    INTEGER NOT NULL DEFAULT 0,      -- Pocket Monitoring server
  monitoring_name  TEXT NOT NULL DEFAULT '',
  source           TEXT NOT NULL DEFAULT 'scan',    -- scan | manual | import | <pocket app slug>
  created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX devices_user ON devices(user_id, review);
CREATE INDEX devices_user_seen ON devices(user_id, last_seen);

-- How a device is recognised: kind = mac | tailscale (node id) | adguard
-- (client name) | host (a hostname, for devices only known by name). One
-- identifier belongs to one device per person.
CREATE TABLE device_ids (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id   INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL,
  value       TEXT NOT NULL,
  first_seen  TEXT NOT NULL DEFAULT '',
  last_seen   TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX device_ids_value ON device_ids(user_id, kind, value);
CREATE INDEX device_ids_device ON device_ids(device_id);

-- Addresses a device used, per source.
CREATE TABLE addresses (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id   INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  ip          TEXT NOT NULL,
  mac         TEXT NOT NULL DEFAULT '',
  network     TEXT NOT NULL DEFAULT '',
  source_id   INTEGER NOT NULL DEFAULT 0,
  first_seen  TEXT NOT NULL,
  last_seen   TEXT NOT NULL
);
CREATE UNIQUE INDEX addresses_key ON addresses(device_id, ip, source_id);
CREATE INDEX addresses_user ON addresses(user_id, ip);

-- What happened: new | online | offline | ip | merged | wan_ip | source_failed | source_ok | renamed.
CREATE TABLE events (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id   INTEGER REFERENCES devices(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL,
  at          TEXT NOT NULL,
  text        TEXT NOT NULL DEFAULT '',
  ip          TEXT NOT NULL DEFAULT '',
  source_id   INTEGER NOT NULL DEFAULT 0,
  alerted     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX events_user_at ON events(user_id, at);
CREATE INDEX events_device ON events(device_id, at);

-- Online history: per device and hour, a bitmask of the twelve five-minute
-- slots it was seen in.
CREATE TABLE presence (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  hour       TEXT NOT NULL,                        -- local "2026-10-25T17"
  slots      INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (device_id, hour)
);
CREATE INDEX presence_user_hour ON presence(user_id, hour);

-- One run of a source.
CREATE TABLE scans (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_id   INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
  at          TEXT NOT NULL,
  found       INTEGER NOT NULL DEFAULT 0,
  new         INTEGER NOT NULL DEFAULT 0,
  ms          INTEGER NOT NULL DEFAULT 0,
  error       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX scans_user_at ON scans(user_id, at);
CREATE INDEX scans_source ON scans(source_id, at);
