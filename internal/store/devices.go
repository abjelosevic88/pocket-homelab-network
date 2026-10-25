package store

import (
	"context"
	"strings"
)

const deviceCols = `id, user_id, name, hostname, owner, kind, kind_guess, vendor, model, os, location, notes, link_url, review, watch, muted, pinned, online, status_at,
	first_seen, last_seen, last_ip, last_mac, last_network, last_source, random_mac, dns_queries, dns_blocked, ports, inventory_id, inventory_name, monitoring_id, monitoring_name, source, created_at, updated_at`

func scanDevice(row interface{ Scan(...any) error }) (Device, error) {
	var d Device
	err := row.Scan(&d.ID, &d.UserID, &d.Name, &d.Hostname, &d.Owner, &d.Kind, &d.KindGuess, &d.Vendor, &d.Model, &d.OS, &d.Location, &d.Notes, &d.LinkURL,
		&d.Review, &d.Watch, &d.Muted, &d.Pinned, &d.Online, &d.StatusAt, &d.FirstSeen, &d.LastSeen, &d.LastIP, &d.LastMAC, &d.LastNetwork, &d.LastSource,
		&d.RandomMAC, &d.DNSQueries, &d.DNSBlocked, &d.Ports, &d.InventoryID, &d.InventoryName, &d.MonitoringID, &d.MonitoringName, &d.Source, &d.CreatedAt, &d.UpdatedAt)
	return d, err
}

// DeviceFilter narrows a device list.
type DeviceFilter struct {
	Review string // new | known | guest
	Online *bool
	Watch  bool
	Query  string
	Owner  string
	Kind   string
}

