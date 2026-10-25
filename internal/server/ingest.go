package server

import (
	"context"
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/abjelosevic88/pocket-homelab-network/internal/netscan"
	"github.com/abjelosevic88/pocket-homelab-network/internal/store"
)

// ---- turning sightings into devices ----
//
// Every source reports netscan.Observations. A sighting is matched to a
// device by its MAC, else its Tailscale node, else (AdGuard, which only
// knows addresses) the device last seen at that address. A new private
// (changing) MAC whose name matches a device you already have joins that
// device instead of showing up as new (Settings → auto_merge). A Tailscale
// peer whose LAN address matches a device the scan found is joined with it,
// so a laptop is one device whether it is at home or away.

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// layer2 sources see devices on the local network itself (MAC + LAN address).
func layer2(kind string) bool { return kind == "scan" || kind == "agent" || kind == "openwrt" }

// selfDeclared names come from the device itself (or its DHCP request).
var selfDeclared = map[string]bool{"mdns": true, "dhcp": true, "netbios": true}

// autoWatchKinds are watched from the start: you want to know when they go down.
var autoWatchKinds = map[string]bool{"router": true, "access_point": true, "server": true, "nas": true, "switch": true}

type ingestRun struct {
	s       *Server
	src     *store.Source
	st      store.Settings
	network string
	at      time.Time
	now     string
	alerts  []int64
	fresh   []*store.Device
}

// ingest writes one source run: devices, identifiers, addresses, online
// state, history and events. It returns how many devices were there and how
// many are new.
func (s *Server) ingest(ctx context.Context, src *store.Source, network string, obs []netscan.Observation, at time.Time) (found, fresh int, err error) {
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	st, err := s.store.GetSettings(ctx, src.UserID)
	if err != nil {
		return 0, 0, err
	}
	run := &ingestRun{s: s, src: src, st: st, network: network, at: at, now: fmtTime(at)}
	// Tailscale's LAN hints after the peers themselves.
	sort.SliceStable(obs, func(i, j int) bool {
		return obs[i].NameSource != "tailscale-lan" && obs[j].NameSource == "tailscale-lan"
	})
	for _, o := range obs {
		o.MAC = netscan.NormalizeMAC(o.MAC)
		if p := net.ParseIP(o.IP); p == nil || p.To4() == nil {
			o.IP = ""
		}
		if o.MAC == "" && o.IP == "" && o.TailscaleID == "" {
			continue
		}
		if o.NameSource == "tailscale-lan" {
			if err := run.joinTailscaleLAN(ctx, o); err != nil {
				return found, len(run.fresh), err
			}
			continue
		}
		present := !o.Offline && src.Presence
		ok, err := run.one(ctx, o, present)
		if err != nil {
			return found, len(run.fresh), err
		}
		if ok && present {
			found++
		}
	}
	for _, d := range run.fresh {
		s.publish(ctx, src.UserID, "network.device_new", fmt.Sprintf("device:%d", d.ID), map[string]any{
			"device_id": d.ID, "name": d.Label(), "kind": firstNonEmpty(d.Kind, d.KindGuess), "vendor": d.Vendor, "ip": d.LastIP, "mac": d.LastMAC,
			"network": d.LastNetwork, "private_mac": d.RandomMAC, "review": d.Review, "url": s.deviceURL(d.ID)})
	}
	s.queueAlert(ctx, src.UserID, run.alerts...)
	return found, len(run.fresh), nil
}

func (s *Server) deviceURL(id int64) string {
	if s.cfg.BaseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/devices/%d", s.cfg.BaseURL, id)
}

func (r *ingestRun) event(ctx context.Context, d *store.Device, kind, text, ip string, alert bool) {
	e := &store.Event{DeviceID: &d.ID, Kind: kind, At: r.now, Text: text, IP: ip, SourceID: r.src.ID}
	if err := r.s.store.AddEvent(ctx, r.src.UserID, e); err == nil && alert {
		r.alerts = append(r.alerts, e.ID)
	}
}

