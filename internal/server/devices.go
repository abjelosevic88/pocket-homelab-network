package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-network/internal/netscan"
	"github.com/abjelosevic88/pocket-homelab-network/internal/store"
)

// ---- devices ----

func validReview(v string) bool { return v == "new" || v == "known" || v == "guest" }

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	f := store.DeviceFilter{Review: q.Get("review"), Query: q.Get("q"), Owner: q.Get("owner"), Kind: q.Get("kind"), Watch: queryBool(r, "watch")}
	if f.Review != "" && !validReview(f.Review) {
		writeErr(w, http.StatusBadRequest, "review is new, known or guest")
		return
	}
	switch q.Get("online") {
	case "1", "true":
		f.Online = boolp(true)
	case "0", "false":
		f.Online = boolp(false)
	}
	list, err := s.store.ListDevices(ctx, actorID(ctx), f)
	if err != nil {
		s.fail(w, err, "devices")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// deviceInput is a device edit; absent keys stay as they are.
type deviceInput struct {
	Name     string `json:"name"`
	Owner    string `json:"owner"`
	Kind     string `json:"kind"`
	Location string `json:"location"`
	Notes    string `json:"notes"`
	LinkURL  string `json:"link_url"`
	Review   string `json:"review"`
	Watch    bool   `json:"watch"`
	Muted    bool   `json:"muted"`
	Pinned   bool   `json:"pinned"`
	MAC      string `json:"mac"` // adding a device by hand
	IP       string `json:"ip"`
}

func (in deviceInput) apply(keys map[string]bool, d *store.Device) string {
	text := func(k string, dst *string, v string, n int) bool {
		if !keys[k] {
			return true
		}
		v = strings.TrimSpace(v)
		if len([]rune(v)) > n {
			return false
		}
		*dst = v
		return true
	}
	if !text("name", &d.Name, in.Name, 120) || !text("owner", &d.Owner, in.Owner, 80) || !text("location", &d.Location, in.Location, 80) || !text("notes", &d.Notes, in.Notes, 4000) {
		return "that is too long"
	}
	if keys["link_url"] {
		u := strings.TrimSpace(in.LinkURL)
		if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			u = "http://" + u
		}
		d.LinkURL = u
	}
	if keys["kind"] {
		if !netscan.ValidKind(in.Kind) {
			return "unknown kind of device"
		}
		d.Kind = in.Kind
	}
	if keys["review"] {
		if !validReview(in.Review) {
			return "review is new, known or guest"
		}
		d.Review = in.Review
	}
	if keys["watch"] {
		d.Watch = in.Watch
	}
	if keys["muted"] {
		d.Muted = in.Muted
	}
	if keys["pinned"] {
		d.Pinned = in.Pinned
	}
	return ""
}

// handleCreateDevice adds a device by hand (one no source can see, or before it shows up).
func (s *Server) handleCreateDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in deviceInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	d := &store.Device{UserID: uid, Review: "known", Source: firstNonEmpty(linkApp(ctx), "manual")}
	if msg := in.apply(keys, d); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if d.Name == "" {
		writeErr(w, http.StatusBadRequest, "give it a name")
		return
	}
	mac := ""
	if strings.TrimSpace(in.MAC) != "" {
		if mac = netscan.NormalizeMAC(in.MAC); mac == "" {
			writeErr(w, http.StatusBadRequest, "that is not a MAC address (aa:bb:cc:dd:ee:ff)")
			return
		}
		if id := s.store.DeviceByID(ctx, uid, "mac", mac); id > 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "a device with that MAC address is already here", "device_id": id})
			return
		}
		d.LastMAC, d.RandomMAC, d.Vendor = mac, netscan.PrivateMAC(mac), netscan.Vendor(mac)
	}
	if ip := strings.TrimSpace(in.IP); ip != "" {
		if p := net.ParseIP(ip); p == nil || p.To4() == nil {
			writeErr(w, http.StatusBadRequest, "that is not an IPv4 address")
			return
		}
		d.LastIP = ip
	}
	d.KindGuess = netscan.GuessKind(d.Name, d.Vendor, "", "")
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	if err := s.store.CreateDevice(ctx, d); err != nil {
		s.fail(w, err, "create device")
		return
	}
	if mac != "" {
		_ = s.store.AddDeviceID(ctx, uid, d.ID, "mac", mac, "")
	}
	_ = s.store.AddEvent(ctx, uid, &store.Event{DeviceID: &d.ID, Kind: "new", Text: "Added by hand: " + d.Label()})
	writeJSON(w, http.StatusCreated, d)
}

