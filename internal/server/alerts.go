package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-network/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-network/internal/store"
)

// ---- going offline ----

// checkOffline turns devices nobody has seen for the person's offline time
// (Settings → offline_min) offline. A device is only judged by sources that
// are working: when the scan that sees it is failing, nobody knows whether it
// is there, so it keeps its state instead of setting off a wave of alerts.
func (s *Server) checkOffline(ctx context.Context) {
	if s.cfg.DemoData {
		return
	}
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	for _, uid := range ids {
		st, _ := s.store.GetSettings(ctx, uid)
		srcs, err := s.store.ListSources(ctx, uid)
		if err != nil {
			continue
		}
		healthy := map[int64]bool{}
		for _, src := range srcs {
			last := parseTime(src.LastOK)
			if src.Enabled && src.Presence && src.LastError == "" && !last.IsZero() && now.Sub(last) < 2*time.Duration(max(src.IntervalS, 60))*time.Second+2*time.Minute {
				healthy[src.ID] = true
			}
		}
		if len(healthy) == 0 {
			continue
		}
		grace := time.Duration(max(st.OfflineMin, 2)) * time.Minute
		online := true
		devs, err := s.store.ListDevices(ctx, uid, store.DeviceFilter{Online: &online})
		if err != nil {
			continue
		}
		var alerts []int64
		for _, d := range devs {
			last := parseTime(d.LastSeen)
			if now.Sub(last) < grace || !s.judgedBy(ctx, uid, d.ID, healthy) {
				continue
			}
			d.Online = false
			d.StatusAt = fmtTime(now)
			if err := s.store.SaveDevice(ctx, &d); err != nil {
				continue
			}
			e := &store.Event{DeviceID: &d.ID, Kind: "offline", At: fmtTime(now), Text: "Went offline (last seen " + last.Local().Format("15:04") + ")", IP: d.LastIP}
			if s.store.AddEvent(ctx, uid, e) != nil {
				continue
			}
			if d.Watch && !d.Muted && d.Review != "guest" {
				if st.NotifyOffline {
					alerts = append(alerts, e.ID)
				}
				s.publish(ctx, uid, "network.device_offline", fmt.Sprintf("device:%d", d.ID), map[string]any{
					"device_id": d.ID, "name": d.Label(), "kind": firstNonEmpty(d.Kind, d.KindGuess), "ip": d.LastIP, "last_seen": d.LastSeen, "url": s.deviceURL(d.ID)})
			}
		}
		s.queueAlert(ctx, uid, alerts...)
	}
}

