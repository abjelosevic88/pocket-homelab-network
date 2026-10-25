package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-network/internal/collect"
	"github.com/abjelosevic88/pocket-homelab-network/internal/netscan"
	"github.com/abjelosevic88/pocket-homelab-network/internal/store"
)

// ---- sources: where devices are seen ----

var sourceKinds = map[string]string{
	"scan":      "Scan of this server's network",
	"agent":     "Agent on another network",
	"tailscale": "Tailscale",
	"adguard":   "AdGuard Home",
	"openwrt":   "Router (OpenWrt)",
}

// sourceCreates says whether a source's sightings may add new devices
// (AdGuard only names and counts devices the others found).
func sourceCreates(kind string) bool { return kind != "adguard" }

func defaultInterval(kind string) int {
	switch kind {
	case "tailscale":
		return 120
	case "agent":
		return 300
	}
	return 300
}

func sourceCfg(src *store.Source) store.SourceConfig {
	var c store.SourceConfig
	_ = json.Unmarshal(src.Config, &c)
	return c
}

func boolp(b bool) *bool { return &b }

// seedSources gives the box owner their first sources, once each (deleting
// one sticks): a scan of this server's network, the local Tailscale and
// AdGuard Home when the environment names one.
func (s *Server) seedSources(ctx context.Context, owner int64) {
	once := func(key string, make func() *store.Source) {
		k := fmt.Sprintf("seeded-source:%d:%s", owner, key)
		if _, _, done := s.store.CacheGet(ctx, k); done {
			return
		}
		src := make()
		if src == nil {
			return
		}
		src.UserID = owner
		if err := s.store.CreateSource(ctx, src); err != nil {
			s.log.Warn("create first source", "kind", src.Kind, "err", err)
			return
		}
		_ = s.store.CachePut(ctx, k, "1")
		s.log.Info("added a first source", "kind", src.Kind, "name", src.Name)
	}
	if s.cfg.LocalScan {
		once("scan", func() *store.Source {
			ifs := netscan.Interfaces()
			cfg := store.SourceConfig{Interface: s.cfg.ScanInterface, Subnet: s.cfg.ScanSubnet, Names: boolp(true)}
			subnet := cfg.Subnet
			if cfg.Interface == "" && cfg.Subnet == "" {
				if len(ifs) == 0 {
					return nil
				}
				cfg.Interface, subnet = ifs[0].Name, ifs[0].Subnet
			}
			for _, i := range ifs {
				if i.Name == cfg.Interface && subnet == "" {
					subnet = i.Subnet
				}
			}
			b, _ := json.Marshal(cfg)
			return &store.Source{Kind: "scan", Name: "This server's network", Network: firstNonEmpty(subnet, "Home network"), Config: b, Enabled: true, Presence: true, IntervalS: int(s.cfg.ScanInterval.Seconds())}
		})
	}
	if sock := s.cfg.TailscaleSocket; sock != "" {
		if _, err := os.Stat(sock); err == nil {
			once("tailscale", func() *store.Source {
				b, _ := json.Marshal(store.SourceConfig{Socket: sock})
				return &store.Source{Kind: "tailscale", Name: "Tailscale", Network: "Tailscale", Config: b, Enabled: true, Presence: true, IntervalS: 120}
			})
		}
	}
	if s.cfg.AdGuardURL != "" {
		once("adguard", func() *store.Source {
			b, _ := json.Marshal(store.SourceConfig{URL: s.cfg.AdGuardURL, Username: s.cfg.AdGuardUser})
			sealed, _ := s.seal(s.cfg.AdGuardPassword)
			return &store.Source{Kind: "adguard", Name: "AdGuard Home", Config: b, Secret: sealed, Enabled: true, Presence: true, IntervalS: 300}
		})
	}
}

// ---- running ----

func (s *Server) due(src store.Source, now time.Time) bool {
	if src.Kind == "agent" {
		return false
	}
	if src.LastRun == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339Nano, src.LastRun)
	return err != nil || now.Sub(t) >= time.Duration(max(src.IntervalS, 60))*time.Second
}