// deviceDetail is everything the device page shows.
type deviceDetail struct {
	Device    *store.Device        `json:"device"`
	IDs       []store.DeviceID     `json:"ids"`
	Addresses []store.Address      `json:"addresses"`
	Events    []store.Event        `json:"events"`
	Presence  []store.PresenceHour `json:"presence"`
	Similar   []store.Device       `json:"similar"`
	Sources   map[int64]string     `json:"sources"`
	Services  json.RawMessage      `json:"services"`
	CanWake   bool                 `json:"can_wake"`
}

func (s *Server) handleGetDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	d, err := s.store.GetDevice(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "device")
		return
	}
	out := deviceDetail{Device: d, Sources: map[int64]string{}, Services: json.RawMessage("null")}
	out.IDs, _ = s.store.DeviceIDs(ctx, uid, d.ID)
	for i := range out.IDs {
		if out.IDs[i].Kind == "mac" {
			out.IDs[i].Private = netscan.PrivateMAC(out.IDs[i].Value)
			out.IDs[i].Vendor = netscan.Vendor(out.IDs[i].Value)
		}
	}
	out.Addresses, _ = s.store.Addresses(ctx, uid, d.ID)
	out.Events, _ = s.store.ListEvents(ctx, uid, store.EventFilter{DeviceID: d.ID, Limit: 200})
	out.Presence, _ = s.store.Presence(ctx, uid, d.ID, store.HourKey(time.Now().AddDate(0, 0, -29).Truncate(24*time.Hour)))
	out.Similar = s.similar(ctx, uid, d)
	if srcs, err := s.store.ListSources(ctx, uid); err == nil {
		for _, src := range srcs {
			out.Sources[src.ID] = src.Name
		}
	}
	if json.Valid([]byte(d.Ports)) && d.Ports != "" {
		out.Services = json.RawMessage(d.Ports)
	}
	out.CanWake = wakeable(d) && d.LastSource == "scan" && s.cfg.LocalScan
	writeJSON(w, http.StatusOK, out)
}

// similar are other devices that look like the same one: the same distinct
// name (a phone or watch that changes its private address), or the same name
// given by hand.
func (s *Server) similar(ctx context.Context, uid int64, d *store.Device) []store.Device {
	keys := map[string]bool{}
	for _, n := range []string{d.Hostname, d.Name} {
		if netscan.DistinctName(n) {
			keys[netscan.NameKey(n)] = true
		}
	}
	out := []store.Device{}
	if len(keys) == 0 {
		return out
	}
	list, _ := s.store.DevicesByName(ctx, uid, func(o store.Device) bool {
		return o.ID != d.ID && (keys[netscan.NameKey(o.Hostname)] && o.Hostname != "" || keys[netscan.NameKey(o.Name)] && o.Name != "")
	})
	return append(out, list...)
}

