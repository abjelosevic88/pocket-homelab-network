// Package server wires the HTTP API, auth and the embedded SPA.
package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-network/internal/config"
	"github.com/abjelosevic88/pocket-homelab-network/internal/store"
)

// Version is injected at build time.
var Version = "dev"

// Server holds dependencies.
type Server struct {
	cfg      *config.Config
	store    *store.Store
	log      *slog.Logger
	webFS    fs.FS
	started  time.Time
	requests atomic.Int64
	errors   atomic.Int64
	runs     atomic.Int64

	ingestMu sync.Mutex // one source's results are written at a time
	runMu    sync.Mutex
	running  map[int64]bool // sources being run right now
	digestMu sync.Mutex
	digest   map[int64][]int64 // per person: new-device events gathered for one alert

	oidcMu sync.Mutex
	oidc   *oidcProvider // for the issuer in use; rebuilt when it changes

	core     *core.Client
	coreMu   sync.Mutex
	coreOK   bool
	coreErr  string
	resolved map[string]resolvedURL
	modesMu  sync.Mutex // one connection-mode report at a time
}

type resolvedURL struct {
	url string
	at  time.Time
}

// New creates a server.
func New(cfg *config.Config, st *store.Store, log *slog.Logger, webFS fs.FS) *Server {
	return &Server{cfg: cfg, store: st, log: log, webFS: webFS, started: time.Now(), oidc: &oidcProvider{},
		core:     core.New(cfg.CoreURL, core.Options{DB: st.DB, TokenFile: cfg.DataDir + "/core.token", UserAgent: cfg.UserAgent}),
		resolved: map[string]resolvedURL{}, running: map[int64]bool{}, digest: map[int64][]int64{}}
}

// Bootstrap seeds the first admin (if configured), the owner's integration
// settings and first sources from the environment and demo data, then starts
// the background work.
func (s *Server) Bootstrap(ctx context.Context) error {
	if err := s.core.Start(ctx); err != nil {
		return err
	}
	if err := s.loadSecretKey(); err != nil {
		return err
	}
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 0 && s.cfg.AuthMode == "local" && s.cfg.AdminEmail != "" && s.cfg.AdminPassword != "" {
		if len(s.cfg.AdminPassword) < 8 {
			return errors.New("ADMIN_PASSWORD must be at least 8 characters")
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(s.cfg.AdminPassword), bcrypt.DefaultCost)
		u := &store.User{Email: s.cfg.AdminEmail, Name: s.cfg.AdminName, PasswordHash: string(h), Role: "admin"}
		if err := s.store.CreateUser(ctx, u); err != nil {
			return err
		}
		st, _ := s.store.GetSettings(ctx, 0)
		st.SetupComplete = true
		if err := s.store.SaveBoxSettings(ctx, st); err != nil {
			return err
		}
		s.log.Info("admin user created from environment", "email", u.Email)
	}
	if owner := s.store.OwnerID(ctx); owner > 0 {
		if err := s.ownerReady(ctx, owner); err != nil {
			return err
		}
	}
	_ = s.store.PurgeSessions(ctx)
	go s.janitor(ctx)
	if s.core.Configured() {
		go s.coreLoop(ctx)
	}
	return nil
}

// ownerReady runs once the box owner (the first admin) exists: the
// integration settings from the environment are the owner's — seeded into the
// owner's personal settings only, never into anyone else's — the owner gets
// the first sources (a scan of this server's network, its Tailscale, AdGuard
// from the environment) and demo data lands in the owner's account.
func (s *Server) ownerReady(ctx context.Context, owner int64) error {
	st, err := s.store.GetSettings(ctx, owner)
	if err != nil {
		return err
	}
	changed := false
	once := func(name string, empty bool, v string, apply func()) {
		key := fmt.Sprintf("seeded:%d:%s", owner, name)
		if _, _, done := s.store.CacheGet(ctx, key); done {
			return
		}
		if empty && v != "" {
			apply()
			changed = true
			empty = false
		}
		if !empty {
			_ = s.store.CachePut(ctx, key, "1")
		}
	}
	set := func(name string, dst *string, v string) {
		once(name, *dst == "", v, func() { *dst = v })
	}
	set("NotifyURL", &st.NotifyURL, s.cfg.NotifyURL)
	set("InventoryURL", &st.InventoryURL, s.cfg.InventoryURL)
	set("InventoryToken", &st.InventoryToken, s.cfg.InventoryToken)
	set("MonitoringURL", &st.MonitoringURL, s.cfg.MonitoringURL)
	set("MonitoringToken", &st.MonitoringToken, s.cfg.MonitoringToken)
	set("OpenAIKey", &st.OpenAIKey, s.cfg.OpenAIKey)
	set("OpenAIModel", &st.OpenAIModel, s.cfg.OpenAIModel)
	set("OpenAIBaseURL", &st.OpenAIBaseURL, s.cfg.OpenAIBaseURL)
	if s.cfg.NotifyFormat != "" && st.NotifyURL == s.cfg.NotifyURL && st.NotifyFormat != s.cfg.NotifyFormat {
		st.NotifyFormat = s.cfg.NotifyFormat
		changed = true
	}
	if changed {
		if err := s.store.SaveUserSettings(ctx, owner, st); err != nil {
			return err
		}
	}
	if s.cfg.DemoData {
		return s.seedDemo(ctx, owner)
	}
	s.seedSources(ctx, owner)
	return nil
}