// runDue starts every source whose turn it is (each at most once at a time)
// and notices agents that stopped reporting.
func (s *Server) runDue(ctx context.Context) {
	if s.cfg.DemoData {
		return
	}
	list, err := s.store.EnabledSources(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	for _, src := range list {
		if src.Kind == "agent" {
			s.checkAgent(ctx, src, now)
			continue
		}
		if !s.due(src, now) || !s.claim(src.ID) {
			continue
		}
		go func(src store.Source) {
			defer s.release(src.ID)
			c, cancel := context.WithTimeout(ctx, 3*time.Minute)
			defer cancel()
			_, _, _ = s.runSource(c, &src)
		}(src)
	}
}

func (s *Server) claim(id int64) bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.running[id] {
		return false
	}
	s.running[id] = true
	return true
}

func (s *Server) release(id int64) {
	s.runMu.Lock()
	delete(s.running, id)
	s.runMu.Unlock()
}

func (s *Server) isRunning(id int64) bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	return s.running[id]
}

// checkAgent marks an agent that has not reported for three of its intervals.
func (s *Server) checkAgent(ctx context.Context, src store.Source, now time.Time) {
	if src.LastRun == "" || src.LastError != "" {
		return
	}
	t, err := time.Parse(time.RFC3339Nano, src.LastRun)
	if err != nil || now.Sub(t) < 3*time.Duration(max(src.IntervalS, 60))*time.Second+time.Minute {
		return
	}
	msg := "the agent has not reported since " + t.Local().Format("2 Jan 15:04")
	_ = s.store.RecordRun(ctx, &src, store.Now(), 0, 0, 0, msg)
	s.sourceChanged(ctx, &src, "", msg)
}