func (s *Server) handleUpdateDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in deviceInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	d, err := s.store.GetDevice(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "device")
		return
	}
	if app := linkApp(ctx); app != "" && (keys["review"] || keys["watch"] || keys["muted"]) && d.Source != app {
		writeErr(w, http.StatusForbidden, "a linked app may only rename and describe devices")
		return
	}
	old := d.Label()
	if msg := in.apply(keys, d); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.SaveDevice(ctx, d); err != nil {
		s.fail(w, err, "save device")
		return
	}
	if keys["name"] && d.Label() != old {
		_ = s.store.AddEvent(ctx, uid, &store.Event{DeviceID: &d.ID, Kind: "renamed", Text: "Renamed from " + old + " to " + d.Label()})
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	if err := s.store.DeleteDevice(ctx, actorID(ctx), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete device")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMergeDevices folds other devices into this one ("these are the same device").
func (s *Server) handleMergeDevices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil || len(in.IDs) == 0 {
		writeErr(w, http.StatusBadRequest, "ids are required")
		return
	}
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	target, err := s.store.GetDevice(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "device")
		return
	}
	var names []string
	for _, id := range in.IDs {
		o, err := s.store.GetDevice(ctx, uid, id)
		if err != nil {
			s.fail(w, err, "device")
			return
		}
		if o.ID == target.ID {
			continue
		}
		names = append(names, o.Label())
		if target.Name == "" {
			target.Name = o.Name
		}
		for _, p := range []struct {
			dst *string
			v   string
		}{{&target.Owner, o.Owner}, {&target.Location, o.Location}, {&target.Notes, o.Notes}, {&target.Kind, o.Kind}, {&target.Hostname, o.Hostname}, {&target.Vendor, o.Vendor}, {&target.OS, o.OS}} {
			if *p.dst == "" {
				*p.dst = p.v
			}
		}
		if o.LastSeen > target.LastSeen {
			target.LastSeen, target.LastIP, target.LastMAC, target.Online, target.LastNetwork, target.LastSource, target.RandomMAC = o.LastSeen, o.LastIP, o.LastMAC, o.Online, o.LastNetwork, o.LastSource, o.RandomMAC
		}
		if target.Review == "new" && o.Review != "new" {
			target.Review = o.Review
		}
		target.Watch = target.Watch || o.Watch
		target.Pinned = target.Pinned || o.Pinned
		if target.InventoryID == 0 {
			target.InventoryID, target.InventoryName = o.InventoryID, o.InventoryName
		}
		if target.MonitoringID == 0 {
			target.MonitoringID, target.MonitoringName = o.MonitoringID, o.MonitoringName
		}
	}
	if err := s.store.SaveDevice(ctx, target); err != nil {
		s.fail(w, err, "save device")
		return
	}
	if err := s.store.MergeDevices(ctx, uid, target.ID, in.IDs); err != nil {
		s.fail(w, err, "join devices")
		return
	}
	if len(names) > 0 {
		_ = s.store.AddEvent(ctx, uid, &store.Event{DeviceID: &target.ID, Kind: "merged", Text: "Joined with " + strings.Join(names, ", ")})
	}
	d, _ := s.store.GetDevice(ctx, uid, target.ID)
	writeJSON(w, http.StatusOK, d)
}

// handleForgetID removes one identifier from a device ("this address is not this device").
func (s *Server) handleForgetID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	if err := s.store.RemoveDeviceID(ctx, uid, idParam(r, "id"), idParam(r, "ident")); err != nil {
		s.fail(w, err, "identifier")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleReviewMany sets the review (known / guest) of several devices at once.
func (s *Server) handleReviewMany(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		IDs    []int64 `json:"ids"`
		Review string  `json:"review"`
		Owner  *string `json:"owner"`
		Muted  *bool   `json:"muted"`
	}
	if err := decode(r, &in); err != nil || len(in.IDs) == 0 || !validReview(in.Review) {
		writeErr(w, http.StatusBadRequest, "ids and a review (new, known or guest) are required")
		return
	}
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	n := 0
	for _, id := range in.IDs {
		d, err := s.store.GetDevice(ctx, uid, id)
		if err != nil {
			continue
		}
		d.Review = in.Review
		if in.Owner != nil {
			d.Owner = strings.TrimSpace(*in.Owner)
		}
		if in.Muted != nil {
			d.Muted = *in.Muted
		}
		if s.store.SaveDevice(ctx, d) == nil {
			n++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"updated": n})
}

// reviewGroup is new devices that look like one (same distinct name).
type reviewGroup struct {
	Key     string         `json:"key"`
	Name    string         `json:"name"`
	Devices []store.Device `json:"devices"`
	Known   *store.Device  `json:"known,omitempty"` // a reviewed device with the same name to join them to
}

// handleReview is the review queue: new devices, the look-alikes grouped.
func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	fresh, err := s.store.ListDevices(ctx, uid, store.DeviceFilter{Review: "new"})
	if err != nil {
		s.fail(w, err, "review")
		return
	}
	all, _ := s.store.ListDevices(ctx, uid, store.DeviceFilter{})
	known := map[string]*store.Device{}
	for i := range all {
		d := &all[i]
		if d.Review == "new" {
			continue
		}
		for _, n := range []string{d.Hostname, d.Name} {
			if netscan.DistinctName(n) {
				if k := netscan.NameKey(n); known[k] == nil {
					known[k] = d
				}
			}
		}
	}
	groups := map[string]*reviewGroup{}
	var order []string
	single := []store.Device{}
	for _, d := range fresh {
		n := firstNonEmpty(d.Hostname, d.Name)
		if !netscan.DistinctName(n) {
			single = append(single, d)
			continue
		}
		k := netscan.NameKey(n)
		g := groups[k]
		if g == nil {
			g = &reviewGroup{Key: k, Name: n, Known: known[k]}
			groups[k] = g
			order = append(order, k)
		}
		g.Devices = append(g.Devices, d)
	}
	out := []reviewGroup{}
	for _, k := range order {
		g := groups[k]
		if len(g.Devices) == 1 && g.Known == nil {
			single = append(single, g.Devices[0])
			continue
		}
		out = append(out, *g)
	}
	sort.SliceStable(single, func(i, j int) bool { return single[i].LastSeen > single[j].LastSeen })
	owners, _ := s.store.Owners(ctx, uid)
	st, _ := s.store.GetSettings(ctx, uid)
	owners = mergeNames(st.Owners, owners)
	locations, _ := s.store.Locations(ctx, uid)
	writeJSON(w, http.StatusOK, map[string]any{"groups": out, "devices": single, "owners": owners, "locations": locations, "total": len(fresh)})
}