// judgedBy reports whether a working source has ever seen the device.
func (s *Server) judgedBy(ctx context.Context, uid, deviceID int64, healthy map[int64]bool) bool {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT DISTINCT source_id FROM addresses WHERE user_id = ? AND device_id = ?`, uid, deviceID)
	if err != nil {
		return false
	}
	var srcs []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			srcs = append(srcs, id)
		}
	}
	rows.Close()
	for _, id := range srcs {
		if healthy[id] {
			return true
		}
	}
	return false
}

// ---- sending ----

// queueAlert gathers events for a person's next message; it goes out at
// once, or after the digest wait, and never during quiet hours.
func (s *Server) queueAlert(ctx context.Context, uid int64, eventIDs ...int64) {
	if len(eventIDs) == 0 {
		return
	}
	s.digestMu.Lock()
	s.digest[uid] = append(s.digest[uid], eventIDs...)
	s.digestMu.Unlock()
	s.flushUser(ctx, uid, false)
}

// flushDigests sends what waited long enough (every minute).
func (s *Server) flushDigests(ctx context.Context, force bool) {
	s.digestMu.Lock()
	var uids []int64
	for uid := range s.digest {
		uids = append(uids, uid)
	}
	s.digestMu.Unlock()
	for _, uid := range uids {
		s.flushUser(ctx, uid, force)
	}
}

func inQuiet(from, to string, t time.Time) bool {
	if !hhmm.MatchString(from) || !hhmm.MatchString(to) || from == to {
		return false
	}
	now := t.Format("15:04")
	if from < to {
		return now >= from && now < to
	}
	return now >= from || now < to
}

func (s *Server) flushUser(ctx context.Context, uid int64, force bool) {
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil {
		return
	}
	s.digestMu.Lock()
	pending := s.digest[uid]
	s.digestMu.Unlock()
	if len(pending) == 0 {
		return
	}
	if !force && inQuiet(st.QuietFrom, st.QuietTo, time.Now()) {
		return
	}
	evs, err := s.store.ListEvents(ctx, uid, store.EventFilter{Since: fmtTime(time.Now().Add(-48 * time.Hour)), Limit: 1000})
	if err != nil {
		return
	}
	want := map[int64]bool{}
	for _, id := range pending {
		want[id] = true
	}
	var list []store.Event
	oldest := time.Now()
	for _, e := range evs {
		if want[e.ID] && !e.Alerted {
			list = append(list, e)
			if t := parseTime(e.At); t.Before(oldest) {
				oldest = t
			}
		}
	}
	if !force && st.NotifyDigestMin > 0 && time.Since(oldest) < time.Duration(st.NotifyDigestMin)*time.Minute {
		return
	}
	s.digestMu.Lock()
	delete(s.digest, uid)
	s.digestMu.Unlock()
	if len(list) == 0 || st.NotifyURL == "" {
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].At < list[j].At })
	msg := alertMessage(list, s.cfg.BaseURL)
	if err := notify.New(st.NotifyURL, st.NotifyFormat).Send(ctx, msg); err != nil {
		s.log.Warn("alert not sent", "user", uid, "err", err)
		return
	}
	ids := make([]int64, 0, len(list))
	for _, e := range list {
		ids = append(ids, e.ID)
	}
	s.store.MarkAlerted(ctx, uid, ids)
}

// alertMessage words one message for one or more events.
func alertMessage(list []store.Event, base string) notify.Message {
	line := func(e store.Event) string {
		switch e.Kind {
		case "new":
			if e.IP != "" {
				return fmt.Sprintf("New device: %s (%s)", e.DeviceName, e.IP)
			}
			return "New device: " + e.DeviceName
		case "offline":
			return e.DeviceName + " went offline"
		case "online":
			return e.DeviceName + " is back online"
		}
		return e.Text
	}
	link := base
	if len(list) == 1 && list[0].DeviceID != nil && base != "" {
		link = fmt.Sprintf("%s/devices/%d", base, *list[0].DeviceID)
	} else if base != "" {
		link = base + "/activity"
	}
	prio, tags := "info", []string{"globe_with_meridians"}
	for _, e := range list {
		switch e.Kind {
		case "offline", "source_failed":
			prio, tags = "warning", []string{"warning"}
		case "new":
			if prio == "info" {
				tags = []string{"new"}
			}
		}
	}
	if len(list) == 1 {
		e := list[0]
		body := e.Text
		switch e.Kind {
		case "new":
			body = "Something you have not seen before joined your network. Open Pocket Network to say whether it is yours."
		case "offline":
			body = e.Text + "."
		case "online":
			body = e.DeviceName + " is reachable again."
		}
		return notify.Message{Title: line(e), Body: body, Priority: prio, Tags: tags, Link: link}
	}
	var b strings.Builder
	for i, e := range list {
		if i == 12 {
			fmt.Fprintf(&b, "… and %d more\n", len(list)-12)
			break
		}
		b.WriteString(line(e) + "\n")
	}
	return notify.Message{Title: fmt.Sprintf("%d changes on your network", len(list)), Body: strings.TrimSpace(b.String()), Priority: prio, Tags: tags, Link: link}
}

// ---- the public internet address ----

// checkWAN asks for this server's public address once an hour, for the
// people who follow it (admins with a scan of this server's network).
func (s *Server) checkWAN(ctx context.Context) {
	if s.cfg.WANCheckURL == "" || !s.cfg.LocalScan || s.cfg.DemoData {
		return
	}
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	var who []int64
	for _, uid := range ids {
		st, _ := s.store.GetSettings(ctx, uid)
		if u, err := s.store.GetUser(ctx, uid); err == nil && u.Role == "admin" && st.TrackWAN && s.store.HasSourceKind(ctx, uid, "scan") {
			who = append(who, uid)
		}
	}
	if len(who) == 0 {
		return
	}
	ip, err := s.publicIP(ctx)
	if err != nil {
		s.log.Debug("public address check failed", "err", err)
		return
	}
	for _, uid := range who {
		key := fmt.Sprintf("wan:%d", uid)
		old, _, _ := s.store.CacheGet(ctx, key)
		if old == ip {
			continue
		}
		_ = s.store.CachePut(ctx, key, ip)
		_ = s.store.CachePut(ctx, key+":since", store.Now())
		if old == "" {
			continue
		}
		st, _ := s.store.GetSettings(ctx, uid)
		e := &store.Event{Kind: "wan_ip", Text: "Your internet address changed from " + old + " to " + ip, IP: ip}
		if s.store.AddEvent(ctx, uid, e) == nil && st.NotifyWAN {
			s.queueAlert(ctx, uid, e.ID)
		}
		s.publish(ctx, uid, "network.wan_ip_changed", "wan", map[string]any{"ip": ip, "previous": old})
	}
}

func (s *Server) publicIP(ctx context.Context) (string, error) {
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet, s.cfg.WANCheckURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", s.cfg.UserAgent)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	text := strings.TrimSpace(string(b))
	for _, line := range strings.Split(text, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "ip="); ok {
			text = v
			break
		}
	}
	if net.ParseIP(text) == nil {
		return "", fmt.Errorf("no address in the answer from %s", s.cfg.WANCheckURL)
	}
	return text, nil
}