// runSource reads one source and writes what it saw.
func (s *Server) runSource(ctx context.Context, src *store.Source) (found, fresh int, err error) {
	start := time.Now()
	obs, network, err := s.collect(ctx, src)
	prevErr := src.LastError
	if err == nil {
		found, fresh, err = s.ingest(ctx, src, network, obs, time.Now())
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	s.runs.Add(1)
	_ = s.store.RecordRun(ctx, src, store.Now(), found, fresh, int(time.Since(start).Milliseconds()), msg)
	s.sourceChanged(ctx, src, prevErr, msg)
	if err != nil {
		s.log.Warn("source failed", "source", src.Name, "kind", src.Kind, "err", err)
	} else {
		s.log.Debug("source ran", "source", src.Name, "found", found, "new", fresh, "took", time.Since(start).String())
	}
	return found, fresh, err
}

// sourceChanged records (and alerts on) a source starting or stopping to work.
func (s *Server) sourceChanged(ctx context.Context, src *store.Source, prevErr, nowErr string) {
	switch {
	case prevErr == "" && nowErr != "" && src.LastRun != "":
		e := &store.Event{Kind: "source_failed", Text: src.Name + " stopped working: " + nowErr, SourceID: src.ID}
		if s.store.AddEvent(ctx, src.UserID, e) == nil {
			s.queueAlert(ctx, src.UserID, e.ID)
		}
	case prevErr != "" && nowErr == "":
		_ = s.store.AddEvent(ctx, src.UserID, &store.Event{Kind: "source_ok", Text: src.Name + " works again", SourceID: src.ID})
	}
}

// collect asks a source what it sees now. network is the label its devices get.
func (s *Server) collect(ctx context.Context, src *store.Source) ([]netscan.Observation, string, error) {
	cfg := sourceCfg(src)
	secret, err := s.open(src.Secret)
	if err != nil {
		return nil, "", err
	}
	owner, err := s.store.GetUser(ctx, src.UserID)
	if err != nil {
		return nil, "", err
	}
	switch src.Kind {
	case "scan":
		if !s.cfg.LocalScan {
			return nil, "", errors.New("scanning from this server is switched off (LOCAL_SCAN=false)")
		}
		if owner.Role != "admin" {
			return nil, "", errors.New("only admins can scan this server's networks; use an agent")
		}
		res, err := netscan.Sweep(ctx, netscan.Options{Interface: cfg.Interface, Subnet: cfg.Subnet, Names: cfg.Names == nil || *cfg.Names, DNS: cfg.DNS})
		if err != nil {
			return nil, "", err
		}
		if res.Note != "" {
			s.log.Debug("scan", "note", res.Note)
		}
		return res.Hosts, firstNonEmpty(src.Network, res.Subnet), nil
	case "tailscale":
		var obs []netscan.Observation
		if secret != "" {
			obs, err = collect.TailscaleAPI(ctx, cfg.URL, cfg.Tailnet, secret)
		} else {
			if owner.Role != "admin" {
				return nil, "", errors.New("only admins can read this server's Tailscale; add an API key")
			}
			obs, err = collect.TailscaleLocal(ctx, firstNonEmpty(cfg.Socket, s.cfg.TailscaleSocket))
		}
		return obs, firstNonEmpty(src.Network, "Tailscale"), err
	case "adguard":
		url, user, pass := cfg.URL, cfg.Username, secret
		if cfg.ViaCore {
			cred, err := s.core.ServiceFor(ctx, owner.Email, "adguard")
			if err != nil {
				return nil, "", fmt.Errorf("AdGuard Home through Pocket Core: %w", err)
			}
			url, user, pass = firstNonEmpty(cred.URL, cred.PublicURL), cred.Fields["USERNAME"], cred.Fields["PASSWORD"]
		}
		obs, err := collect.AdGuard(ctx, url, user, pass)
		return obs, src.Network, err
	case "openwrt":
		obs, err := collect.OpenWrt(ctx, cfg.URL, cfg.Username, secret)
		return obs, firstNonEmpty(src.Network, "Router"), err
	case "agent":
		return nil, "", errors.New("an agent reports by itself; run `pocket-network agent` on that network")
	}
	return nil, "", fmt.Errorf("unknown source kind %q", src.Kind)
}

// ---- API ----

type sourceView struct {
	store.Source
	Cfg      store.SourceConfig `json:"cfg"`
	KindName string             `json:"kind_name"`
	Status   string             `json:"status"` // ok | error | waiting | off | running
	Running  bool               `json:"running"`
}

func (s *Server) sourceView(src store.Source) sourceView {
	v := sourceView{Source: src, Cfg: sourceCfg(&src), KindName: sourceKinds[src.Kind], Running: s.isRunning(src.ID)}
	switch {
	case !src.Enabled:
		v.Status = "off"
	case v.Running:
		v.Status = "running"
	case src.LastError != "":
		v.Status = "error"
	case src.LastRun == "":
		v.Status = "waiting"
	default:
		v.Status = "ok"
	}
	return v
}

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := userFrom(ctx)
	list, err := s.store.ListSources(ctx, me.ID)
	if err != nil {
		s.fail(w, err, "sources")
		return
	}
	out := make([]sourceView, 0, len(list))
	for _, src := range list {
		out = append(out, s.sourceView(src))
	}
	admin := me.Role == "admin"
	caps := map[string]any{"local_scan": s.cfg.LocalScan && admin, "tailscale_local": false, "interfaces": []netscan.Interface{}, "core": s.core.Configured()}
	if admin {
		if s.cfg.LocalScan {
			caps["interfaces"] = netscan.Interfaces()
		}
		if _, err := os.Stat(s.cfg.TailscaleSocket); err == nil {
			caps["tailscale_local"] = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": out, "can": caps, "kinds": sourceKinds, "base_url": s.cfg.BaseURL})
}

type sourceInput struct {
	Kind      *string             `json:"kind"`
	Name      *string             `json:"name"`
	Network   *string             `json:"network"`
	Cfg       *store.SourceConfig `json:"cfg"`
	Secret    *string             `json:"secret"`
	Enabled   *bool               `json:"enabled"`
	IntervalS *int                `json:"interval_s"`
	Presence  *bool               `json:"presence"`
}

// apply checks and copies the input onto a source; "" means fine.
func (s *Server) applySource(me *store.User, src *store.Source, in sourceInput) string {
	if in.Name != nil {
		src.Name = strings.TrimSpace(*in.Name)
	}
	if in.Network != nil {
		src.Network = strings.TrimSpace(*in.Network)
	}
	if in.Enabled != nil {
		src.Enabled = *in.Enabled
	}
	if in.Presence != nil {
		src.Presence = *in.Presence
	}
	if in.IntervalS != nil {
		if *in.IntervalS < 60 || *in.IntervalS > 86400 {
			return "check every 1 minute to 24 hours"
		}
		src.IntervalS = *in.IntervalS
	}
	if in.Secret != nil && !strings.Contains(*in.Secret, "••") {
		sealed, err := s.seal(strings.TrimSpace(*in.Secret))
		if err != nil {
			return err.Error()
		}
		src.Secret = sealed
	}
	cfg := sourceCfg(src)
	if in.Cfg != nil {
		cfg = *in.Cfg
	}
	cfg.URL = strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	admin := me.Role == "admin"
	switch src.Kind {
	case "scan":
		if !s.cfg.LocalScan {
			return "scanning from this server is switched off on this box"
		}
		if !admin {
			return "only admins can scan this server's networks; add an agent instead"
		}
		if cfg.Subnet != "" {
			n, err := netscan.ParseSubnet(cfg.Subnet)
			if err != nil {
				return err.Error()
			}
			cfg.Subnet = n.String()
		}
		if cfg.Interface != "" {
			if _, err := netInterface(cfg.Interface); err != nil {
				return err.Error()
			}
		}
		if cfg.Names == nil {
			cfg.Names = boolp(true)
		}
		cfg.URL, cfg.Username, cfg.Socket, cfg.Tailnet, cfg.ViaCore = "", "", "", "", false
	case "tailscale":
		if src.Secret == "" && !admin {
			return "add a Tailscale API key (only admins can read this server's own Tailscale)"
		}
		if src.Secret == "" && cfg.Socket == "" {
			cfg.Socket = s.cfg.TailscaleSocket
		}
		cfg.Interface, cfg.Subnet, cfg.Username, cfg.ViaCore = "", "", "", false
	case "adguard":
		if cfg.ViaCore {
			if !s.core.Configured() {
				return "AdGuard Home through Pocket Core needs Pocket Core"
			}
			cfg.URL, cfg.Username, src.Secret = "", "", ""
		} else if cfg.URL == "" {
			return "add AdGuard Home's address (http://192.168.1.2:3000)"
		}
		cfg.Interface, cfg.Subnet, cfg.Socket, cfg.Tailnet = "", "", "", ""
	case "openwrt":
		if cfg.URL == "" {
			return "add the router's address (http://192.168.8.1)"
		}
		if cfg.Username == "" {
			cfg.Username = "root"
		}
		cfg.Interface, cfg.Subnet, cfg.Socket, cfg.Tailnet, cfg.ViaCore = "", "", "", "", false
	case "agent":
		cfg = store.SourceConfig{}
	default:
		return "unknown kind of source"
	}
	if cfg.URL != "" && !strings.HasPrefix(cfg.URL, "http://") && !strings.HasPrefix(cfg.URL, "https://") {
		cfg.URL = "http://" + cfg.URL
	}
	b, _ := json.Marshal(cfg)
	src.Config = b
	if src.Name == "" {
		src.Name = sourceKinds[src.Kind]
	}
	return ""
}

func netInterface(name string) (*netscan.Interface, error) {
	for _, i := range netscan.Interfaces() {
		if i.Name == name {
			return &i, nil
		}
	}
	return nil, fmt.Errorf("this server has no network interface %q", name)
}

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := userFrom(ctx)
	var in sourceInput
	if err := decode(r, &in); err != nil || in.Kind == nil {
		writeErr(w, http.StatusBadRequest, "kind is required")
		return
	}
	src := &store.Source{UserID: me.ID, Kind: *in.Kind, Enabled: true, Presence: true, IntervalS: defaultInterval(*in.Kind)}
	if _, ok := sourceKinds[src.Kind]; !ok {
		writeErr(w, http.StatusBadRequest, "unknown kind of source")
		return
	}
	if msg := s.applySource(me, src, in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	token := ""
	if src.Kind == "agent" {
		token = "pna_" + store.RandomToken(24)
		src.TokenHash, src.TokenHint = store.HashToken(token), token[:8]
	}
	if err := s.store.CreateSource(ctx, src); err != nil {
		s.fail(w, err, "create source")
		return
	}
	if src.Kind != "agent" && src.Enabled {
		go s.runNowBackground(*src)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"source": s.sourceView(*src), "token": token})
}

func (s *Server) runNowBackground(src store.Source) {
	if !s.claim(src.ID) {
		return
	}
	defer s.release(src.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	_, _, _ = s.runSource(ctx, &src)
}

func (s *Server) handleUpdateSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := userFrom(ctx)
	src, err := s.store.GetSource(ctx, me.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "source")
		return
	}
	var in sourceInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Kind = nil
	if msg := s.applySource(me, src, in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateSource(ctx, src); err != nil {
		s.fail(w, err, "save source")
		return
	}
	writeJSON(w, http.StatusOK, s.sourceView(*src))
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	src, err := s.store.GetSource(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "source")
		return
	}
	if err := s.store.DeleteSource(ctx, uid, src.ID); err != nil {
		s.fail(w, err, "delete source")
		return
	}
	_ = s.store.CachePut(ctx, fmt.Sprintf("seeded-source:%d:%s", uid, src.Kind), "1")
	w.WriteHeader(http.StatusNoContent)
}