// match finds the device a sighting belongs to (0 = none yet) and why.
func (r *ingestRun) match(ctx context.Context, o netscan.Observation) (int64, string) {
	uid := r.src.UserID
	if o.MAC != "" {
		if id := r.s.store.DeviceByID(ctx, uid, "mac", o.MAC); id > 0 {
			return id, ""
		}
	}
	if o.TailscaleID != "" {
		if id := r.s.store.DeviceByID(ctx, uid, "tailscale", o.TailscaleID); id > 0 {
			return id, ""
		}
	}
	if r.src.Kind == "adguard" && o.IP != "" && o.MAC == "" {
		return r.s.store.DeviceByIP(ctx, uid, o.IP, fmtTime(r.at.Add(-24*time.Hour))), ""
	}
	if r.st.AutoMerge && o.MAC != "" && netscan.PrivateMAC(o.MAC) && netscan.DistinctName(o.Hostname) {
		key := netscan.NameKey(o.Hostname)
		same, _ := r.s.store.DevicesByName(ctx, uid, func(d store.Device) bool {
			return d.RandomMAC && (netscan.NameKey(d.Hostname) == key || (d.Name != "" && netscan.NameKey(d.Name) == key))
		})
		if len(same) > 0 {
			sort.Slice(same, func(i, j int) bool { return same[i].LastSeen > same[j].LastSeen })
			return same[0].ID, "private"
		}
	}
	return 0, ""
}

// one applies a single sighting; false when it belongs to no device (AdGuard
// knows an address nobody else has seen).
func (r *ingestRun) one(ctx context.Context, o netscan.Observation, present bool) (bool, error) {
	uid := r.src.UserID
	id, why := r.match(ctx, o)
	var d *store.Device
	isNew := false
	if id > 0 {
		var err error
		if d, err = r.s.store.GetDevice(ctx, uid, id); err != nil {
			return false, err
		}
	} else {
		if !sourceCreates(r.src.Kind) || (o.MAC == "" && o.TailscaleID == "") {
			return false, nil
		}
		isNew = true
		seen := r.now
		if !present && !o.LastSeen.IsZero() {
			seen = fmtTime(o.LastSeen)
		}
		d = &store.Device{UserID: uid, Review: "new", Source: r.src.Kind, FirstSeen: seen, StatusAt: r.now}
		if o.Self || r.src.Kind == "tailscale" {
			// this machine, or a device on your own tailnet: not a stranger
			d.Review = "known"
		}
	}
	wasOnline := d.Online
	r.apply(ctx, d, o, present)
	if isNew {
		if o.Self {
			d.Name, d.Kind = firstNonEmpty(d.Hostname, "This server"), firstNonEmpty(d.KindGuess, "server")
		}
		if r.st.AutoWatch && layer2(r.src.Kind) && !d.RandomMAC && autoWatchKinds[d.KindGuess] {
			d.Watch = true
		}
		if r.src.Kind == "tailscale" && d.Name == "" {
			d.Name = d.Hostname
		}
		if err := r.s.store.CreateDevice(ctx, d); err != nil {
			return false, err
		}
		r.fresh = append(r.fresh, d)
		text := "New device: " + d.Label()
		if d.LastIP != "" {
			text += " at " + d.LastIP
		}
		if r.src.Kind == "tailscale" {
			text = "Added from Tailscale: " + d.Label()
		}
		r.event(ctx, d, "new", text, d.LastIP, d.Review == "new" && r.st.NotifyNew)
	} else {
		if present && !wasOnline {
			d.StatusAt = r.now
			watched := d.Watch && !d.Muted && d.Review != "guest"
			r.event(ctx, d, "online", "Came back online", o.IP, watched && r.st.NotifyBack)
			if watched {
				r.s.publish(ctx, uid, "network.device_back", fmt.Sprintf("device:%d", d.ID), map[string]any{
					"device_id": d.ID, "name": d.Label(), "kind": firstNonEmpty(d.Kind, d.KindGuess), "ip": firstNonEmpty(o.IP, d.LastIP), "url": r.s.deviceURL(d.ID)})
			}
		}
		if why == "private" {
			r.event(ctx, d, "address", "Recognised under a new private address "+o.MAC+" (same name)", o.IP, false)
		}
		if err := r.s.store.SaveDevice(ctx, d); err != nil {
			return false, err
		}
	}
	seenAt := r.now
	if !present {
		seenAt = d.LastSeen
	}
	if o.MAC != "" {
		if err := r.s.store.AddDeviceID(ctx, uid, d.ID, "mac", o.MAC, seenAt); err != nil {
			return false, err
		}
	}
	if o.TailscaleID != "" {
		if err := r.s.store.AddDeviceID(ctx, uid, d.ID, "tailscale", o.TailscaleID, seenAt); err != nil {
			return false, err
		}
	}
	if o.IP != "" && seenAt != "" && (present || r.src.Kind == "tailscale") {
		if err := r.s.store.TouchAddress(ctx, uid, d.ID, o.IP, o.MAC, r.network, r.src.ID, seenAt); err != nil {
			return false, err
		}
	}
	if present {
		_ = r.s.store.MarkPresence(ctx, uid, d.ID, r.at)
	}
	return true, nil
}

