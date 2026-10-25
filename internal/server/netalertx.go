package server

import (
	"context"
	"encoding/csv"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-network/internal/netscan"
	"github.com/abjelosevic88/pocket-homelab-network/internal/store"
)

// ---- moving over from NetAlertX ----
//
// NetAlertX writes its devices to config/devices.csv (the CSV backup plugin)
// and exports the same file from Maintenance → Backup / Restore. Each row
// becomes a device here, keyed by its MAC: importing the file again updates
// what is still empty instead of adding devices twice. What you named and
// reviewed there arrives named and known; what it never got to arrives as
// new, so the review groups the look-alikes.

var naxPlaceholders = map[string]bool{"": true, "(name not found)": true, "<unknown>": true, "(unknown)": true, "unknown": true, "null": true, "none": true}

func naxKind(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	switch {
	case t == "":
		return ""
	case strings.Contains(t, "watch"):
		return "watch"
	case strings.Contains(t, "phone"):
		return "phone"
	case strings.Contains(t, "tablet"), strings.Contains(t, "ipad"):
		return "tablet"
	case strings.Contains(t, "laptop"), strings.Contains(t, "notebook"):
		return "laptop"
	case t == "pc", strings.Contains(t, "desktop"), strings.Contains(t, "computer"), strings.Contains(t, "workstation"):
		return "computer"
	case strings.Contains(t, "nas"), strings.Contains(t, "storage"):
		return "nas"
	case strings.Contains(t, "server"), strings.Contains(t, "raspberry"):
		return "server"
	case strings.Contains(t, "access point"), t == "ap":
		return "access_point"
	case strings.Contains(t, "router"), strings.Contains(t, "gateway"), strings.Contains(t, "firewall"):
		return "router"
	case strings.Contains(t, "switch"):
		return "switch"
	case strings.Contains(t, "console"), strings.Contains(t, "game"):
		return "console"
	case strings.Contains(t, "tv"), strings.Contains(t, "streaming"), strings.Contains(t, "media"):
		return "tv"
	case strings.Contains(t, "speaker"), strings.Contains(t, "audio"):
		return "speaker"
	case strings.Contains(t, "printer"):
		return "printer"
	case strings.Contains(t, "camera"):
		return "camera"
	case strings.Contains(t, "iot"), strings.Contains(t, "smart"), strings.Contains(t, "domotic"), strings.Contains(t, "plug"):
		return "smart_home"
	}
	return "other"
}

// naxTime reads NetAlertX's local "2026-08-13 11:36:14".
func naxTime(v string) string {
	v = strings.TrimSpace(v)
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return fmtTime(t)
		}
	}
	return ""
}

func (s *Server) handleImportNetAlertX(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var rd io.Reader = http.MaxBytesReader(w, r.Body, 16<<20)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		f, _, err := r.FormFile("file")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "attach devices.csv as file")
			return
		}
		defer f.Close()
		rd = f
	}
	counts, err := s.importNetAlertX(ctx, uid, rd)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, counts)
}