// handleRunSource runs a source now and answers with what it found.
func (s *Server) handleRunSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	src, err := s.store.GetSource(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "source")
		return
	}
	if src.Kind == "agent" {
		writeErr(w, http.StatusBadRequest, "an agent reports by itself on its own schedule")
		return
	}
	if !s.claim(src.ID) {
		writeErr(w, http.StatusConflict, "it is running already")
		return
	}
	defer s.release(src.ID)
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
	defer cancel()
	found, fresh, err := s.runSource(c, src)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "found": found, "new": fresh})
}

// handleScanAll runs every switched-on source of the caller now.
func (s *Server) handleScanAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.ListSources(ctx, actorID(ctx))
	if err != nil {
		s.fail(w, err, "sources")
		return
	}
	started := 0
	for _, src := range list {
		if src.Enabled && src.Kind != "agent" && !s.isRunning(src.ID) {
			started++
			go s.runNowBackground(src)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"started": started})
}

// handleSourceToken makes a new agent token (the old one stops working).
func (s *Server) handleSourceToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	src, err := s.store.GetSource(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "source")
		return
	}
	if src.Kind != "agent" {
		writeErr(w, http.StatusBadRequest, "only agents have tokens")
		return
	}
	token := "pna_" + store.RandomToken(24)
	src.TokenHash, src.TokenHint = store.HashToken(token), token[:8]
	if err := s.store.UpdateSource(ctx, src); err != nil {
		s.fail(w, err, "save source")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "source": s.sourceView(*src)})
}

