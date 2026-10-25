package store

import (
	"context"
	"encoding/json"
	"strings"
)

const sourceCols = `id, user_id, kind, name, network, config, secret, token_hash, token_hint, enabled, interval_s, presence, last_run, last_ok, last_error, last_count, last_ms, sort, created_at, updated_at`

func scanSource(row interface{ Scan(...any) error }) (Source, error) {
	var s Source
	var cfg string
	err := row.Scan(&s.ID, &s.UserID, &s.Kind, &s.Name, &s.Network, &cfg, &s.Secret, &s.TokenHash, &s.TokenHint, &s.Enabled, &s.IntervalS, &s.Presence,
		&s.LastRun, &s.LastOK, &s.LastError, &s.LastCount, &s.LastMS, &s.Sort, &s.CreatedAt, &s.UpdatedAt)
	if !json.Valid([]byte(cfg)) {
		cfg = "{}"
	}
	s.Config = json.RawMessage(cfg)
	s.HasSecret = s.Secret != ""
	return s, err
}

func (s *Store) sources(ctx context.Context, where string, args ...any) ([]Source, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+sourceCols+` FROM sources WHERE `+where+` ORDER BY sort, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// ListSources lists one person's sources.
func (s *Store) ListSources(ctx context.Context, uid int64) ([]Source, error) {
	return s.sources(ctx, `user_id = ?`, uid)
}

// EnabledSources lists every person's switched-on sources (the scheduler).
func (s *Store) EnabledSources(ctx context.Context) ([]Source, error) {
	return s.sources(ctx, `enabled = 1`)
}

// GetSource loads one of a person's sources.
func (s *Store) GetSource(ctx context.Context, uid, id int64) (*Source, error) {
	src, err := scanSource(s.DB.QueryRowContext(ctx, `SELECT `+sourceCols+` FROM sources WHERE user_id = ? AND id = ?`, uid, id))
	if err != nil {
		return nil, notFound(err)
	}
	return &src, nil
}

// SourceByToken finds the agent source a token belongs to.
func (s *Store) SourceByToken(ctx context.Context, plain string) (*Source, error) {
	if plain == "" {
		return nil, ErrNotFound
	}
	src, err := scanSource(s.DB.QueryRowContext(ctx, `SELECT `+sourceCols+` FROM sources WHERE token_hash = ? AND kind = 'agent'`, HashToken(plain)))
	if err != nil {
		return nil, notFound(err)
	}
	return &src, nil
}

// HasSourceKind reports whether a person has a source of a kind.
func (s *Store) HasSourceKind(ctx context.Context, uid int64, kind string) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM sources WHERE user_id = ? AND kind = ?`, uid, kind).Scan(&n)
	return n > 0
}

// CreateSource adds a source.
func (s *Store) CreateSource(ctx context.Context, src *Source) error {
	if len(src.Config) == 0 {
		src.Config = json.RawMessage("{}")
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO sources (user_id, kind, name, network, config, secret, token_hash, token_hint, enabled, interval_s, presence, sort)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(sort), 0) + 1 FROM sources WHERE user_id = ?))`,
		src.UserID, src.Kind, src.Name, src.Network, string(src.Config), src.Secret, src.TokenHash, src.TokenHint, src.Enabled, src.IntervalS, src.Presence, src.UserID)
	if err != nil {
		return conflict(err)
	}
	src.ID, _ = res.LastInsertId()
	return nil
}

// UpdateSource saves a source's settings.
func (s *Store) UpdateSource(ctx context.Context, src *Source) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE sources SET name = ?, network = ?, config = ?, secret = ?, token_hash = ?, token_hint = ?, enabled = ?, interval_s = ?, presence = ?, sort = ?, updated_at = ? WHERE user_id = ? AND id = ?`,
		src.Name, src.Network, string(src.Config), src.Secret, src.TokenHash, src.TokenHint, src.Enabled, src.IntervalS, src.Presence, src.Sort, Now(), src.UserID, src.ID)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSource removes a source (its devices stay).
func (s *Store) DeleteSource(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM sources WHERE user_id = ? AND id = ?`, uid, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordRun stores how a source's last run went, and the run itself.
func (s *Store) RecordRun(ctx context.Context, src *Source, at string, found, fresh, ms int, errText string) error {
	errText = strings.TrimSpace(errText)
	q := `UPDATE sources SET last_run = ?, last_error = ?, last_count = ?, last_ms = ?, last_ok = CASE WHEN ? = '' THEN ? ELSE last_ok END WHERE id = ?`
	if _, err := s.DB.ExecContext(ctx, q, at, errText, found, ms, errText, at, src.ID); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO scans (user_id, source_id, at, found, new, ms, error) VALUES (?, ?, ?, ?, ?, ?, ?)`, src.UserID, src.ID, at, found, fresh, ms, errText)
	return err
}

// ListScans lists a person's recent runs (of one source, when sourceID > 0).
func (s *Store) ListScans(ctx context.Context, uid, sourceID int64, limit int) ([]Scan, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	q := `SELECT id, source_id, at, found, new, ms, error FROM scans WHERE user_id = ?`
	args := []any{uid}
	if sourceID > 0 {
		q += ` AND source_id = ?`
		args = append(args, sourceID)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY at DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Scan{}
	for rows.Next() {
		var sc Scan
		if err := rows.Scan(&sc.ID, &sc.SourceID, &sc.At, &sc.Found, &sc.New, &sc.MS, &sc.Error); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}
