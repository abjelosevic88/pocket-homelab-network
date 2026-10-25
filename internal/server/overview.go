package server

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-network/internal/store"
)

// ---- the overview, the sidebar counts and the day summary ----

type networkCount struct {
	Name   string `json:"name"`
	Online int    `json:"online"`
	Total  int    `json:"total"`
}

type homePerson struct {
	Owner   string         `json:"owner"`
	Devices []store.Device `json:"devices"`
	Since   string         `json:"since"`
}

func kindOf(d store.Device) string { return firstNonEmpty(d.Kind, d.KindGuess) }

// networksOf counts devices per network they were last seen on.
func networksOf(devs []store.Device) []networkCount {
	by := map[string]*networkCount{}
	for _, d := range devs {
		n := d.LastNetwork
		if n == "" {
			continue
		}
		c := by[n]
		if c == nil {
			c = &networkCount{Name: n}
			by[n] = c
		}
		c.Total++
		if d.Online {
			c.Online++
		}
	}
	out := make([]networkCount, 0, len(by))
	for _, c := range by {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Total > out[j].Total || out[i].Total == out[j].Total && out[i].Name < out[j].Name
	})
	return out
}

// athome is who has a phone or watch online on a local network (not just over Tailscale).
func athome(devs []store.Device) []homePerson {
	by := map[string]*homePerson{}
	for _, d := range devs {
		k := kindOf(d)
		if !d.Online || d.Owner == "" || d.Review == "guest" || !layer2(d.LastSource) || (k != "phone" && k != "watch") {
			continue
		}
		p := by[d.Owner]
		if p == nil {
			p = &homePerson{Owner: d.Owner, Since: d.StatusAt}
			by[d.Owner] = p
		}
		p.Devices = append(p.Devices, d)
		if d.StatusAt != "" && (p.Since == "" || d.StatusAt < p.Since) {
			p.Since = d.StatusAt
		}
	}
	out := make([]homePerson, 0, len(by))
	for _, p := range by {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Owner < out[j].Owner })
	return out
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	devs, err := s.store.ListDevices(ctx, uid, store.DeviceFilter{})
	if err != nil {
		s.fail(w, err, "dashboard")
		return
	}
	srcs, _ := s.store.ListSources(ctx, uid)
	failing := 0
	for _, src := range srcs {
		if src.Enabled && src.LastError != "" {
			failing++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"counts": s.store.CountDevices(ctx, uid), "networks": networksOf(devs), "home": athome(devs), "sources": len(srcs), "sources_failing": failing})
}

// handleOverview is the home page: headline numbers, what needs a look, who
// is home, devices online over the last day, recent activity, the internet
// address and how the sources are doing.
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	devs, err := s.store.ListDevices(ctx, uid, store.DeviceFilter{})
	if err != nil {
		s.fail(w, err, "overview")
		return
	}
	var down, fresh, recent []store.Device
	for _, d := range devs {
		if d.Watch && !d.Online && d.Review != "guest" {
			down = append(down, d)
		}
		if d.Review == "new" {
			fresh = append(fresh, d)
		}
		if d.Online {
			recent = append(recent, d)
		}
	}
	sort.Slice(fresh, func(i, j int) bool { return fresh[i].FirstSeen > fresh[j].FirstSeen })
	if len(fresh) > 5 {
		fresh = fresh[:5]
	}
	now := time.Now()
	from := now.Add(-23 * time.Hour)
	byHour, _ := s.store.OnlineByHour(ctx, uid, store.HourKey(from))
	type hourCount struct {
		Hour  string `json:"hour"`
		Count int    `json:"count"`
	}
	hours := make([]hourCount, 0, 24)
	for t := from; !t.After(now); t = t.Add(time.Hour) {
		k := store.HourKey(t)
		hours = append(hours, hourCount{Hour: k, Count: byHour[k]})
	}
	events, _ := s.store.ListEvents(ctx, uid, store.EventFilter{Limit: 12})
	srcs, _ := s.store.ListSources(ctx, uid)
	views := make([]sourceView, 0, len(srcs))
	for _, src := range srcs {
		views = append(views, s.sourceView(src))
	}
	wan := map[string]any{}
	if ip, _, ok := s.store.CacheGet(ctx, fmt.Sprintf("wan:%d", uid)); ok {
		since, _, _ := s.store.CacheGet(ctx, fmt.Sprintf("wan:%d:since", uid))
		wan = map[string]any{"ip": ip, "since": since}
	}
	if down == nil {
		down = []store.Device{}
	}
	if fresh == nil {
		fresh = []store.Device{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"counts": s.store.CountDevices(ctx, uid), "down": down, "fresh": fresh, "home": athome(devs), "networks": networksOf(devs),
		"hours": hours, "events": events, "sources": views, "wan": wan, "online_now": len(recent),
	})
}

// handleSummary is one day for Pocket Day and the AI: new devices, what went
// down and came back, the internet address changing.
func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	date := chi.URLParam(r, "date")
	day, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "date is YYYY-MM-DD")
		return
	}
	evs, err := s.store.ListEvents(ctx, uid, store.EventFilter{Since: fmtTime(day), Before: fmtTime(day.AddDate(0, 0, 1)), Limit: 1000})
	if err != nil {
		s.fail(w, err, "summary")
		return
	}
	watched := map[int64]bool{}
	if devs, err := s.store.ListDevices(ctx, uid, store.DeviceFilter{Watch: true}); err == nil {
		for _, d := range devs {
			watched[d.ID] = true
		}
	}
	type item struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Kind   string `json:"kind"`
		At     string `json:"at"`
		URL    string `json:"url,omitempty"`
		Device int64  `json:"device_id,omitempty"`
	}
	items := []item{}
	counts := map[string]int{}
	for _, e := range evs {
		keep := false
		switch e.Kind {
		case "new", "wan_ip", "source_failed":
			keep = true
		case "offline", "online":
			keep = e.DeviceID != nil && watched[*e.DeviceID]
		}
		if !keep {
			continue
		}
		counts[e.Kind]++
		it := item{ID: fmt.Sprintf("network-%d", e.ID), Title: e.Text, Kind: e.Kind, At: e.At}
		if e.DeviceID != nil {
			it.Device = *e.DeviceID
			it.URL = s.deviceURL(*e.DeviceID)
			switch e.Kind {
			case "offline":
				it.Title = e.DeviceName + " went offline"
			case "online":
				it.Title = e.DeviceName + " came back online"
			}
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].At < items[j].At })
	var parts []string
	if n := counts["new"]; n > 0 {
		parts = append(parts, plural(n, "new device", "new devices"))
	}
	if n := counts["offline"]; n > 0 {
		parts = append(parts, plural(n, "watched device went offline", "watched devices went offline"))
	}
	if counts["wan_ip"] > 0 {
		parts = append(parts, "the internet address changed")
	}
	text := "A quiet day on the network."
	if len(parts) > 0 {
		text = strings.ToUpper(strings.Join(parts, ", ")[:1]) + strings.Join(parts, ", ")[1:] + "."
	}
	writeJSON(w, http.StatusOK, map[string]any{"date": date, "text": text, "counts": counts, "items": items, "devices": s.store.CountDevices(ctx, uid)})
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
