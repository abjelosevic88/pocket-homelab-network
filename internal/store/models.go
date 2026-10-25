package store

import "encoding/json"

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Source is somewhere devices are seen (see 0001_init.sql).
type Source struct {
	ID        int64           `json:"id"`
	UserID    int64           `json:"-"`
	Kind      string          `json:"kind"`
	Name      string          `json:"name"`
	Network   string          `json:"network"`
	Config    json.RawMessage `json:"config"`
	Secret    string          `json:"-"`
	TokenHash string          `json:"-"`
	TokenHint string          `json:"token_hint"`
	Enabled   bool            `json:"enabled"`
	IntervalS int             `json:"interval_s"`
	Presence  bool            `json:"presence"`
	LastRun   string          `json:"last_run"`
	LastOK    string          `json:"last_ok"`
	LastError string          `json:"last_error"`
	LastCount int             `json:"last_count"`
	LastMS    int             `json:"last_ms"`
	Sort      int             `json:"sort"`
	CreatedAt string          `json:"created_at"`
	UpdatedAt string          `json:"updated_at"`
	HasSecret bool            `json:"has_secret"`
}

// SourceConfig is what a source's config JSON may hold.
type SourceConfig struct {
	Interface string `json:"interface,omitempty"` // scan
	Subnet    string `json:"subnet,omitempty"`    // scan: CIDR, "" = the interface's own
	Names     *bool  `json:"names,omitempty"`     // scan: look names up (reverse DNS, mDNS, NetBIOS)
	DNS       string `json:"dns,omitempty"`       // scan: DNS server for reverse lookups
	URL       string `json:"url,omitempty"`       // adguard, openwrt, tailscale API
	Username  string `json:"username,omitempty"`  // adguard, openwrt
	Socket    string `json:"socket,omitempty"`    // tailscale: local tailscaled socket
	Tailnet   string `json:"tailnet,omitempty"`   // tailscale API: tailnet name ("-" = the key's own)
	ViaCore   bool   `json:"via_core,omitempty"`  // adguard: use the AdGuard set up in Pocket Core
}

// Device is one physical thing on a network.
type Device struct {
	ID             int64  `json:"id"`
	UserID         int64  `json:"-"`
	Name           string `json:"name"`
	Hostname       string `json:"hostname"`
	Owner          string `json:"owner"`
	Kind           string `json:"kind"`
	KindGuess      string `json:"kind_guess"`
	Vendor         string `json:"vendor"`
	Model          string `json:"model"`
	OS             string `json:"os"`
	Location       string `json:"location"`
	Notes          string `json:"notes"`
	LinkURL        string `json:"link_url"`
	Review         string `json:"review"`
	Watch          bool   `json:"watch"`
	Muted          bool   `json:"muted"`
	Pinned         bool   `json:"pinned"`
	Online         bool   `json:"online"`
	StatusAt       string `json:"status_at"`
	FirstSeen      string `json:"first_seen"`
	LastSeen       string `json:"last_seen"`
	LastIP         string `json:"last_ip"`
	LastMAC        string `json:"last_mac"`
	LastNetwork    string `json:"last_network"`
	LastSource     string `json:"last_source"`
	RandomMAC      bool   `json:"random_mac"`
	DNSQueries     int    `json:"dns_queries"`
	DNSBlocked     int    `json:"dns_blocked"`
	Ports          string `json:"ports"`
	InventoryID    int64  `json:"inventory_id"`
	InventoryName  string `json:"inventory_name"`
	MonitoringID   int64  `json:"monitoring_id"`
	MonitoringName string `json:"monitoring_name"`
	Source         string `json:"source"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// Label is the name to show: the given name, else the network's name for it, else the vendor.
func (d Device) Label() string {
	switch {
	case d.Name != "":
		return d.Name
	case d.Hostname != "":
		return d.Hostname
	case d.Vendor != "":
		return d.Vendor + " device"
	case d.LastIP != "":
		return "Device at " + d.LastIP
	}
	return "Unnamed device"
}

// DeviceID is one way a device is recognised.
type DeviceID struct {
	ID        int64  `json:"id"`
	DeviceID  int64  `json:"device_id"`
	Kind      string `json:"kind"`
	Value     string `json:"value"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
	Private   bool   `json:"private"` // a locally administered (randomised) MAC
	Vendor    string `json:"vendor"`
}