func (s *Server) importNetAlertX(ctx context.Context, uid int64, rd io.Reader) (map[string]int, error) {
	cr := csv.NewReader(rd)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	head, err := cr.Read()
	if err != nil {
		return nil, errBadFile("that file is empty")
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))] = i
	}
	if _, ok := col["devMac"]; !ok {
		return nil, errBadFile("this is not a NetAlertX devices.csv (no devMac column)")
	}
	srcs, _ := s.store.ListSources(ctx, uid)
	networkFor := func(ip string) string {
		p := net.ParseIP(ip)
		for _, src := range srcs {
			if src.Kind != "scan" {
				continue
			}
			if _, n, err := net.ParseCIDR(sourceCfg(&src).Subnet); err == nil && p != nil && n.Contains(p) {
				return src.Network
			}
			if _, n, err := net.ParseCIDR(src.Network); err == nil && p != nil && n.Contains(p) {
				return src.Network
			}
		}
		return ""
	}
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	counts := map[string]int{"created": 0, "updated": 0, "skipped": 0}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return counts, errBadFile("the CSV could not be read: " + err.Error())
		}
		get := func(k string) string {
			if i, ok := col[k]; ok && i < len(rec) {
				v := strings.TrimSpace(rec[i])
				if v == "None" || v == "null" {
					return ""
				}
				return v
			}
			return ""
		}
		mac := netscan.NormalizeMAC(get("devMac"))
		if mac == "" {
			if strings.EqualFold(get("devMac"), "internet") && net.ParseIP(get("devLastIP")) != nil {
				_ = s.store.CachePut(ctx, "wan:"+strconv.FormatInt(uid, 10), get("devLastIP"))
			}
			counts["skipped"]++
			continue
		}
		name := strings.TrimSpace(strings.Trim(get("devName"), "\t"))
		if naxPlaceholders[strings.ToLower(name)] || netscan.CleanName(name) == "" {
			name = ""
		}
		reviewed := get("devIsNew") == "0" || get("devNameSource") == "USER"
		vendor := get("devVendor")
		if strings.HasPrefix(vendor, "(Unknown") || strings.HasPrefix(strings.ToLower(vendor), "unknown") {
			vendor = ""
		}
		ip := get("devLastIP")
		if p := net.ParseIP(ip); p == nil || p.To4() == nil {
			ip = ""
		}
		var d *store.Device
		fresh := false
		if id := s.store.DeviceByID(ctx, uid, "mac", mac); id > 0 {
			if d, err = s.store.GetDevice(ctx, uid, id); err != nil {
				return counts, err
			}
		} else {
			fresh = true
			d = &store.Device{UserID: uid, Source: "import", Review: "new", LastMAC: mac, RandomMAC: netscan.PrivateMAC(mac)}
		}
		fill := func(dst *string, v string) {
			if *dst == "" && v != "" {
				*dst = v
			}
		}
		if reviewed {
			fill(&d.Name, name)
			if d.Review == "new" {
				d.Review = "known"
			}
		} else {
			fill(&d.Hostname, name)
		}
		fill(&d.Hostname, netscan.CleanName(get("devFQDN")))
		fill(&d.Owner, get("devOwner"))
		fill(&d.Kind, naxKind(get("devType")))
		fill(&d.Vendor, firstNonEmpty(vendor, netscan.Vendor(mac)))
		fill(&d.Notes, get("devComments"))
		fill(&d.Location, get("devLocation"))
		fill(&d.LastIP, ip)
		fill(&d.LastNetwork, networkFor(ip))
		if first := naxTime(get("devFirstConnection")); first != "" && (d.FirstSeen == "" || first < d.FirstSeen) {
			d.FirstSeen = first
		}
		if last := naxTime(get("devLastConnection")); last != "" && last > d.LastSeen {
			d.LastSeen = last
		}
		if get("devFavorite") == "1" {
			d.Pinned = true
		}
		if get("devAlertDown") == "1" {
			d.Watch = true
		}
		if get("devIsArchived") == "1" && d.Review == "new" {
			d.Review = "guest"
		}
		d.KindGuess = netscan.GuessKind(firstNonEmpty(d.Hostname, d.Name), d.Vendor, d.Model, d.OS)
		if fresh {
			if err := s.store.CreateDevice(ctx, d); err != nil {
				return counts, err
			}
			counts["created"]++
			_ = s.store.AddEvent(ctx, uid, &store.Event{DeviceID: &d.ID, Kind: "imported", Text: "Imported from NetAlertX", IP: ip})
		} else {
			if err := s.store.SaveDevice(ctx, d); err != nil {
				return counts, err
			}
			counts["updated"]++
		}
		if err := s.store.AddDeviceID(ctx, uid, d.ID, "mac", mac, d.LastSeen); err != nil {
			return counts, err
		}
		if ip != "" && d.LastSeen != "" {
			_ = s.store.TouchAddress(ctx, uid, d.ID, ip, mac, d.LastNetwork, 0, d.LastSeen)
		}
	}
	return counts, nil
}

type errBadFile string

func (e errBadFile) Error() string { return string(e) }
