package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/abjelosevic88/pocket-homelab-network/internal/netscan"
	"github.com/abjelosevic88/pocket-homelab-network/internal/store"
)

// ---- demo data (DEMO_DATA=true): a household with two months of history ----
//
// Sources are made but never run (nothing is scanned in demo mode), so the
// numbers stay as seeded: a home network, a garden office behind its own
// router (an agent), the tailnet, and AdGuard Home.

type demoDevice struct {
	name, host, owner, kind, place, oui string
	private, watch                      bool
	review                              string
	network                             string // home | office | tailscale
	pattern                             string // always | evening | work | weekend | sometimes | guest | new | gone
	os, model                           string
	ip                                  int
	link                                string
	notes                               string
	dns                                 int
	firstDays                           int
	rotate                              int // a private address that changed this many times (for the review)
}

var demoDevices = []demoDevice{
	{name: "Home router", host: "fritz.box", kind: "router", place: "Hallway", oui: "04B4FE", watch: true, review: "known", network: "home", pattern: "always", ip: 1, link: "http://192.168.1.1", notes: "Fritz!Box 7590. Admin password in the vault.", dns: 2100, firstDays: 62},
	{name: "Upstairs Wi-Fi", host: "u6-lite", kind: "access_point", place: "Landing", oui: "00156D", watch: true, review: "known", network: "home", pattern: "always", ip: 2, link: "https://192.168.1.2", firstDays: 62},
	{name: "Office switch", host: "gs308e", kind: "switch", place: "Office", oui: "00095B", watch: true, review: "known", network: "home", pattern: "always", ip: 3, firstDays: 62},
	{name: "Photo NAS", host: "diskstation", kind: "nas", place: "Office", oui: "001132", watch: true, review: "known", network: "home", pattern: "always", ip: 10, link: "https://192.168.1.10:5001", notes: "Synology DS920+, 4 × 4 TB. Backs up to the cloud every night.", dns: 3800, firstDays: 62},
	{name: "Home Assistant", host: "homeassistant", kind: "server", place: "Office", oui: "2CCF67", watch: true, review: "known", network: "home", pattern: "always", ip: 11, link: "http://192.168.1.11:8123", os: "Linux", dns: 9100, firstDays: 62},
	{name: "Printer", host: "BRW3C2AF4", kind: "printer", place: "Office", oui: "3C2AF4", review: "known", network: "home", pattern: "sometimes", ip: 20, link: "http://192.168.1.20", firstDays: 62},
	{name: "Living room TV", host: "LGwebOSTV", owner: "Family", kind: "tv", place: "Living room", oui: "00A159", review: "known", network: "home", pattern: "evening", ip: 30, os: "webOS", dns: 4200, firstDays: 62},
	{name: "Kitchen speaker", host: "Sonos-Kitchen", owner: "Family", kind: "speaker", place: "Kitchen", oui: "000E58", review: "known", network: "home", pattern: "always", ip: 31, dns: 800, firstDays: 62},
	{name: "Chromecast", host: "Chromecast-Ultra", owner: "Family", kind: "tv", place: "Bedroom", oui: "001A11", review: "known", network: "home", pattern: "evening", ip: 32, dns: 600, firstDays: 40},
	{name: "Hallway plug", host: "shellyplug-s-a1b2", kind: "smart_home", place: "Hallway", oui: "10A85B", review: "known", network: "home", pattern: "always", ip: 40, dns: 300, firstDays: 62},
	{name: "Heating", host: "ecobee", kind: "smart_home", place: "Hallway", oui: "004B12", review: "known", network: "home", pattern: "always", ip: 41, dns: 450, firstDays: 62},
	{name: "Robot vacuum", host: "roborock-vacuum-s7", kind: "smart_home", place: "Kitchen", oui: "004B12", review: "known", network: "home", pattern: "always", ip: 42, dns: 220, firstDays: 50},
	{name: "Maya's iPhone", host: "Mayas-iPhone", owner: "Maya", kind: "phone", oui: "", private: true, review: "known", network: "home", pattern: "evening", ip: 50, os: "iOS", model: "iPhone16,1", dns: 5200, firstDays: 62},
	{name: "Maya's Apple Watch", host: "Mayas-Apple-Watch", owner: "Maya", kind: "watch", private: true, review: "known", network: "home", pattern: "evening", ip: 51, os: "watchOS", dns: 700, firstDays: 62},
	{name: "Maya's MacBook Air", host: "Mayas-MacBook-Air", owner: "Maya", kind: "laptop", private: true, review: "known", network: "home", pattern: "work", ip: 52, os: "macOS", model: "Mac15,12", dns: 6400, firstDays: 62},
	{name: "Family PC", host: "DESKTOP-7Q2M4", owner: "Family", kind: "computer", place: "Office", oui: "002522", review: "known", network: "home", pattern: "sometimes", ip: 53, os: "Windows", dns: 2900, firstDays: 62},
	{name: "Leo's Pixel", host: "Pixel-8", owner: "Leo", kind: "phone", private: true, review: "known", network: "home", pattern: "evening", ip: 60, os: "Android", dns: 4700, firstDays: 62},
	{name: "Leo's iPad", host: "Leos-iPad", owner: "Leo", kind: "tablet", private: true, review: "known", network: "home", pattern: "evening", ip: 61, os: "iPadOS", dns: 1900, firstDays: 62},
	{name: "PlayStation 5", host: "PS5-421", owner: "Leo", kind: "console", place: "Living room", oui: "00041F", review: "known", network: "home", pattern: "weekend", ip: 62, dns: 1300, firstDays: 62},
	{name: "Leo's work laptop", host: "LEO-THINKPAD", owner: "Leo", kind: "laptop", oui: "10C595", review: "known", network: "office", pattern: "work", ip: 21, os: "Windows", dns: 3300, firstDays: 62},
	{name: "Office router", host: "GL-MT3000", kind: "router", place: "Garden office", oui: "003192", watch: true, review: "known", network: "office", pattern: "always", ip: 1, firstDays: 62},
	{name: "Office camera", host: "camera-garden", kind: "camera", place: "Garden office", oui: "10A85B", watch: true, review: "known", network: "office", pattern: "always", ip: 30, firstDays: 30},
	{name: "Sam's phone", host: "Galaxy-S22", owner: "Guests", kind: "phone", private: true, review: "guest", network: "home", pattern: "guest", ip: 120, os: "Android", dns: 640, firstDays: 9},
	// not reviewed yet
	{host: "Galaxy-A54", private: true, review: "new", network: "home", pattern: "new", ip: 131, dns: 310, firstDays: 3, rotate: 3},
	{host: "", oui: "007147", review: "new", network: "home", pattern: "new", ip: 133, dns: 90, firstDays: 1},
	{host: "esp32-4F1A20", oui: "10A85B", review: "new", network: "home", pattern: "new", ip: 134, dns: 12, firstDays: 2},
	{host: "", private: true, review: "new", network: "home", pattern: "gone", ip: 137, firstDays: 5},
}

