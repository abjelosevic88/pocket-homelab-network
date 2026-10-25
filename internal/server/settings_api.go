package server

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-network/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-network/internal/netscan"
	"github.com/abjelosevic88/pocket-homelab-network/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-network/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-network/internal/store"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// publicSettings is what one person sees: their settings with the secrets
// masked and, per Pocket app, the mode in effect (<app>_mode), whether it
// works and how (<app>_via: core | direct | "") and why not (<app>_detail).
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	if st.Owners == nil {
		st.Owners = []string{}
	}
	out := map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"title": st.Title, "date_format": st.DateFormat, "time_format": st.TimeFormat, "week_start": st.WeekStart,
		"offline_min": st.OfflineMin, "auto_merge": st.AutoMerge, "auto_watch": st.AutoWatch, "keep_days": st.KeepDays, "track_wan": st.TrackWAN,
		"forget_guests_days": st.ForgetGuestsDays, "owners": st.Owners,
		"notify_new": st.NotifyNew, "notify_offline": st.NotifyOffline, "notify_back": st.NotifyBack, "notify_wan": st.NotifyWAN, "notify_source": st.NotifySource,
		"quiet_from": st.QuietFrom, "quiet_to": st.QuietTo, "notify_digest_min": st.NotifyDigestMin,
		"notify_url": st.NotifyURL, "notify_format": st.NotifyFormat, "notify_configured": st.NotifyURL != "",
		"inventory_url": st.InventoryURL, "inventory_token": mask(st.InventoryToken), "inventory_public_url": st.InventoryPublicURL,
		"monitoring_url": st.MonitoringURL, "monitoring_token": mask(st.MonitoringToken), "monitoring_public_url": st.MonitoringPublicURL,
		"ai_provider": st.AIProvider, "ai_pocket_model": st.AIPocketModel, "openai_api_key": mask(st.OpenAIKey), "openai_model": st.OpenAIModel, "openai_base_url": st.OpenAIBaseURL,
		"setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL, "local_scan": s.cfg.LocalScan,
	}
	for _, a := range pocketApps {
		mode, via, detail := s.connStatus(ctx, u, st, a)
		out[a.slug+"_mode"], out[a.slug+"_via"], out[a.slug+"_detail"] = mode, via, detail
		out[a.slug+"_configured"] = via != ""
		_, _, _, public := connSettings(st, a)
		if public == "" && via != "" {
			public = s.resolveSibling(ctx, a.slug)
		}
		out[a.slug+"_open_url"] = public
	}
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