// ListDevices lists one person's devices, pinned first, then by name.
func (s *Store) ListDevices(ctx context.Context, uid int64, f DeviceFilter) ([]Device, error) {
	q := `SELECT ` + deviceCols + ` FROM devices WHERE user_id = ?`
	args := []any{uid}
	if f.Review != "" {
		q += ` AND review = ?`
		args = append(args, f.Review)
	}
	if f.Online != nil {
		q += ` AND online = ?`
		args = append(args, *f.Online)
	}
	if f.Watch {
		q += ` AND watch = 1`
	}
	if f.Owner != "" {
		q += ` AND owner = ?`
		args = append(args, f.Owner)
	}
	if f.Kind != "" {
		q += ` AND (kind = ? OR (kind = '' AND kind_guess = ?))`
		args = append(args, f.Kind, f.Kind)
	}
	if t := strings.TrimSpace(f.Query); t != "" {
		like := "%" + strings.ToLower(t) + "%"
		q += ` AND (lower(name) LIKE ? OR lower(hostname) LIKE ? OR lower(vendor) LIKE ? OR last_ip LIKE ? OR lower(last_mac) LIKE ? OR lower(owner) LIKE ? OR lower(location) LIKE ? OR lower(notes) LIKE ?
			OR id IN (SELECT device_id FROM device_ids WHERE user_id = ? AND lower(value) LIKE ?) OR id IN (SELECT device_id FROM addresses WHERE user_id = ? AND ip LIKE ?))`
		args = append(args, like, like, like, like, like, like, like, like, uid, like, uid, like)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY pinned DESC, CASE WHEN name = '' THEN 1 ELSE 0 END, lower(COALESCE(NULLIF(name, ''), NULLIF(hostname, ''), vendor)), id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetDevice loads one of a person's devices.
func (s *Store) GetDevice(ctx context.Context, uid, id int64) (*Device, error) {
	d, err := scanDevice(s.DB.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE user_id = ? AND id = ?`, uid, id))
	if err != nil {
		return nil, notFound(err)
	}
	return &d, nil
}

// CreateDevice adds a device.
func (s *Store) CreateDevice(ctx context.Context, d *Device) error {
	if d.Review == "" {
		d.Review = "new"
	}
	if d.Source == "" {
		d.Source = "scan"
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO devices (user_id, name, hostname, owner, kind, kind_guess, vendor, model, os, location, notes, link_url, review, watch, muted, pinned, online, status_at,
		first_seen, last_seen, last_ip, last_mac, last_network, last_source, random_mac, dns_queries, dns_blocked, ports, inventory_id, inventory_name, monitoring_id, monitoring_name, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.UserID, d.Name, d.Hostname, d.Owner, d.Kind, d.KindGuess, d.Vendor, d.Model, d.OS, d.Location, d.Notes, d.LinkURL, d.Review, d.Watch, d.Muted, d.Pinned, d.Online, d.StatusAt,
		d.FirstSeen, d.LastSeen, d.LastIP, d.LastMAC, d.LastNetwork, d.LastSource, d.RandomMAC, d.DNSQueries, d.DNSBlocked, d.Ports, d.InventoryID, d.InventoryName, d.MonitoringID, d.MonitoringName, d.Source)
	if err != nil {
		return err
	}
	d.ID, _ = res.LastInsertId()
	d.CreatedAt, d.UpdatedAt = Now(), Now()
	return nil
}

// SaveDevice writes every field of a device.
func (s *Store) SaveDevice(ctx context.Context, d *Device) error {
	d.UpdatedAt = Now()
	res, err := s.DB.ExecContext(ctx, `UPDATE devices SET name = ?, hostname = ?, owner = ?, kind = ?, kind_guess = ?, vendor = ?, model = ?, os = ?, location = ?, notes = ?, link_url = ?, review = ?,
		watch = ?, muted = ?, pinned = ?, online = ?, status_at = ?, first_seen = ?, last_seen = ?, last_ip = ?, last_mac = ?, last_network = ?, last_source = ?, random_mac = ?,
		dns_queries = ?, dns_blocked = ?, ports = ?, inventory_id = ?, inventory_name = ?, monitoring_id = ?, monitoring_name = ?, updated_at = ? WHERE user_id = ? AND id = ?`,
		d.Name, d.Hostname, d.Owner, d.Kind, d.KindGuess, d.Vendor, d.Model, d.OS, d.Location, d.Notes, d.LinkURL, d.Review, d.Watch, d.Muted, d.Pinned, d.Online, d.StatusAt,
		d.FirstSeen, d.LastSeen, d.LastIP, d.LastMAC, d.LastNetwork, d.LastSource, d.RandomMAC, d.DNSQueries, d.DNSBlocked, d.Ports, d.InventoryID, d.InventoryName, d.MonitoringID, d.MonitoringName, d.UpdatedAt, d.UserID, d.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteDevice forgets a device (its identifiers, addresses, events and history go with it).
func (s *Store) DeleteDevice(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM devices WHERE user_id = ? AND id = ?`, uid, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeviceByID finds the device an identifier belongs to (0 when none).
func (s *Store) DeviceByID(ctx context.Context, uid int64, kind, value string) int64 {
	var id int64
	_ = s.DB.QueryRowContext(ctx, `SELECT device_id FROM device_ids WHERE user_id = ? AND kind = ? AND value = ?`, uid, kind, value).Scan(&id)
	return id
}

// DeviceByIP is the device seen most recently at an address since a time (0 when none).
func (s *Store) DeviceByIP(ctx context.Context, uid int64, ip, since string) int64 {
	var id int64
	_ = s.DB.QueryRowContext(ctx, `SELECT device_id FROM addresses WHERE user_id = ? AND ip = ? AND last_seen >= ? ORDER BY last_seen DESC LIMIT 1`, uid, ip, since).Scan(&id)
	if id == 0 {
		_ = s.DB.QueryRowContext(ctx, `SELECT id FROM devices WHERE user_id = ? AND last_ip = ? AND last_seen >= ? ORDER BY last_seen DESC LIMIT 1`, uid, ip, since).Scan(&id)
	}
	return id
}

// AddDeviceID records (or refreshes) an identifier of a device. An identifier
// already on another device is moved to this one.
func (s *Store) AddDeviceID(ctx context.Context, uid, deviceID int64, kind, value, at string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO device_ids (user_id, device_id, kind, value, first_seen, last_seen) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, kind, value) DO UPDATE SET device_id = excluded.device_id, last_seen = CASE WHEN excluded.last_seen > device_ids.last_seen THEN excluded.last_seen ELSE device_ids.last_seen END`,
		uid, deviceID, kind, value, at, at)
	return err
}

// DeviceIDs lists a device's identifiers.
func (s *Store) DeviceIDs(ctx context.Context, uid, deviceID int64) ([]DeviceID, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, device_id, kind, value, first_seen, last_seen FROM device_ids WHERE user_id = ? AND device_id = ? ORDER BY last_seen DESC`, uid, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeviceID{}
	for rows.Next() {
		var d DeviceID
		if err := rows.Scan(&d.ID, &d.DeviceID, &d.Kind, &d.Value, &d.FirstSeen, &d.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// RemoveDeviceID drops one identifier of a device.
func (s *Store) RemoveDeviceID(ctx context.Context, uid, deviceID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM device_ids WHERE user_id = ? AND device_id = ? AND id = ?`, uid, deviceID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchAddress records that a device used an address.
func (s *Store) TouchAddress(ctx context.Context, uid, deviceID int64, ip, mac, network string, sourceID int64, at string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO addresses (user_id, device_id, ip, mac, network, source_id, first_seen, last_seen) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_id, ip, source_id) DO UPDATE SET last_seen = excluded.last_seen, mac = CASE WHEN excluded.mac <> '' THEN excluded.mac ELSE addresses.mac END, network = excluded.network`,
		uid, deviceID, ip, mac, network, sourceID, at, at)
	return err
}

// Addresses lists the addresses a device used, newest first.
func (s *Store) Addresses(ctx context.Context, uid, deviceID int64) ([]Address, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, device_id, ip, mac, network, source_id, first_seen, last_seen FROM addresses WHERE user_id = ? AND device_id = ? ORDER BY last_seen DESC LIMIT 100`, uid, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Address{}
	for rows.Next() {
		var a Address
		if err := rows.Scan(&a.ID, &a.DeviceID, &a.IP, &a.MAC, &a.Network, &a.SourceID, &a.FirstSeen, &a.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MergeDevices folds the others into target: identifiers, addresses, events
// and online history move over, then the others are deleted.
func (s *Store) MergeDevices(ctx context.Context, uid, target int64, others []int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM devices WHERE user_id = ? AND id = ?`, uid, target).Scan(&n); err != nil || n == 0 {
		return ErrNotFound
	}
	for _, o := range others {
		if o == target {
			continue
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM devices WHERE user_id = ? AND id = ?`, uid, o).Scan(&n); err != nil || n == 0 {
			return ErrNotFound
		}
		stmts := []string{
			`UPDATE device_ids SET device_id = ? WHERE user_id = ? AND device_id = ?`,
			`UPDATE OR IGNORE addresses SET device_id = ? WHERE user_id = ? AND device_id = ?`,
			`UPDATE events SET device_id = ? WHERE user_id = ? AND device_id = ?`,
		}
		for _, q := range stmts {
			if _, err := tx.ExecContext(ctx, q, target, uid, o); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO presence (user_id, device_id, hour, slots) SELECT user_id, ?, hour, slots FROM presence WHERE user_id = ? AND device_id = ? AND true
			ON CONFLICT(device_id, hour) DO UPDATE SET slots = presence.slots | excluded.slots`, target, uid, o); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE user_id = ? AND id = ?`, uid, o); err != nil {
			return err
		}
	}
	// the merged device keeps the earliest first sighting and the latest last one
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET
		first_seen = COALESCE((SELECT MIN(first_seen) FROM device_ids WHERE device_id = ? AND first_seen <> ''), first_seen),
		updated_at = ? WHERE id = ?`, target, Now(), target); err != nil {
		return err
	}
	return tx.Commit()
}

// DevicesByName lists a person's devices for which match is true.
func (s *Store) DevicesByName(ctx context.Context, uid int64, match func(d Device) bool) ([]Device, error) {
	all, err := s.ListDevices(ctx, uid, DeviceFilter{})
	if err != nil {
		return nil, err
	}
	var out []Device
	for _, d := range all {
		if match(d) {
			out = append(out, d)
		}
	}
	return out, nil
}

// DeviceCounts are the headline numbers.
type DeviceCounts struct {
	Total          int `json:"total"`
	Online         int `json:"online"`
	New            int `json:"new"`
	Guests         int `json:"guests"`
	Watched        int `json:"watched"`
	WatchedOffline int `json:"watched_offline"`
}

// CountDevices returns a person's headline numbers.
func (s *Store) CountDevices(ctx context.Context, uid int64) DeviceCounts {
	var c DeviceCounts
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1), COALESCE(SUM(online), 0), COALESCE(SUM(review = 'new'), 0), COALESCE(SUM(review = 'guest'), 0), COALESCE(SUM(watch), 0),
		COALESCE(SUM(watch = 1 AND online = 0), 0) FROM devices WHERE user_id = ?`, uid).Scan(&c.Total, &c.Online, &c.New, &c.Guests, &c.Watched, &c.WatchedOffline)
	return c
}

// Owners lists the distinct owners a person gave their devices.
func (s *Store) Owners(ctx context.Context, uid int64) ([]string, error) {
	return s.strings(ctx, `SELECT DISTINCT owner FROM devices WHERE user_id = ? AND owner <> '' ORDER BY lower(owner)`, uid)
}

// Locations lists the distinct places a person gave their devices.
func (s *Store) Locations(ctx context.Context, uid int64) ([]string, error) {
	return s.strings(ctx, `SELECT DISTINCT location FROM devices WHERE user_id = ? AND location <> '' ORDER BY lower(location)`, uid)
}

func (s *Store) strings(ctx context.Context, q string, args ...any) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ForgetGuests deletes guests not seen since the cut-off; returns how many.
func (s *Store) ForgetGuests(ctx context.Context, uid int64, before string) int {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM devices WHERE user_id = ? AND review = 'guest' AND last_seen <> '' AND last_seen < ?`, uid, before)
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}