// janitor runs the background work: every 20 seconds the sources that are
// due, every minute who went offline (and the alerts), every hour the
// public address check, history clean-up and forgetting old guests.
func (s *Server) janitor(ctx context.Context) {
	first := time.NewTimer(5 * time.Second)
	tick := time.NewTicker(20 * time.Second)
	minute := time.NewTicker(time.Minute)
	hour := time.NewTicker(time.Hour)
	defer tick.Stop()
	defer minute.Stop()
	defer hour.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			s.runDue(ctx)
			s.checkWAN(ctx)
		case <-tick.C:
			s.runDue(ctx)
		case <-minute.C:
			s.checkOffline(ctx)
			s.flushDigests(ctx, false)
		case <-hour.C:
			s.checkWAN(ctx)
			s.housekeeping(ctx)
		}
	}
}

// housekeeping prunes old history, forgets old guests and expired sessions.
func (s *Server) housekeeping(ctx context.Context) {
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	for _, uid := range ids {
		st, _ := s.store.GetSettings(ctx, uid)
		s.store.Prune(ctx, uid, st.KeepDays)
		if st.ForgetGuestsDays > 0 {
			if n := s.store.ForgetGuests(ctx, uid, time.Now().AddDate(0, 0, -st.ForgetGuestsDays).UTC().Format(time.RFC3339Nano)); n > 0 {
				s.log.Info("forgot old guests", "user", uid, "count", n)
			}
		}
	}
	_ = s.store.PurgeSessions(ctx)
}

// ---- Pocket Core ----

// coreLoop registers with Core on start (retrying until it answers) and
// re-registers every 30 minutes so version and last-seen stay fresh; each
// time it also reports how everybody connects (modes.go).
func (s *Server) coreLoop(ctx context.Context) {
	wait := 5 * time.Second
	for {
		err := s.registerWithCore(ctx)
		s.coreMu.Lock()
		s.coreOK, s.coreErr = err == nil, ""
		if err != nil {
			s.coreErr = err.Error()
		}
		s.coreMu.Unlock()
		if err != nil {
			s.log.Warn("pocket core: registration failed, retrying", "err", err, "in", wait.String())
		} else {
			wait = 30 * time.Minute
			s.reportModes(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err != nil && wait < 5*time.Minute {
			wait *= 2
		}
	}
}

// eventTopics are what Network publishes on the Core bus.
var eventTopics = []string{"network.device_new", "network.device_offline", "network.device_back", "network.wan_ip_changed"}

func (s *Server) registerWithCore(ctx context.Context) error {
	m := core.Manifest{
		Slug: "network", Name: "Pocket Network",
		Description: "Every device on your home network in one list: found by scanning, Tailscale, AdGuard Home and your router, with a simple review of new devices, online and offline history, alerts when something new joins or an important device drops off, and links to the item in Pocket Inventory and the server in Pocket Monitoring.",
		Icon:        "/favicon.svg", Version: Version,
		URL: s.cfg.CoreAppURL, PublicURL: s.cfg.BaseURL, HealthPath: "/healthz", EnvPrefix: "NETWORK",
		Publishes: eventTopics,
		Offers:    linkOffers,
		Uses:      append(linkUses(), core.InferenceUse()),
		Services:  []core.ServiceUse{{Type: "adguard", Reason: "Names and DNS activity for each device from your AdGuard Home"}},
	}
	if s.cfg.BaseURL != "" {
		m.OIDC = &core.OIDCRequest{RedirectURIs: []string{s.oidcCallbackURL()}}
	}
	reg, err := s.core.Register(ctx, s.cfg.CoreRegistrationKey, m)
	if err != nil {
		return err
	}
	if reg.Token != "" {
		s.log.Info("pocket core: registered, app token stored", "core", s.cfg.CoreURL)
	} else {
		s.log.Debug("pocket core: re-registered", "core", s.cfg.CoreURL)
	}
	return nil
}

// resolveSibling asks Core where a sibling app lives (cached a minute).
func (s *Server) resolveSibling(ctx context.Context, slug string) string {
	if !s.core.Configured() {
		return ""
	}
	s.coreMu.Lock()
	if r, ok := s.resolved[slug]; ok && time.Since(r.at) < time.Minute {
		s.coreMu.Unlock()
		return r.url
	}
	s.coreMu.Unlock()
	url := ""
	if res, err := s.core.Resolve(ctx, slug); err == nil {
		url = res.URL
	}
	s.coreMu.Lock()
	s.resolved[slug] = resolvedURL{url: url, at: time.Now()}
	s.coreMu.Unlock()
	return url
}

// coreStatus summarises the Pocket Core link for the About page.
func (s *Server) coreStatus() map[string]any {
	s.coreMu.Lock()
	defer s.coreMu.Unlock()
	return map[string]any{"configured": s.core.Configured(), "url": s.cfg.CoreURL, "registered": s.coreOK, "error": s.coreErr}
}

// publish sends an event for a person (fire and forget, through the outbox).
func (s *Server) publish(ctx context.Context, uid int64, topic, subject string, payload map[string]any) {
	if !s.core.Configured() {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		payload["user_email"] = u.Email
	}
	if err := s.core.Publish(ctx, topic, subject, payload); err != nil {
		s.log.Debug("pocket core: publish failed", "topic", topic, "err", err)
	}
}