var hhmm = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// handlePutSettings saves the caller's personal settings. The box-wide title
// is only taken from an admin.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var in store.Settings
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	bad := func(msg string) { writeErr(w, http.StatusBadRequest, msg) }
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	if keys["date_format"] {
		cur.DateFormat = in.DateFormat
	}
	if keys["time_format"] {
		cur.TimeFormat = in.TimeFormat
	}
	if keys["week_start"] && (in.WeekStart == 1 || in.WeekStart == 7 || in.WeekStart == 6) {
		cur.WeekStart = in.WeekStart
	}
	intIn := func(k string, dst *int, v, lo, hi int) bool {
		if !keys[k] {
			return true
		}
		if v < lo || v > hi {
			bad(fmt.Sprintf("%s is %d to %d", k, lo, hi))
			return false
		}
		*dst = v
		return true
	}
	if !intIn("offline_min", &cur.OfflineMin, in.OfflineMin, 2, 1440) || !intIn("keep_days", &cur.KeepDays, in.KeepDays, 7, 3650) ||
		!intIn("forget_guests_days", &cur.ForgetGuestsDays, in.ForgetGuestsDays, 0, 3650) || !intIn("notify_digest_min", &cur.NotifyDigestMin, in.NotifyDigestMin, 0, 1440) {
		return
	}
	for _, k := range []struct {
		key string
		v   string
	}{{"quiet_from", in.QuietFrom}, {"quiet_to", in.QuietTo}} {
		if keys[k.key] && k.v != "" && !hhmm.MatchString(k.v) {
			bad("quiet hours are HH:MM")
			return
		}
	}
	if keys["quiet_from"] {
		cur.QuietFrom = in.QuietFrom
	}
	if keys["quiet_to"] {
		cur.QuietTo = in.QuietTo
	}
	if keys["owners"] {
		cur.Owners = mergeNames(in.Owners, nil)
		if len(cur.Owners) > 30 {
			bad("up to 30 people")
			return
		}
	}
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	url := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	text := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimSpace(v)
		}
	}
	boolIn := func(k string, dst *bool, v bool) {
		if keys[k] {
			*dst = v
		}
	}
	boolIn("auto_merge", &cur.AutoMerge, in.AutoMerge)
	boolIn("auto_watch", &cur.AutoWatch, in.AutoWatch)
	boolIn("track_wan", &cur.TrackWAN, in.TrackWAN)
	boolIn("notify_new", &cur.NotifyNew, in.NotifyNew)
	boolIn("notify_offline", &cur.NotifyOffline, in.NotifyOffline)
	boolIn("notify_back", &cur.NotifyBack, in.NotifyBack)
	boolIn("notify_wan", &cur.NotifyWAN, in.NotifyWAN)
	boolIn("notify_source", &cur.NotifySource, in.NotifySource)
	text("notify_url", &cur.NotifyURL, in.NotifyURL)
	if keys["notify_format"] && in.NotifyFormat != "" {
		cur.NotifyFormat = in.NotifyFormat
	}
	url("inventory_url", &cur.InventoryURL, in.InventoryURL)
	secret("inventory_token", &cur.InventoryToken, in.InventoryToken)
	url("inventory_public_url", &cur.InventoryPublicURL, in.InventoryPublicURL)
	url("monitoring_url", &cur.MonitoringURL, in.MonitoringURL)
	secret("monitoring_token", &cur.MonitoringToken, in.MonitoringToken)
	url("monitoring_public_url", &cur.MonitoringPublicURL, in.MonitoringPublicURL)
	if keys["ai_provider"] {
		if !validAIProvider(in.AIProvider) {
			bad("AI is automatic, Pocket AI or OpenAI")
			return
		}
		cur.AIProvider = in.AIProvider
	}
	if keys["ai_pocket_model"] {
		if !validPocketModel(in.AIPocketModel) {
			bad("unknown model")
			return
		}
		cur.AIPocketModel = in.AIPocketModel
	}
	secret("openai_api_key", &cur.OpenAIKey, in.OpenAIKey)
	text("openai_model", &cur.OpenAIModel, in.OpenAIModel)
	url("openai_base_url", &cur.OpenAIBaseURL, in.OpenAIBaseURL)
	mode := func(k string, dst *string, v string) {
		if keys[k] && (v == modeCore || v == modeDirect || v == modeOff) {
			*dst = v
		}
	}
	mode("inventory_mode", &cur.InventoryMode, in.InventoryMode)
	mode("monitoring_mode", &cur.MonitoringMode, in.MonitoringMode)
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && keys["title"] {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	for _, k := range modeKeys {
		if keys[k] {
			s.reportModesSoon()
			break
		}
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		User   string `json:"user"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	uid := actorID(r.Context())
	st, _ := s.store.GetSettings(r.Context(), uid)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return strings.TrimRight(strings.TrimSpace(in.URL), "/")
	}
	var msg string
	var err error
	if a, ok := appBySlug(in.Which); ok {
		mode, savedURL, savedTok, _ := connSettings(st, a)
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		var conn *siblingConn
		if tok := keyOr(savedTok); tok != "" && (typed || s.connMode(mode, savedTok) == modeDirect) {
			base := urlOr(savedURL)
			if base == "" {
				base = s.resolveSibling(ctx, a.slug)
			}
			conn = &siblingConn{client: pocket.New(base, tok, 15*time.Second), via: modeDirect}
		} else {
			conn, err = s.clients(ctx, uid).conn(ctx, a, a.scopes...)
		}
		if err == nil {
			how := "direct connection"
			if conn.via == modeCore {
				how = "through Pocket Core"
			}
			var v string
			if v, err = conn.client.Health(ctx); err == nil {
				switch a.slug {
				case "inventory":
					var items []pocket.Item
					items, err = conn.client.Items(ctx, "", 200)
					msg = fmt.Sprintf("Pocket Inventory %s, %s, %d items", v, how, len(items))
				case "monitoring":
					var list []pocket.Server
					list, err = conn.client.Servers(ctx)
					msg = fmt.Sprintf("Pocket Monitoring %s, %s, %d servers", v, how, len(list))
				}
			}
		}
	} else if in.Which == "openai" {
		c := ai.New(urlOr(st.OpenAIBaseURL), keyOr(st.OpenAIKey), st.OpenAIModel, 20*time.Second)
		var ids []string
		if ids, err = c.Models(ctx); err == nil {
			msg = fmt.Sprintf("The key works (%d models)", len(ids))
		}
	} else if in.Which == "notify" {
		f := in.Format
		if f == "" {
			f = st.NotifyFormat
		}
		n := notify.New(urlOr(st.NotifyURL), f)
		err = n.Send(ctx, notify.Message{Title: "Pocket Network", Body: "Notifications work. New devices, devices going offline and coming back will arrive here.", Priority: "info", Tags: []string{"globe_with_meridians"}})
		msg = "Test message sent via " + n.Format
	} else {
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Pocket Network", Body: "Notifications work.", Priority: "info", Tags: []string{"globe_with_meridians"}}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- system / backup / export ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())), "runs": s.runs.Load(),
	})
}

// handleBackup downloads the whole database, an admin-only box operation.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-network-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}

// exportDoc is the JSON export of one person's data (source passwords never leave).
type exportDoc struct {
	App      string         `json:"app"`
	Version  string         `json:"version"`
	Exported string         `json:"exported_at"`
	Devices  []exportDevice `json:"devices"`
	Sources  []store.Source `json:"sources"`
}

type exportDevice struct {
	store.Device
	IDs       []store.DeviceID `json:"ids"`
	Addresses []store.Address  `json:"addresses"`
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	doc := exportDoc{App: "pocket-network", Version: Version, Exported: store.Now(), Devices: []exportDevice{}}
	devs, err := s.store.ListDevices(ctx, uid, store.DeviceFilter{})
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	for _, d := range devs {
		ed := exportDevice{Device: d}
		ed.IDs, _ = s.store.DeviceIDs(ctx, uid, d.ID)
		ed.Addresses, _ = s.store.Addresses(ctx, uid, d.ID)
		doc.Devices = append(doc.Devices, ed)
	}
	doc.Sources, _ = s.store.ListSources(ctx, uid)
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-network-export.json"`)
	writeJSON(w, http.StatusOK, doc)
}