func (s *Server) handleSourceScans(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	if _, err := s.store.GetSource(ctx, uid, idParam(r, "id")); err != nil {
		s.fail(w, err, "source")
		return
	}
	list, err := s.store.ListScans(ctx, uid, idParam(r, "id"), queryInt(r, "limit"))
	if err != nil {
		s.fail(w, err, "runs")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// ---- the agent ----

// agentReport is what `pocket-network agent` posts after each sweep.
type agentReport struct {
	Version string                `json:"version"`
	Host    string                `json:"host"`
	Subnet  string                `json:"subnet"`
	Method  string                `json:"method"`
	Note    string                `json:"note"`
	TookMS  int                   `json:"took_ms"`
	Error   string                `json:"error"`
	Hosts   []netscan.Observation `json:"hosts"`
}

// handleAgentReport takes an agent's sweep (authenticated by its own pna_ token).
func (s *Server) handleAgentReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	src, err := s.store.SourceByToken(ctx, tok)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unknown agent token")
		return
	}
	if !src.Enabled {
		writeJSON(w, http.StatusOK, map[string]any{"interval_s": src.IntervalS, "paused": true})
		return
	}
	var in agentReport
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(in.Hosts) > 4096 {
		writeErr(w, http.StatusBadRequest, "too many hosts in one report")
		return
	}
	prevErr := src.LastError
	found, fresh := 0, 0
	msg := strings.TrimSpace(in.Error)
	if msg == "" {
		found, fresh, err = s.ingest(ctx, src, firstNonEmpty(src.Network, in.Subnet), in.Hosts, time.Now())
		if err != nil {
			msg = err.Error()
		}
	}
	if src.Network == "" && in.Subnet != "" {
		src.Network = in.Subnet
		_ = s.store.UpdateSource(ctx, src)
	}
	_ = s.store.RecordRun(ctx, src, store.Now(), found, fresh, in.TookMS, msg)
	s.sourceChanged(ctx, src, prevErr, msg)
	writeJSON(w, http.StatusOK, map[string]any{"interval_s": src.IntervalS, "found": found, "new": fresh})
}