func (s *Server) seedDemo(ctx context.Context, owner int64) error {
	if n := s.store.CountDevices(ctx, owner).Total; n > 0 {
		return nil
	}
	rng := rand.New(rand.NewSource(42))
	now := time.Now()
	st, _ := s.store.GetSettings(ctx, owner)
	st.Owners = []string{"Maya", "Leo", "Family", "Guests"}
	st.QuietFrom, st.QuietTo = "23:00", "07:00"
	if err := s.store.SaveUserSettings(ctx, owner, st); err != nil {
		return err
	}

	mk := func(kind, name, network string, cfg store.SourceConfig, interval int, lastErr string) *store.Source {
		b, _ := json.Marshal(cfg)
		src := &store.Source{UserID: owner, Kind: kind, Name: name, Network: network, Config: b, Enabled: true, Presence: true, IntervalS: interval}
		if kind == "agent" {
			src.TokenHash, src.TokenHint = store.HashToken("pna_demo-"+store.RandomToken(8)), "pna_demo"
		}
		_ = s.store.CreateSource(ctx, src)
		at := now.Add(-time.Duration(rng.Intn(interval)) * time.Second)
		_ = s.store.RecordRun(ctx, src, fmtTime(at), 20+rng.Intn(6), 0, 1800+rng.Intn(2500), lastErr)
		return src
	}
	home := mk("scan", "This server's network", "Home network", store.SourceConfig{Interface: "eth0", Subnet: "192.168.1.0/24", Names: boolp(true)}, 300, "")
	office := mk("agent", "Garden office", "Garden office", store.SourceConfig{}, 300, "")
	ts := mk("tailscale", "Tailscale", "Tailscale", store.SourceConfig{Socket: "/var/run/tailscale/tailscaled.sock"}, 120, "")
	mk("adguard", "AdGuard Home", "", store.SourceConfig{URL: "http://192.168.1.11:3000", Username: "admin"}, 300, "")

	nets := map[string]struct {
		label, prefix string
		src           int64
	}{"home": {"Home network", "192.168.1.", home.ID}, "office": {"Garden office", "10.0.0.", office.ID}}

	online := func(pattern string, t time.Time, d int) bool {
		h, wd := t.Hour(), t.Weekday()
		weekend := wd == time.Saturday || wd == time.Sunday
		switch pattern {
		case "always":
			return true
		case "evening":
			return h >= 17 || h < 8 || (weekend && h >= 9)
		case "work":
			return !weekend && h >= 8 && h < 18
		case "weekend":
			return weekend && h >= 10 && h < 23 || h >= 19 && h < 22 && d%3 == 0
		case "sometimes":
			return h >= 9 && h < 21 && (d*7+h)%5 == 0
		case "guest":
			return d >= 7 && d <= 8 && h >= 12
		case "new":
			return h >= 8 && h < 23
		case "gone":
			return d >= 4 && d <= 5 && h >= 10 && h < 15
		}
		return false
	}

	// rows are gathered and written in one transaction at the end (the pool
	// has one connection: no store call may run while a transaction is open)
	type row struct {
		q    string
		args []any
	}
	var rows []row
	exec := func(q string, args ...any) { rows = append(rows, row{q, args}) }
	presence := func(devID int64, hour string, slots int) {
		exec(`INSERT INTO presence (user_id, device_id, hour, slots) VALUES (?, ?, ?, ?) ON CONFLICT(device_id, hour) DO UPDATE SET slots = presence.slots | excluded.slots`, owner, devID, hour, slots)
	}
	event := func(devID int64, kind string, at time.Time, text, ip string, srcID int64) {
		var dp any
		if devID > 0 {
			dp = devID
		}
		exec(`INSERT INTO events (user_id, device_id, kind, at, text, ip, source_id, alerted) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`, owner, dp, kind, fmtTime(at), text, ip, srcID)
	}
	randMAC := func(oui string, private bool) string {
		b := make([]byte, 6)
		for i := range b {
			b[i] = byte(rng.Intn(256))
		}
		if private || oui == "" {
			b[0] = b[0]&0xfc | 0x02
		} else {
			fmt.Sscanf(oui, "%02X%02X%02X", &b[0], &b[1], &b[2])
		}
		return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5])
	}

	start := now.Add(-62 * 24 * time.Hour).Truncate(time.Hour)
	nasOutage := now.Add(-12 * 24 * time.Hour).Truncate(time.Hour).Add(3 * time.Hour)
	for _, dd := range demoDevices {
		n := nets[dd.network]
		ip := fmt.Sprintf("%s%d", n.prefix, dd.ip)
		first := now.Add(-time.Duration(dd.firstDays)*24*time.Hour + time.Duration(rng.Intn(600))*time.Minute)
		mac := randMAC(dd.oui, dd.private)
		dev := &store.Device{UserID: owner, Name: dd.name, Hostname: dd.host, Owner: dd.owner, Kind: dd.kind, Location: dd.place, Notes: dd.notes, LinkURL: dd.link,
			Review: dd.review, Watch: dd.watch, OS: dd.os, Model: dd.model, Source: "scan", FirstSeen: fmtTime(first), LastIP: ip, LastMAC: mac, RandomMAC: netscan.PrivateMAC(mac),
			LastNetwork: n.label, LastSource: map[string]string{"home": "scan", "office": "agent"}[dd.network], DNSQueries: dd.dns, DNSBlocked: dd.dns * (4 + rng.Intn(14)) / 100}
		dev.Vendor = netscan.Vendor(mac)
		dev.KindGuess = netscan.GuessKind(dd.host, dev.Vendor, dd.model, dd.os)
		if dd.name == "Photo NAS" || dd.name == "Home Assistant" {
			dev.Pinned = true
		}
		if err := s.store.CreateDevice(ctx, dev); err != nil {
			return err
		}
		_ = s.store.AddDeviceID(ctx, owner, dev.ID, "mac", mac, "")
		if dd.os == "iOS" || dd.os == "macOS" || dd.os == "Android" && dd.owner == "Leo" || dd.name == "Home Assistant" {
			_ = s.store.AddDeviceID(ctx, owner, dev.ID, "tailscale", "n"+store.RandomToken(6)+"CNTRL", "")
			exec(`INSERT INTO addresses (user_id, device_id, ip, mac, network, source_id, first_seen, last_seen) VALUES (?, ?, ?, '', 'Tailscale', ?, ?, ?)`,
				owner, dev.ID, fmt.Sprintf("100.%d.%d.%d", 64+rng.Intn(60), rng.Intn(255), 1+rng.Intn(250)), ts.ID, fmtTime(first), fmtTime(now))
		}
		event(dev.ID, "new", first, "New device: "+dev.Label()+" at "+ip, ip, n.src)
		var lastSeen, onSince time.Time
		wasOn := false
		for t := start; !t.After(now); t = t.Add(time.Hour) {
			if t.Before(first.Truncate(time.Hour)) {
				continue
			}
			day := int(now.Sub(t).Hours() / 24)
			on := online(dd.pattern, t, day)
			if dd.name == "Photo NAS" && !t.Before(nasOutage) && t.Before(nasOutage.Add(2*time.Hour)) {
				on = false
			}
			if dd.name == "Office camera" && day == 6 && t.Hour() >= 14 && t.Hour() < 15 {
				on = false
			}
			if on && dd.pattern != "always" && rng.Intn(9) == 0 {
				on = false
			}
			if on && !wasOn {
				onSince = t.Add(time.Duration(rng.Intn(50)) * time.Minute)
			}
			if on {
				slots := 0xfff
				if dd.private && rng.Intn(3) == 0 {
					slots = rng.Intn(0xfff) | 1
				}
				presence(dev.ID, store.HourKey(t), slots)
				lastSeen = t.Add(time.Duration(55) * time.Minute)
				if lastSeen.After(now) {
					lastSeen = now.Add(-time.Duration(rng.Intn(120)) * time.Second)
				}
			}
			if dd.watch && on != wasOn && !t.Equal(first.Truncate(time.Hour)) {
				if on {
					event(dev.ID, "online", t.Add(4*time.Minute), "Came back online", ip, n.src)
				} else {
					event(dev.ID, "offline", t.Add(17*time.Minute), "Went offline (last seen "+t.Add(-time.Minute*3).Format("15:04")+")", ip, n.src)
				}
			}
			wasOn = on
		}
		dev.Online = online(dd.pattern, now, 0) && !lastSeen.IsZero() && now.Sub(lastSeen) < 30*time.Minute
		if !lastSeen.IsZero() {
			dev.LastSeen = fmtTime(lastSeen)
		}
		dev.StatusAt = dev.LastSeen
		if dev.Online && !onSince.IsZero() && onSince.Before(now) {
			dev.StatusAt = fmtTime(onSince)
		}
		if err := s.store.SaveDevice(ctx, dev); err != nil {
			return err
		}
		exec(`INSERT INTO addresses (user_id, device_id, ip, mac, network, source_id, first_seen, last_seen) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			owner, dev.ID, ip, mac, n.label, n.src, fmtTime(first), firstNonEmpty(dev.LastSeen, fmtTime(first)))
		if dd.name == "Leo's Pixel" {
			old := n.prefix + "57"
			exec(`INSERT INTO addresses (user_id, device_id, ip, mac, network, source_id, first_seen, last_seen) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				owner, dev.ID, old, mac, n.label, n.src, fmtTime(first), fmtTime(now.Add(-20*24*time.Hour)))
			event(dev.ID, "ip", now.Add(-20*24*time.Hour), "Address changed from "+old+" to "+ip, ip, n.src)
		}
		// the same phone under several private addresses, waiting in the review
		for i := 0; i < dd.rotate; i++ {
			m := randMAC("", true)
			at := now.Add(-time.Duration(dd.firstDays*24-i*20) * time.Hour)
			twin := &store.Device{UserID: owner, Hostname: dd.host, Review: "new", Source: "scan", FirstSeen: fmtTime(at), LastSeen: fmtTime(at.Add(5 * time.Hour)),
				LastIP: fmt.Sprintf("%s%d", n.prefix, 140+i), LastMAC: m, RandomMAC: true, LastNetwork: n.label, LastSource: "scan", StatusAt: fmtTime(at.Add(5 * time.Hour))}
			twin.KindGuess = netscan.GuessKind(dd.host, "", "", "")
			if err := s.store.CreateDevice(ctx, twin); err != nil {
				return err
			}
			_ = s.store.AddDeviceID(ctx, owner, twin.ID, "mac", m, twin.LastSeen)
			exec(`INSERT INTO addresses (user_id, device_id, ip, mac, network, source_id, first_seen, last_seen) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				owner, twin.ID, twin.LastIP, m, n.label, n.src, twin.FirstSeen, twin.LastSeen)
			event(twin.ID, "new", at, "New device: "+twin.Label()+" at "+twin.LastIP, twin.LastIP, n.src)
		}
	}
	event(0, "wan_ip", now.Add(-19*24*time.Hour-5*time.Hour), "Your internet address changed from 84.112.37.201 to 84.112.40.18", "84.112.40.18", 0)
	event(0, "source_failed", now.Add(-5*24*time.Hour-3*time.Hour), "Garden office stopped working: the agent has not reported since "+now.Add(-5*24*time.Hour-3*time.Hour-16*time.Minute).Format("2 Jan 15:04"), "", office.ID)
	event(0, "source_ok", now.Add(-5*24*time.Hour-1*time.Hour), "Garden office works again", "", office.ID)
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := tx.ExecContext(ctx, r.q, r.args...); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.store.CachePut(ctx, fmt.Sprintf("wan:%d", owner), "84.112.40.18")
	_ = s.store.CachePut(ctx, fmt.Sprintf("wan:%d:since", owner), fmtTime(now.Add(-19*24*time.Hour-5*time.Hour)))
	s.log.Info("demo data seeded", "devices", len(demoDevices))
	return nil
}