// Address is an IP a device used.
type Address struct {
	ID        int64  `json:"id"`
	DeviceID  int64  `json:"device_id"`
	IP        string `json:"ip"`
	MAC       string `json:"mac"`
	Network   string `json:"network"`
	SourceID  int64  `json:"source_id"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
}

// Event is one thing that happened.
type Event struct {
	ID         int64  `json:"id"`
	DeviceID   *int64 `json:"device_id"`
	Kind       string `json:"kind"`
	At         string `json:"at"`
	Text       string `json:"text"`
	IP         string `json:"ip"`
	SourceID   int64  `json:"source_id"`
	Alerted    bool   `json:"alerted"`
	DeviceName string `json:"device_name,omitempty"`
	DeviceKind string `json:"device_kind,omitempty"`
}

// Scan is one run of a source.
type Scan struct {
	ID       int64  `json:"id"`
	SourceID int64  `json:"source_id"`
	At       string `json:"at"`
	Found    int    `json:"found"`
	New      int    `json:"new"`
	MS       int    `json:"ms"`
	Error    string `json:"error"`
}

// Settings are the box-wide values plus one person's preferences and their
// own connections. Connection modes: "core" (an app link through Pocket
// Core), "direct" (URL + token) or "off"; "" = not chosen yet.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`
	WeekStart  int    `json:"week_start"` // 1 = Monday, 7 = Sunday

	// How long a device may go unseen before it counts as offline.
	OfflineMin int `json:"offline_min"`
	// A new private address with the same name as a device you have joins
	// that device instead of showing up as new.
	AutoMerge bool `json:"auto_merge"`
	// Watch new routers, servers and NAS boxes automatically.
	AutoWatch bool `json:"auto_watch"`
	// Days of history (events, online history, scans) kept.
	KeepDays int `json:"keep_days"`
	// Follow this server's public internet address.
	TrackWAN bool `json:"track_wan"`
	// Guests not seen for this many days are forgotten (0 = never).
	ForgetGuestsDays int `json:"forget_guests_days"`
	// Owners offered in the review (people in the household).
	Owners []string `json:"owners"`

	// Alerts.
	NotifyNew       bool   `json:"notify_new"`     // a device you have not seen before
	NotifyOffline   bool   `json:"notify_offline"` // a watched device dropped off
	NotifyBack      bool   `json:"notify_back"`    // a watched device came back
	NotifyWAN       bool   `json:"notify_wan"`     // the internet address changed
	NotifySource    bool   `json:"notify_source"`  // a source stopped working
	QuietFrom       string `json:"quiet_from"`     // "23:00", "" = no quiet hours
	QuietTo         string `json:"quiet_to"`       // "07:00"
	NotifyURL       string `json:"notify_url"`
	NotifyFormat    string `json:"notify_format"`     // apprise | ntfy | json
	NotifyDigestMin int    `json:"notify_digest_min"` // gather new-device alerts for this many minutes (0 = at once)

	InventoryMode      string `json:"inventory_mode"`
	InventoryURL       string `json:"inventory_url"`
	InventoryToken     string `json:"inventory_token"`
	InventoryPublicURL string `json:"inventory_public_url"`

	MonitoringMode      string `json:"monitoring_mode"`
	MonitoringURL       string `json:"monitoring_url"`
	MonitoringToken     string `json:"monitoring_token"`
	MonitoringPublicURL string `json:"monitoring_public_url"`

	// AI for "what is this device?" and questions: Pocket AI through Core,
	// or an OpenAI-compatible API with the person's own key.
	AIProvider    string `json:"ai_provider"` // auto | pocket | openai
	AIPocketModel string `json:"ai_pocket_model"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"`
}