func mergeNames(a, b []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, n := range append(append([]string{}, a...), b...) {
		if k := strings.ToLower(strings.TrimSpace(n)); k != "" && !seen[k] {
			seen[k] = true
			out = append(out, strings.TrimSpace(n))
		}
	}
	return out
}

func (s *Server) handleDeviceMeta(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	owners, _ := s.store.Owners(ctx, uid)
	st, _ := s.store.GetSettings(ctx, uid)
	locations, _ := s.store.Locations(ctx, uid)
	kinds := make([]map[string]string, 0, len(netscan.Kinds))
	for _, k := range netscan.Kinds {
		kinds = append(kinds, map[string]string{"id": k, "name": netscan.KindName[k]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"owners": mergeNames(st.Owners, owners), "locations": locations, "kinds": kinds})
}

// privateTarget: only addresses on your own networks are probed (never the internet).
func privateTarget(ip string) bool {
	p := net.ParseIP(ip)
	if p == nil || p.To4() == nil {
		return false
	}
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return p.IsPrivate() || cgnat.Contains(p)
}

// handleDeviceServices checks which common services a device answers on.
func (s *Server) handleDeviceServices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	d, err := s.store.GetDevice(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "device")
		return
	}
	if !privateTarget(d.LastIP) {
		writeErr(w, http.StatusBadRequest, "it has no address on your own network to check")
		return
	}
	c, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	svcs, err := netscan.CheckServices(c, d.LastIP)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	out := map[string]any{"ip": d.LastIP, "checked_at": store.Now(), "services": svcs}
	b, _ := json.Marshal(out)
	s.ingestMu.Lock()
	if cur, err := s.store.GetDevice(ctx, uid, d.ID); err == nil {
		cur.Ports = string(b)
		if cur.LinkURL == "" {
			for _, sv := range svcs {
				if sv.Web != "" && (sv.Port == 80 || sv.Port == 443) {
					cur.LinkURL = fmt.Sprintf("%s://%s", sv.Web, d.LastIP)
					break
				}
			}
		}
		_ = s.store.SaveDevice(ctx, cur)
	}
	s.ingestMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": out})
}

// wakeable: computers, servers and the like with a real (not private) MAC.
func wakeable(d *store.Device) bool {
	switch kindOf(*d) {
	case "phone", "watch", "tablet", "speaker", "camera", "smart_home", "car":
		return false
	}
	return d.LastMAC != "" && !d.RandomMAC
}

// handleWake sends Wake-on-LAN to a device on this server's network.
func (s *Server) handleWake(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	d, err := s.store.GetDevice(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "device")
		return
	}
	if !wakeable(d) || d.LastSource != "scan" || !s.cfg.LocalScan {
		writeErr(w, http.StatusBadRequest, "Wake-on-LAN works for devices on this server's own network")
		return
	}
	if err := netscan.WakeOnLAN(d.LastMAC, d.LastNetwork); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	_ = s.store.AddEvent(ctx, uid, &store.Event{DeviceID: &d.ID, Kind: "wake", Text: "Wake-on-LAN sent"})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- activity ----

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	f := store.EventFilter{DeviceID: int64(queryInt(r, "device")), Before: q.Get("before"), Limit: queryInt(r, "limit")}
	if k := strings.TrimSpace(q.Get("kind")); k != "" {
		f.Kinds = strings.Split(k, ",")
	}
	list, err := s.store.ListEvents(ctx, actorID(ctx), f)
	if err != nil {
		s.fail(w, err, "activity")
		return
	}
	writeJSON(w, http.StatusOK, list)
}
