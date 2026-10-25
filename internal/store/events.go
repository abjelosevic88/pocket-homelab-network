package store

import (
	"context"
	"math/bits"
	"strings"
	"time"
)

// AddEvent records something that happened.
func (s *Store) AddEvent(ctx context.Context, uid int64, e *Event) error {
	if e.At == "" {
		e.At = Now()
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO events (user_id, device_id, kind, at, text, ip, source_id, alerted) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, nullI(e.DeviceID), e.Kind, e.At, e.Text, e.IP, e.SourceID, e.Alerted)
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	return nil
}

// MarkAlerted flags events whose alert went out.
func (s *Store) MarkAlerted(ctx context.Context, uid int64, ids []int64) {
	for _, id := range ids {
		_, _ = s.DB.ExecContext(ctx, `UPDATE events SET alerted = 1 WHERE user_id = ? AND id = ?`, uid, id)
	}
}

// EventFilter narrows the activity list.
type EventFilter struct {
	DeviceID int64
	Kinds    []string
	Before   string // at < before (paging)
	Since    string
	Limit    int
}

// ListEvents lists a person's events, newest first, with the device's name.
func (s *Store) ListEvents(ctx context.Context, uid int64, f EventFilter) ([]Event, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	q := `SELECT e.id, e.device_id, e.kind, e.at, e.text, e.ip, e.source_id, e.alerted,
		COALESCE(NULLIF(d.name, ''), NULLIF(d.hostname, ''), CASE WHEN d.vendor <> '' THEN d.vendor || ' device' END, CASE WHEN d.last_ip <> '' THEN 'Device at ' || d.last_ip END, ''),
		COALESCE(NULLIF(d.kind, ''), d.kind_guess, '')
		FROM events e LEFT JOIN devices d ON d.id = e.device_id WHERE e.user_id = ?`
	args := []any{uid}
	if f.DeviceID > 0 {
		q += ` AND e.device_id = ?`
		args = append(args, f.DeviceID)
	}
	if len(f.Kinds) > 0 {
		q += ` AND e.kind IN (?` + strings.Repeat(`, ?`, len(f.Kinds)-1) + `)`
		for _, k := range f.Kinds {
			args = append(args, k)
		}
	}
	if f.Before != "" {
		q += ` AND e.at < ?`
		args = append(args, f.Before)
	}
	if f.Since != "" {
		q += ` AND e.at >= ?`
		args = append(args, f.Since)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY e.at DESC, e.id DESC LIMIT ?`, append(args, f.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var dev *int64
		if err := rows.Scan(&e.ID, &dev, &e.Kind, &e.At, &e.Text, &e.IP, &e.SourceID, &e.Alerted, &e.DeviceName, &e.DeviceKind); err != nil {
			return nil, err
		}
		e.DeviceID = dev
		out = append(out, e)
	}
	return out, rows.Err()
}

// HourKey is the local hour a time falls in ("2026-10-25T17").
func HourKey(t time.Time) string { return t.Local().Format("2006-01-02T15") }

// MarkPresence records that a device was seen at t (its five-minute slot in the hour).
func (s *Store) MarkPresence(ctx context.Context, uid, deviceID int64, t time.Time) error {
	slot := uint(t.Local().Minute() / 5)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO presence (user_id, device_id, hour, slots) VALUES (?, ?, ?, ?) ON CONFLICT(device_id, hour) DO UPDATE SET slots = presence.slots | excluded.slots`,
		uid, deviceID, HourKey(t), 1<<slot)
	return err
}

// PresenceHour is one hour of a device's online history.
type PresenceHour struct {
	Hour    string `json:"hour"`
	Minutes int    `json:"minutes"` // minutes seen (five-minute slots × 5)
}

// Presence lists a device's online hours since an hour key.
func (s *Store) Presence(ctx context.Context, uid, deviceID int64, from string) ([]PresenceHour, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT hour, slots FROM presence WHERE user_id = ? AND device_id = ? AND hour >= ? ORDER BY hour`, uid, deviceID, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PresenceHour{}
	for rows.Next() {
		var h PresenceHour
		var slots uint
		if err := rows.Scan(&h.Hour, &slots); err != nil {
			return nil, err
		}
		h.Minutes = bits.OnesCount(slots) * 5
		out = append(out, h)
	}
	return out, rows.Err()
}

// OnlineByHour counts, per hour since from, how many devices were seen.
func (s *Store) OnlineByHour(ctx context.Context, uid int64, from string) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT hour, COUNT(1) FROM presence WHERE user_id = ? AND hour >= ? GROUP BY hour`, uid, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var h string
		var n int
		if err := rows.Scan(&h, &n); err != nil {
			return nil, err
		}
		out[h] = n
	}
	return out, rows.Err()
}

// Prune drops a person's history older than the cut-off (events, online
// history, runs); devices themselves stay.
func (s *Store) Prune(ctx context.Context, uid int64, days int) {
	if days <= 0 {
		return
	}
	cut := time.Now().AddDate(0, 0, -days)
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM events WHERE user_id = ? AND at < ?`, uid, cut.UTC().Format(time.RFC3339Nano))
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM presence WHERE user_id = ? AND hour < ?`, uid, HourKey(cut))
	scanCut := time.Now().AddDate(0, 0, -min(days, 14))
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM scans WHERE user_id = ? AND at < ?`, uid, scanCut.UTC().Format(time.RFC3339Nano))
}