// handleExportCSV writes every device as a CSV row.
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	devs, err := s.store.ListDevices(ctx, uid, store.DeviceFilter{})
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-network-devices.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"name", "hostname", "kind", "owner", "location", "review", "watch", "online", "ip", "mac", "private_mac", "vendor", "model", "os", "network", "first_seen", "last_seen", "notes"})
	for _, d := range devs {
		_ = cw.Write([]string{d.Name, d.Hostname, kindOf(d), d.Owner, d.Location, d.Review, strconv.FormatBool(d.Watch), strconv.FormatBool(d.Online), d.LastIP, d.LastMAC,
			strconv.FormatBool(d.RandomMAC), d.Vendor, d.Model, d.OS, d.LastNetwork, d.FirstSeen, d.LastSeen, d.Notes})
	}
	cw.Flush()
}

// handleImportJSON merges an export into the caller's own devices: a device
// whose MAC or Tailscale node is already here is filled in, never doubled,
// so importing the same file twice adds nothing. Sources are not imported
// (their passwords are not in the file).
func (s *Server) handleImportJSON(w http.ResponseWriter, r *http.Request) {
	var doc exportDoc
	if err := decode(r, &doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if doc.App != "" && doc.App != "pocket-network" {
		writeErr(w, http.StatusBadRequest, "this is not a Pocket Network export")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	counts := map[string]int{"created": 0, "updated": 0, "skipped": 0}
	for _, ed := range doc.Devices {
		var d *store.Device
		for _, id := range ed.IDs {
			if id.Kind != "mac" && id.Kind != "tailscale" {
				continue
			}
			if found := s.store.DeviceByID(ctx, uid, id.Kind, id.Value); found > 0 {
				d, _ = s.store.GetDevice(ctx, uid, found)
				break
			}
		}
		in := ed.Device
		if !validReview(in.Review) || !netscan.ValidKind(in.Kind) {
			counts["skipped"]++
			continue
		}
		if d == nil {
			in.ID, in.UserID, in.Source, in.Online = 0, uid, "import", false
			if err := s.store.CreateDevice(ctx, &in); err != nil {
				counts["skipped"]++
				continue
			}
			d = &in
			counts["created"]++
		} else {
			for _, p := range []struct {
				dst *string
				v   string
			}{{&d.Name, in.Name}, {&d.Owner, in.Owner}, {&d.Kind, in.Kind}, {&d.Location, in.Location}, {&d.Notes, in.Notes}, {&d.LinkURL, in.LinkURL}, {&d.Hostname, in.Hostname}} {
				if *p.dst == "" {
					*p.dst = p.v
				}
			}
			if d.Review == "new" && in.Review != "new" {
				d.Review = in.Review
			}
			d.Watch, d.Pinned = d.Watch || in.Watch, d.Pinned || in.Pinned
			if err := s.store.SaveDevice(ctx, d); err != nil {
				counts["skipped"]++
				continue
			}
			counts["updated"]++
		}
		for _, id := range ed.IDs {
			if (id.Kind == "mac" && netscan.NormalizeMAC(id.Value) != "") || id.Kind == "tailscale" {
				_ = s.store.AddDeviceID(ctx, uid, d.ID, id.Kind, id.Value, id.LastSeen)
			}
		}
		for _, a := range ed.Addresses {
			if a.IP != "" && a.LastSeen != "" {
				_ = s.store.TouchAddress(ctx, uid, d.ID, a.IP, a.MAC, a.Network, 0, a.LastSeen)
			}
		}
	}
	writeJSON(w, http.StatusOK, counts)
}

var _ = sort.Strings
var _ = context.Background