// apply copies what a sighting tells about a device onto it.
func (r *ingestRun) apply(ctx context.Context, d *store.Device, o netscan.Observation, present bool) {
	kind := r.src.Kind
	if present {
		d.Online = true
		d.LastSeen = r.now
		if r.network != "" {
			d.LastNetwork = r.network
		}
		d.LastSource = kind
	} else if !o.LastSeen.IsZero() && fmtTime(o.LastSeen) > d.LastSeen {
		d.LastSeen = fmtTime(o.LastSeen)
	}
	if d.LastNetwork == "" && r.network != "" && kind != "adguard" {
		d.LastNetwork = r.network
	}
	if o.IP != "" && (layer2(kind) && present || d.LastIP == "" && kind != "adguard") {
		if layer2(kind) && d.LastIP != "" && d.LastIP != o.IP && d.ID > 0 {
			r.event(ctx, d, "ip", "Address changed from "+d.LastIP+" to "+o.IP, o.IP, false)
		}
		d.LastIP = o.IP
	}
	if o.MAC != "" {
		d.LastMAC = o.MAC
		d.RandomMAC = netscan.PrivateMAC(o.MAC)
		if d.Vendor == "" {
			d.Vendor = firstNonEmpty(o.Vendor, netscan.Vendor(o.MAC))
		}
	}
	if n := netscan.CleanName(o.Hostname); n != "" && (d.Hostname == "" || (selfDeclared[o.NameSource] && kind != "adguard")) {
		d.Hostname = n
	}
	if d.Name == "" && o.ClientName != "" {
		d.Name = o.ClientName
	}
	if o.Model != "" {
		d.Model = o.Model
	}
	if o.OS != "" {
		d.OS = o.OS
	}
	if kind == "adguard" {
		d.DNSQueries, d.DNSBlocked = o.DNSQueries, o.DNSBlocked
	}
	if g := netscan.GuessKind(firstNonEmpty(d.Hostname, d.Name), d.Vendor, d.Model, d.OS); g != "" {
		d.KindGuess = g
	}
}

// joinTailscaleLAN: Tailscale says its peer talks from this LAN address. When
// a scan saw a device there in the last half hour, the Tailscale device and
// the LAN device are the same machine: join them.
func (r *ingestRun) joinTailscaleLAN(ctx context.Context, o netscan.Observation) error {
	uid := r.src.UserID
	tsID := r.s.store.DeviceByID(ctx, uid, "tailscale", o.TailscaleID)
	lanID := r.s.store.DeviceByIP(ctx, uid, o.IP, fmtTime(r.at.Add(-30*time.Minute)))
	if lanID == 0 || lanID == tsID {
		return nil
	}
	lan, err := r.s.store.GetDevice(ctx, uid, lanID)
	if err != nil || lan.LastMAC == "" {
		return nil
	}
	if tsID == 0 {
		return r.s.store.AddDeviceID(ctx, uid, lanID, "tailscale", o.TailscaleID, r.now)
	}
	ts, err := r.s.store.GetDevice(ctx, uid, tsID)
	if err != nil {
		return nil
	}
	if lan.Name == "" {
		lan.Name = ts.Name
	}
	if lan.Owner == "" {
		lan.Owner = ts.Owner
	}
	if lan.Notes == "" {
		lan.Notes = ts.Notes
	}
	if lan.Review == "new" && ts.Review != "new" {
		lan.Review = ts.Review
	}
	if lan.OS == "" {
		lan.OS = ts.OS
	}
	lan.Watch = lan.Watch || ts.Watch
	lan.Pinned = lan.Pinned || ts.Pinned
	if lan.InventoryID == 0 {
		lan.InventoryID, lan.InventoryName = ts.InventoryID, ts.InventoryName
	}
	if lan.MonitoringID == 0 {
		lan.MonitoringID, lan.MonitoringName = ts.MonitoringID, ts.MonitoringName
	}
	if err := r.s.store.SaveDevice(ctx, lan); err != nil {
		return err
	}
	if err := r.s.store.MergeDevices(ctx, uid, lanID, []int64{tsID}); err != nil {
		return err
	}
	for i, d := range r.fresh {
		if d.ID == tsID {
			r.fresh = append(r.fresh[:i], r.fresh[i+1:]...)
			break
		}
	}
	r.event(ctx, lan, "merged", "Joined with its Tailscale entry "+ts.Label(), o.IP, false)
	return nil
}
